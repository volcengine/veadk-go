// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package agentkit

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/adk/v2/session"
)

// AIOLifecycleConfig configures AIO sandbox sessions. Persistent deployments
// must configure Store; every replica must share it and the ADK SessionService.
type AIOLifecycleConfig struct {
	SessionService session.Service
	// Store persists orchestration state and coordinates replicas. Nil is for
	// ephemeral single-process deployments with in-memory ADK sessions only.
	Store AIOLifecycleStore
	// LeaseTTL defaults to two minutes; leases renew every TTL/3.
	LeaseTTL      time.Duration
	SkillSpaceIDs []string
	// IdleTimeout controls time since the last completed request that used
	// the sandbox before pause. Ordinary chat does not extend this timeout.
	// Zero reads VEADK_AIO_IDLE_TIMEOUT, then defaults to five minutes.
	IdleTimeout time.Duration
	// PausedRetention controls time since confirmed pause before deletion.
	// Zero reads VEADK_AIO_PAUSED_RETENTION, then defaults to 24 hours.
	PausedRetention time.Duration
	// ScanInterval controls how often the background worker checks sessions.
	// Zero reads VEADK_AIO_SCAN_INTERVAL, then defaults to 30 seconds.
	ScanInterval time.Duration
	Client       *Client // Optional, primarily for embedding and tests.
}

type aioTrackedSession struct {
	app, user, id string
	lastActivity  time.Time
	pausedAt      time.Time
	deleted       bool
	pendingFiles  []string
	mu            sync.Mutex
}

// AIOLifecycle manages physical AgentKit sessions without owning ADK history.
// Store provides restart recovery and cross-replica leases. Without Store the
// lifecycle is deliberately ephemeral, like ADK InMemoryService.
type AIOLifecycle struct {
	cfg     AIOLifecycleConfig
	mu      sync.Mutex
	tracked map[string]*aioTrackedSession
	start   sync.Once
	apps    map[string]bool
}

func (m *AIOLifecycle) SkillSpaceIDs() []string {
	if m == nil {
		return nil
	}
	return append([]string(nil), m.cfg.SkillSpaceIDs...)
}

func (m *AIOLifecycle) SessionService() session.Service {
	if m == nil {
		return nil
	}
	return m.cfg.SessionService
}

var ErrAIOSessionExists = errors.New("ADK session already exists")

func NewAIOLifecycle(cfg AIOLifecycleConfig) (*AIOLifecycle, error) {
	if cfg.SessionService == nil {
		return nil, errors.New("AIO lifecycle requires SessionService")
	}
	if cfg.IdleTimeout < 0 || cfg.PausedRetention < 0 || cfg.ScanInterval < 0 {
		return nil, errors.New("AIO lifecycle durations cannot be negative")
	}
	if cfg.LeaseTTL == 0 {
		cfg.LeaseTTL = 2 * time.Minute
	}
	if cfg.LeaseTTL < 3*time.Second {
		return nil, errors.New("AIO LeaseTTL must be at least 3s")
	}
	var err error
	if cfg.IdleTimeout, err = aioDuration(cfg.IdleTimeout, "VEADK_AIO_IDLE_TIMEOUT", 5*time.Minute); err != nil {
		return nil, err
	}
	if cfg.PausedRetention, err = aioDuration(cfg.PausedRetention, "VEADK_AIO_PAUSED_RETENTION", 24*time.Hour); err != nil {
		return nil, err
	}
	if cfg.ScanInterval, err = aioDuration(cfg.ScanInterval, "VEADK_AIO_SCAN_INTERVAL", 30*time.Second); err != nil {
		return nil, err
	}
	cfg.SkillSpaceIDs = append([]string(nil), cfg.SkillSpaceIDs...)
	return &AIOLifecycle{cfg: cfg, tracked: make(map[string]*aioTrackedSession), apps: make(map[string]bool)}, nil
}

func aioDuration(explicit time.Duration, env string, fallback time.Duration) (time.Duration, error) {
	if explicit > 0 {
		return explicit, nil
	}
	value := strings.TrimSpace(os.Getenv(env))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration, such as 5m or 24h", env)
	}
	return duration, nil
}

func (m *AIOLifecycle) client() (*Client, error) {
	if m.cfg.Client != nil {
		return m.cfg.Client, nil
	}
	return NewClient(nil)
}

