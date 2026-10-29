// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0
package agentkit

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/volcengine/veadk-go/v2/log"
	"google.golang.org/adk/v2/session"
	"sync"
	"time"
)

type lifecycleContextKey struct{}
type lifecycleContext struct {
	manager *AIOLifecycle
	entry   *aioTrackedSession
	owner   string
	tools   *aioToolInvocation
}

type aioToolInvocation struct {
	mu    sync.Mutex
	state *aioMapState
}

func (e *aioTrackedSession) record() AIOLifecycleRecord {
	return AIOLifecycleRecord{App: e.app, User: e.user, Session: e.id, LastActivity: e.lastActivity, PausedAt: e.pausedAt, Deleted: e.deleted, PendingFiles: append([]string(nil), e.pendingFiles...)}
}
func (e *aioTrackedSession) load(r AIOLifecycleRecord) {
	e.lastActivity = r.LastActivity
	e.pausedAt = r.PausedAt
	e.deleted = r.Deleted
	e.pendingFiles = append([]string(nil), r.PendingFiles...)
}

// acquire takes the process lock first, then the durable renewable lease. The
// returned context is cancelled immediately if the lease can no longer renew.
func (m *AIOLifecycle) acquire(ctx context.Context, e *aioTrackedSession) (context.Context, func() error, error) {
	if !e.mu.TryLock() {
		return nil, nil, ErrAIOSessionBusy
	}
	if m.cfg.Store == nil {
		return context.WithValue(ctx, lifecycleContextKey{}, lifecycleContext{manager: m, entry: e, tools: &aioToolInvocation{}}), func() error { e.mu.Unlock(); return nil }, nil
	}
	owner := uuid.NewString()
	r, err := m.cfg.Store.Claim(ctx, e.app, e.user, e.id, owner, m.cfg.LeaseTTL)
	if err != nil {
		e.mu.Unlock()
		return nil, nil, err
	}
	if r.LastActivity.IsZero() {
		r.LastActivity = e.lastActivity
	}
	e.load(r)
	work, cancel := context.WithCancel(context.WithValue(ctx, lifecycleContextKey{}, lifecycleContext{manager: m, entry: e, owner: owner, tools: &aioToolInvocation{}}))
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		tick := time.NewTicker(m.cfg.LeaseTTL / 3)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				done <- nil
				return
			case <-work.Done():
				done <- work.Err()
				return
			case <-tick.C:
				renew, c := context.WithTimeout(work, m.cfg.LeaseTTL/3)
				err := m.cfg.Store.Renew(renew, e.app, e.user, e.id, owner, m.cfg.LeaseTTL)
				c()
				if err != nil {
					cancel()
					done <- err
					return
				}
			}
		}
	}()
	return work, func() error {
		close(stop)
		renewErr := <-done
		cancel()
		defer e.mu.Unlock()
		cleanup, c := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer c()
		saveErr := m.cfg.Store.Save(cleanup, owner, e.record())
		releaseErr := m.cfg.Store.Release(cleanup, e.app, e.user, e.id, owner)
		return errors.Join(renewErr, saveErr, releaseErr)
	}, nil
}
func finishLease(finish func() error, err *error) {
	if e := finish(); e != nil {
		*err = errors.Join(*err, e)
	}
}

func (m *AIOLifecycle) cleanupFiles(ctx context.Context, e *aioTrackedSession) {
	if len(e.pendingFiles) == 0 {
		return
	}
	s, err := GetADKSession(ctx, m.cfg.SessionService, e.app, e.user, e.id)
	if err != nil {
		return
	}
	client, err := m.client()
	if err != nil {
		return
	}
	endpoint, err := ResolveAIOEndpointFor(ctx, client, s.State(), e.app, e.user, e.id)
	if err != nil {
		return
	}
	files, err := NewAIOFiles(endpoint)
	if err != nil {
		return
	}
	defer files.Close()
	remaining := e.pendingFiles[:0]
	for _, p := range e.pendingFiles {
		if err := files.Delete(ctx, p); err != nil {
			remaining = append(remaining, p)
		}
	}
	e.pendingFiles = remaining
}

// RegisterApp enables discovery of persisted AIO bindings, including sessions
// created before the lifecycle registry was configured. It performs no I/O.
func (m *AIOLifecycle) RegisterApp(app string) { m.mu.Lock(); defer m.mu.Unlock(); m.apps[app] = true }
func (m *AIOLifecycle) restore(ctx context.Context) error {
	if m.cfg.Store != nil {
		records, err := m.cfg.Store.List(ctx)
		if err != nil {
			return err
		}
		for _, r := range records {
			m.track(r.App, r.User, r.Session)
		}
	}
	m.mu.Lock()
	apps := make([]string, 0, len(m.apps))
	for app := range m.apps {
		apps = append(apps, app)
	}
	m.mu.Unlock()
	for _, app := range apps {
		listed, err := m.cfg.SessionService.List(ctx, &session.ListRequest{AppName: app})
		if err != nil {
			return err
		}
		for _, s := range listed.Sessions {
			binding, err := bindingFromState(s.State())
			if err != nil {
				return err
			}
			if binding == nil {
				continue
			}
			m.mu.Lock()
			key := aioTrackKey(s.AppName(), s.UserID(), s.ID())
			if m.tracked[key] == nil {
				m.tracked[key] = &aioTrackedSession{app: s.AppName(), user: s.UserID(), id: s.ID(), lastActivity: s.LastUpdateTime()}
			}
			m.mu.Unlock()
		}
	}
	return nil
}
func lifecycleLog(err error) {
	if err != nil {
		log.Warnf("AIO lifecycle coordination failed: %v", err)
	}
}

// TrackFileCleanup journals the upload path before any bytes are sent. Call only
// with the context supplied by WithSandbox; a crash then leaves recoverable work.
func (m *AIOLifecycle) TrackFileCleanup(ctx context.Context, path string) error {
	held, ok := ctx.Value(lifecycleContextKey{}).(lifecycleContext)
	if !ok || held.manager != m {
		return errors.New("file cleanup requires an active sandbox operation")
	}
	for _, p := range held.entry.pendingFiles {
		if p == path {
			return nil
		}
	}
	held.entry.pendingFiles = append(held.entry.pendingFiles, path)
	if m.cfg.Store != nil {
		return m.cfg.Store.Save(ctx, held.owner, held.entry.record())
	}
	return nil
}

// ForgetFileCleanup retires a confirmed cleanup/commit under the active lease.
func (m *AIOLifecycle) ForgetFileCleanup(ctx context.Context, path string) {
	held, ok := ctx.Value(lifecycleContextKey{}).(lifecycleContext)
	if !ok || held.manager != m {
		return
	}
	remaining := held.entry.pendingFiles[:0]
	for _, p := range held.entry.pendingFiles {
		if p != path {
			remaining = append(remaining, p)
		}
	}
	held.entry.pendingFiles = remaining
}
