// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package skills

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
)

// SourceBinding makes failure tolerance explicit. Optional failures retain the
// previous source listing; the first failure contributes no skills and a warning.
type SourceBinding struct {
	Source   SkillSource
	Optional bool
}
type RegistryConfig struct {
	LocalDirectories []string
	Sources          []SourceBinding
	Cache            *SkillCache
}
type RegistryIssue struct {
	Source string
	Err    error
}
type registryEntry struct {
	local  *Skill
	remote RemoteSkill
	source SkillSource
}

// Registry serializes refreshes and atomically publishes complete metadata
// generations. Local skills win collisions, then remote sources in config order.
type Registry struct {
	config   RegistryConfig
	gate     chan struct{}
	mu       sync.RWMutex
	entries  map[string]registryEntry
	previous map[string][]RemoteSkill
}

func NewRegistry(c RegistryConfig) (*Registry, error) {
	if len(c.Sources) > 0 && c.Cache == nil {
		return nil, errors.New("remote sources require a skill cache")
	}
	seen := map[string]bool{}
	for _, b := range c.Sources {
		if b.Source == nil || b.Source.Identity() == "" {
			return nil, errors.New("source identity required")
		}
		if seen[b.Source.Identity()] {
			return nil, errors.New("duplicate source identity")
		}
		seen[b.Source.Identity()] = true
	}
	c.LocalDirectories = append([]string(nil), c.LocalDirectories...)
	c.Sources = append([]SourceBinding(nil), c.Sources...)
	return &Registry{config: c, gate: make(chan struct{}, 1), entries: map[string]registryEntry{}, previous: map[string][]RemoteSkill{}}, nil
}

// Refresh performs metadata discovery only; it never downloads a skill archive.
// A strict failure leaves the entire current generation unchanged. Optional
// failure issues are returned separately from the transaction error.
func (r *Registry) Refresh(ctx context.Context) ([]RegistryIssue, error) {
	select {
	case r.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-r.gate }()
	next := map[string]registryEntry{}
	previous := map[string][]RemoteSkill{}
	issues := []RegistryIssue{}
	for _, dir := range r.config.LocalDirectories {
		list, err := DiscoverSkillsFromDirWithOptions(ctx, dir, DiscoveryOptions{Mode: DiscoveryStrict})
		if err != nil {
			return nil, err
		}
		for _, sk := range list {
			if _, ok := next[sk.Name()]; !ok {
				next[sk.Name()] = registryEntry{local: sk}
			}
		}
	}
	for _, binding := range r.config.Sources {
		source := binding.Source
		id := source.Identity()
		list, err := source.Discover(ctx)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			seen := map[string]bool{}
			for _, d := range list {
				if validateRemote(d) != nil || seen[d.Name] {
					err = errors.New("invalid or duplicate source metadata")
					break
				}
				seen[d.Name] = true
			}
		}
		if err != nil {
			if !binding.Optional {
				return nil, errors.New("required skill source discovery failed")
			}
			issues = append(issues, RegistryIssue{Source: id, Err: errors.New("optional skill source discovery failed; previous listing retained")})
			list = r.previous[id]
		}
		previous[id] = append([]RemoteSkill(nil), list...)
		for _, d := range list {
			if _, ok := next[d.Name]; !ok {
				next[d.Name] = registryEntry{remote: d, source: source}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Pin the current generation even when no caller has requested a snapshot.
	if r.config.Cache != nil {
		for _, e := range next {
			if e.source != nil {
				r.config.Cache.pin(cacheKey(e.source.Identity(), e.remote))
			}
		}
		for _, e := range r.entries {
			if e.source != nil {
				r.config.Cache.unpin(cacheKey(e.source.Identity(), e.remote))
			}
		}
	}
	r.entries = next
	r.previous = previous
	return issues, nil
}

// Snapshot pins the current generation until Close. Refresh cannot change its
// metadata or revision choices. Obtain a fresh snapshot to adopt a refresh.
func (r *Registry) Snapshot() *SkillSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := &SkillSnapshot{entries: map[string]registryEntry{}, cache: r.config.Cache}
	for name, e := range r.entries {
		s.entries[name] = e
		if e.source != nil {
			s.cache.pin(cacheKey(e.source.Identity(), e.remote))
		}
	}
	return s
}

// Close releases the registry's current-generation pins. Existing snapshots
// remain usable. Call only after stopping refreshes; the registry can be reused.
func (r *Registry) Close() error {
	r.gate <- struct{}{}
	defer func() { <-r.gate }()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.source != nil {
			r.config.Cache.unpin(cacheKey(e.source.Identity(), e.remote))
		}
	}
	r.entries = map[string]registryEntry{}
	r.previous = map[string][]RemoteSkill{}
	return nil
}

type SkillSnapshot struct {
	mu      sync.RWMutex
	entries map[string]registryEntry
	cache   *SkillCache
	closed  bool
}

// List returns detached metadata in stable name order, without materialization.
func (s *SkillSnapshot) List() []Frontmatter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil
	}
	out := make([]Frontmatter, 0, len(s.entries))
	for name, e := range s.entries {
		if e.local != nil {
			out = append(out, Frontmatter{Name: name, Description: e.local.Description()})
		} else {
			out = append(out, Frontmatter{Name: name, Description: e.remote.Description})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get materializes only the requested remote revision. Local metadata and
// instructions are pinned, while resource files remain application-owned.
func (s *SkillSnapshot) Get(ctx context.Context, name string) (*Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, errors.New("skill snapshot closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e, ok := s.entries[name]
	if !ok {
		return nil, errors.New("skill not found")
	}
	if e.local != nil {
		copy := *e.local
		data, err := json.Marshal(e.local.Frontmatter)
		if err != nil {
			return nil, errors.New("invalid local skill metadata")
		}
		var front Frontmatter
		if err := json.Unmarshal(data, &front); err != nil {
			return nil, errors.New("invalid local skill metadata")
		}
		copy.Frontmatter = &front
		return &copy, nil
	}
	return s.cache.Materialize(ctx, e.source, e.remote)
}
func (s *SkillSnapshot) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	for _, e := range s.entries {
		if e.source != nil {
			s.cache.unpin(cacheKey(e.source.Identity(), e.remote))
		}
	}
	return nil
}