func (m *AIOLifecycle) sessionTTLSeconds() int {
	seconds := int((m.cfg.IdleTimeout + 5*time.Minute + time.Second - 1) / time.Second)
	if seconds < aioSessionTTL {
		return aioSessionTTL
	}
	return seconds
}

func aioTrackKey(app, user, id string) string {
	return app + "\x00" + user + "\x00" + id
}

func (m *AIOLifecycle) track(app, user, id string) *aioTrackedSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := aioTrackKey(app, user, id)
	if entry := m.tracked[key]; entry != nil {
		return entry
	}
	entry := &aioTrackedSession{app: app, user: user, id: id, lastActivity: time.Now()}
	m.tracked[key] = entry
	return entry
}

// Create creates a logical ADK session without allocating a sandbox.
func (m *AIOLifecycle) Create(ctx context.Context, app, user, id string, initialState map[string]any) (session.Session, error) {
	return m.CreateWithEvents(ctx, app, user, id, initialState, nil)
}

// CreateWithEvents holds the lifecycle lock through history import and rollback.
func (m *AIOLifecycle) CreateWithEvents(ctx context.Context, app, user, id string, initialState map[string]any, events []*session.Event) (result session.Session, err error) {
	if app == "" || user == "" {
		return nil, errors.New("app and user are required")
	}
	if _, ok := initialState[AIOSandboxStateKey]; ok {
		return nil, ErrAIOReservedState
	}
	for _, event := range events {
		if event == nil {
			return nil, errors.New("nil initial event")
		}
		if _, ok := event.Actions.StateDelta[AIOSandboxStateKey]; ok {
			return nil, ErrAIOReservedState
		}
	}
	if id == "" {
		id = uuid.NewString()
	}
	entry := m.track(app, user, id)
	work, finish, lockErr := m.acquire(ctx, entry)
	if lockErr != nil {
		return nil, lockErr
	}
	ctx = work
	defer finishLease(finish, &err)
	listed, err := m.cfg.SessionService.List(ctx, &session.ListRequest{AppName: app, UserID: user})
	if err != nil {
		return nil, fmt.Errorf("check ADK session ID: %w", err)
	}
	for _, existing := range listed.Sessions {
		if existing.ID() == id {
			return nil, ErrAIOSessionExists
		}
	}
	state := &aioMapState{values: maps.Clone(initialState)}
	if state.values == nil {
		state.values = map[string]any{}
	}
	created, err := m.cfg.SessionService.Create(ctx, &session.CreateRequest{AppName: app, UserID: user, SessionID: id, State: state.values})
	if err != nil {
		return nil, fmt.Errorf("create ADK session: %w", err)
	}
	for _, event := range events {
		if err := m.cfg.SessionService.AppendEvent(ctx, created.Session, event); err != nil {
			cleanup, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			_ = m.cfg.SessionService.Delete(cleanup, &session.DeleteRequest{AppName: app, UserID: user, SessionID: id})
			return nil, fmt.Errorf("import session event: %w", err)
		}
	}
	entry.lastActivity = time.Now()
	entry.deleted = false
	entry.pausedAt = time.Time{}
	return created.Session, nil
}

// EnsureReady validates and resumes or recreates the physical sandbox.
func (m *AIOLifecycle) EnsureReady(ctx context.Context, app, user, id string) (result session.Session, err error) {
	entry := m.track(app, user, id)
	work, finish, lockErr := m.acquire(ctx, entry)
	if lockErr != nil {
		return nil, lockErr
	}
	ctx = work
	defer finishLease(finish, &err)
	return m.ensureReadyLocked(ctx, entry)
}

