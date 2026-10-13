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

package runner

import (
	"context"
	"fmt"
	"iter"
	"sync"
	"time"

	"github.com/volcengine/veadk-go/v2/log"
	vememory "github.com/volcengine/veadk-go/v2/memory"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/session"
)

const autoSaveMemoryPluginName = "veadk_auto_save_memory"

func NewAutoSaveMemoryPlugin(memoryService memory.Service, policy *vememory.MemoryAutoSavePolicy) (*plugin.Plugin, error) {
	var mu sync.Mutex
	savedEventCounts := map[string]int{}
	return plugin.New(plugin.Config{
		Name: autoSaveMemoryPluginName,
		AfterRunCallback: func(ctx agent.InvocationContext) {
			if ctx == nil || ctx.Session() == nil {
				return
			}
			sess := ctx.Session()
			key := sess.AppName() + "\x00" + sess.UserID() + "\x00" + sess.ID()
			mu.Lock()
			start := savedEventCounts[key]
			current := sess.Events().Len()
			if start > current {
				start = 0
			}
			if start == current {
				mu.Unlock()
				return
			}
			mu.Unlock()
			incrementalSession := sessionWithEvents(sess, eventsFrom(sess.Events(), start, current))
			if memoryService != nil {
				if err := addSessionToMemory(ctx, memoryService, incrementalSession, policy); err != nil {
					log.Errorf("veadk auto-save memory failed: %v", err)
					return
				}
			} else if ctx.Memory() != nil {
				if err := ctx.Memory().AddSessionToMemory(ctx, incrementalSession); err != nil {
					log.Errorf("veadk auto-save memory failed: %v", err)
					return
				}
			}
			mu.Lock()
			if savedEventCounts[key] < current {
				savedEventCounts[key] = current
			}
			mu.Unlock()
		},
	})
}

func appendPluginIfMissing(plugins []*plugin.Plugin, pluginToAppend *plugin.Plugin) []*plugin.Plugin {
	if pluginToAppend == nil {
		return plugins
	}
	for _, existing := range plugins {
		if existing != nil && existing.Name() == pluginToAppend.Name() {
			return plugins
		}
	}
	return append(plugins, pluginToAppend)
}

func addSessionToMemory(ctx context.Context, memoryService memory.Service, sess session.Session, policy *vememory.MemoryAutoSavePolicy) error {
	if memoryService == nil {
		return fmt.Errorf("long-term memory is not enabled")
	}
	if sess == nil {
		return fmt.Errorf("session is required")
	}
	if policyService, ok := memoryService.(vememory.PolicyMemoryService); ok {
		return policyService.AddSessionToMemoryWithPolicy(ctx, sess, policy)
	}
	return memoryService.AddSessionToMemory(ctx, sess)
}

type memorySession struct {
	session.Session
	events session.Events
}

func (s *memorySession) Events() session.Events {
	return s.events
}

type memoryEvents struct {
	events []*session.Event
}

func (e *memoryEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, event := range e.events {
			if !yield(event) {
				return
			}
		}
	}
}

func (e *memoryEvents) Len() int {
	return len(e.events)
}

func (e *memoryEvents) At(i int) *session.Event {
	return e.events[i]
}

func sessionWithEvents(sess session.Session, events []*session.Event) session.Session {
	return &memorySession{
		Session: sess,
		events:  &memoryEvents{events: events},
	}
}

func eventsFrom(events session.Events, start, end int) []*session.Event {
	if start < 0 {
		start = 0
	}
	if end > events.Len() {
		end = events.Len()
	}
	result := make([]*session.Event, 0, end-start)
	for i := start; i < end; i++ {
		result = append(result, events.At(i))
	}
	return result
}

var _ session.Events = (*memoryEvents)(nil)
var _ session.Session = (*memorySession)(nil)

func (s *memorySession) LastUpdateTime() time.Time {
	return s.Session.LastUpdateTime()
}