func (m *AIOLifecycle) ensureReadyLocked(ctx context.Context, entry *aioTrackedSession) (session.Session, error) {
	value, err := GetADKSession(ctx, m.cfg.SessionService, entry.app, entry.user, entry.id)
	got := &session.GetResponse{Session: value}
	if err != nil {
		return nil, err
	}
	previous, err := bindingFromState(got.Session.State())
	if err != nil {
		return nil, err
	}
	client, err := m.client()
	if err != nil {
		return nil, err
	}
	next, err := ensureAIOBindingWithSkillsTTL(ctx, client, got.Session.State(), entry.app, entry.user, entry.id, m.cfg.SkillSpaceIDs, m.sessionTTLSeconds())
	if err != nil {
		return nil, err
	}
	if previous == nil || *previous != *next {
		// State().Set alone is not durable for ADK database services.
		event := &session.Event{ID: uuid.NewString(), Author: "veadk_lifecycle", Timestamp: time.Now()}
		event.Actions.StateDelta = map[string]any{AIOSandboxStateKey: bindingValue(*next)}
		if err := m.cfg.SessionService.AppendEvent(ctx, got.Session, event); err != nil {
			return nil, fmt.Errorf("persist AIO sandbox binding: %w", err)
		}
	}
	entry.pausedAt = time.Time{}
	entry.deleted = false
	entry.lastActivity = time.Now()
	return got.Session, nil
}

// WithSession serializes one invocation with pause, resume and deletion.
// Callers must keep the callback running until the SSE stream has ended.
// Prefer WithSessionContext for I/O so lease loss cancels that work.
func (m *AIOLifecycle) WithSession(ctx context.Context, app, user, id string, run func() error) error {
	return m.WithSessionContext(ctx, app, user, id, func(context.Context) error { return run() })
}

// WithSessionContext checks session existence and holds the lifecycle lease
// without starting or resuming a sandbox. It propagates lease loss to the handler.
func (m *AIOLifecycle) WithSessionContext(ctx context.Context, app, user, id string, run func(context.Context) error) (err error) {
	entry := m.track(app, user, id)
	work, finish, err := m.acquire(ctx, entry)
	if err != nil {
		return err
	}
	defer finishLease(finish, &err)
	if _, err = GetADKSession(work, m.cfg.SessionService, app, user, id); err != nil {
		return err
	}

	defer func() {
		// ADK persists tool state deltas on its own session snapshot. Flushing
		// only after run avoids making that snapshot stale mid-invocation.
		lc := work.Value(lifecycleContextKey{}).(lifecycleContext)
		if lc.tools.state == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(work), 10*time.Second)
		defer cancel()
		if m.cfg.Store != nil {
			if renewErr := m.cfg.Store.Renew(cleanup, entry.app, entry.user, entry.id, lc.owner, m.cfg.LeaseTTL); renewErr != nil {
				err = errors.Join(err, renewErr)
				return
			}
		}
		err = errors.Join(err, m.flushToolBinding(cleanup, entry, lc.tools.state))
	}()
	previousActivity := entry.lastActivity
	defer func() {
		// Only sandbox use extends its idle timeout, never ordinary chat.
		if !entry.lastActivity.Equal(previousActivity) {
			entry.lastActivity = time.Now()
		}
	}()
	return run(work)
}

// flushToolBinding also covers cancellation before ADK emits the tool result.
func (m *AIOLifecycle) flushToolBinding(ctx context.Context, entry *aioTrackedSession, state session.ReadonlyState) error {
	binding, err := bindingFromState(state)
	if err != nil || binding == nil {
		return err
	}
	s, err := GetADKSession(ctx, m.cfg.SessionService, entry.app, entry.user, entry.id)
	if err != nil {
		return err
	}
	previous, err := bindingFromState(s.State())
	if err != nil {
		return err
	}
	if previous != nil && *previous == *binding {
		return nil
	}
	event := &session.Event{ID: uuid.NewString(), Author: "veadk_lifecycle", Timestamp: time.Now()}
	event.Actions.StateDelta = map[string]any{AIOSandboxStateKey: bindingValue(*binding)}
	return m.cfg.SessionService.AppendEvent(ctx, s, event)
}

var ErrAIOSessionBusy = errors.New("AIO session is busy")

// Delete removes the physical sandbox before removing the ADK session.
func (m *AIOLifecycle) Delete(ctx context.Context, app, user, id string) (err error) {
	entry := m.track(app, user, id)
	work, finish, err := m.acquire(ctx, entry)
	if err != nil {
		return err
	}
	ctx = work
	defer finishLease(finish, &err)
	value, err := GetADKSession(ctx, m.cfg.SessionService, app, user, id)
	got := &session.GetResponse{Session: value}
	if err != nil {
		return err
	}
	binding, err := bindingFromState(got.Session.State())
	if err != nil {
		return err
	}
	if binding != nil {
		client, err := m.client()
		if err != nil {
			return err
		}
		if err := DeleteAIOBinding(ctx, client, got.Session.State(), app, user, id); err != nil {
			return err
		}
	}
	if err := m.cfg.SessionService.Delete(ctx, &session.DeleteRequest{AppName: app, UserID: user, SessionID: id}); err != nil {
		return err
	}
	entry.deleted = true
	entry.pendingFiles = nil
	return nil
}

// Start runs the idle lifecycle worker until ctx is cancelled. Store and
// registered apps restore sessions that have not been touched since startup.
func (m *AIOLifecycle) Start(ctx context.Context) {
	m.start.Do(func() {
		go func() {
			m.Sweep(ctx)
			ticker := time.NewTicker(m.cfg.ScanInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					m.Sweep(ctx)
				}
			}
		}()
	})
}

// Sweep performs one pass. Failed or ambiguous control-plane writes are
// revisited on the next pass after observing the actual session status.
func (m *AIOLifecycle) Sweep(ctx context.Context) {
	if err := m.restore(ctx); err != nil {
		lifecycleLog(err)
		return
	}
	m.mu.Lock()
	entries := make([]*aioTrackedSession, 0, len(m.tracked))
	for _, entry := range m.tracked {
		entries = append(entries, entry)
	}
	m.mu.Unlock()
	for _, entry := range entries {
		work, finish, err := m.acquire(ctx, entry)
		if err != nil {
			if !errors.Is(err, ErrAIOSessionBusy) {
				lifecycleLog(err)
			}
			continue
		}
		func() {
			ctx := work
			defer func() { lifecycleLog(finish()) }()
			if len(entry.pendingFiles) > 0 && !entry.deleted {
				m.cleanupFiles(ctx, entry)
			}
			now := time.Now()
			if entry.pausedAt.IsZero() && now.Sub(entry.lastActivity) < m.cfg.IdleTimeout {
				return
			}
			if entry.deleted {
				return
			}
			if !entry.pausedAt.IsZero() && now.Sub(entry.pausedAt) < m.cfg.PausedRetention {
				return
			}
			value, err := GetADKSession(ctx, m.cfg.SessionService, entry.app, entry.user, entry.id)
			got := &session.GetResponse{Session: value}
			if err != nil {
				return
			}
			binding, err := bindingFromState(got.Session.State())
			if err != nil || binding == nil {
				return
			}
			client, err := m.client()
			if err != nil {
				return
			}
			if entry.pausedAt.IsZero() {
				if err := PauseAIOBinding(ctx, client, got.Session.State(), entry.app, entry.user, entry.id); err == nil {
					entry.pausedAt = time.Now()
				}
				return
			}
			if err := DeleteAIOBinding(ctx, client, got.Session.State(), entry.app, entry.user, entry.id); err == nil {
				entry.pausedAt = time.Time{}
				entry.deleted = true
				entry.lastActivity = time.Now()
				// The old binding remains in ADK state until EnsureReady replaces
				// it. No old endpoint is used while it is absent.
			}
		}()
	}
}

type aioMapState struct{ values map[string]any }

func (s *aioMapState) Get(key string) (any, error) {
	v, ok := s.values[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return v, nil
}
func (s *aioMapState) Set(key string, value any) error { s.values[key] = value; return nil }
func (s *aioMapState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for k, v := range s.values {
			if !yield(k, v) {
				return
			}
		}
	}
}

// WithSandbox holds the session lock through the complete transfer and commit.
func (m *AIOLifecycle) WithSandbox(ctx context.Context, app, user, id string, run func(context.Context, session.Session, string) error) error {
	return m.WithSessionContext(ctx, app, user, id, func(ctx context.Context) error {
		lc := ctx.Value(lifecycleContextKey{}).(lifecycleContext)
		s, err := m.ensureReadyLocked(ctx, lc.entry)
		if err != nil {
			return err
		}
		client, err := m.client()
		if err != nil {
			return err
		}
		endpoint, err := ResolveAIOEndpointFor(ctx, client, s.State(), app, user, id)
		if err != nil {
			return err
		}
		m.cleanupFiles(ctx, lc.entry)
		defer func() { lc.entry.lastActivity = time.Now() }()
		return run(ctx, s, endpoint)
	})
}
