// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package agentkit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/adk/v2/session"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func durableStore(t *testing.T, path string) AIOLifecycleStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path+"?_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sql, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sql.Close() })
	store, err := NewGormAIOLifecycleStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func TestLifecycleDurableLeaseRecoveryAndFencing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lifecycle.db")
	a, b := durableStore(t, path), durableStore(t, path)
	ctx := context.Background()
	r, err := a.Claim(ctx, "app", "user", "session", "first", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Claim(ctx, "app", "user", "session", "second", time.Second); !errors.Is(err, ErrAIOSessionBusy) {
		t.Fatalf("claim=%v", err)
	}
	r.LastActivity = time.Now().Add(-time.Hour)
	r.PendingFiles = []string{"/tmp/staged"}
	if err := a.Save(ctx, "first", r); err != nil {
		t.Fatal(err)
	}
	if err := a.Release(ctx, "app", "user", "session", "first"); err != nil {
		t.Fatal(err)
	}
	restored, err := b.Claim(ctx, "app", "user", "session", "second", time.Second)
	if err != nil || !restored.LastActivity.Equal(r.LastActivity) || len(restored.PendingFiles) != 1 {
		t.Fatalf("restore=%+v %v", restored, err)
	}
	if err := a.Save(ctx, "first", r); err == nil {
		t.Fatal("stale owner saved state")
	}
	if err := a.Release(ctx, "app", "user", "session", "first"); err == nil {
		t.Fatal("stale owner released newer lease")
	}
}
func TestLifecycleRenewalAndRestartSweep(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lifecycle.db")
	coordA, coordB := durableStore(t, path), durableStore(t, path)
	sessions := session.InMemoryService()
	newManager := func(store AIOLifecycleStore) *AIOLifecycle {
		m, err := NewAIOLifecycle(AIOLifecycleConfig{SessionService: sessions, Store: store, LeaseTTL: 3 * time.Second, IdleTimeout: time.Second, PausedRetention: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	a, b := newManager(coordA), newManager(coordB)
	entry := a.track("app", "user", "session")
	_, finish, err := a.acquire(ctx, entry)
	if err != nil {
		t.Fatal(err)
	}
	// Holding the operation beyond one TTL must not allow another replica in.
	time.Sleep(3500 * time.Millisecond)
	if _, _, err := b.acquire(ctx, b.track("app", "user", "session")); !errors.Is(err, ErrAIOSessionBusy) {
		t.Fatalf("lease expired during active request: %v", err)
	}
	entry.lastActivity = time.Now().Add(-time.Hour)
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	status := "Ready"
	identity := aioIdentity("app", "user", "session")
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		result := map[string]any{}
		switch r.URL.Query().Get("Action") {
		case "GetSession":
			result = map[string]any{"SessionId": "physical", "UserSessionId": aioPhysicalPrefix(identity, ""), "Status": status, "Endpoint": "https://sandbox.example"}
		case "PauseSession":
			status = "Paused"
		case "DeleteSession":
			status = "Deleted"
		default:
			t.Errorf("unexpected action %s", r.URL.Query().Get("Action"))
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"Result": result}); err != nil {
			t.Errorf("encode control-plane response: %v", err)
		}
	}))
	defer control.Close()
	u, _ := url.Parse(control.URL)
	for k, v := range map[string]string{"AGENTKIT_TOOL_ID": "tool-test", "VOLCENGINE_ACCESS_KEY": "test", "VOLCENGINE_SECRET_KEY": "test", "AGENTKIT_TOOL_SCHEME": "http", "AGENTKIT_TOOL_HOST": u.Host} {
		t.Setenv(k, v)
	}
	_, err = sessions.Create(ctx, &session.CreateRequest{AppName: "app", UserID: "user", SessionID: "session", State: map[string]any{AIOSandboxStateKey: bindingValue(AIOBinding{ToolID: "tool-test", SessionID: "physical", Identity: identity})}})
	if err != nil {
		t.Fatal(err)
	}
	restarted := newManager(coordB)
	restarted.Sweep(ctx)
	records, err := coordB.List(ctx)
	if err != nil || len(records) != 1 || records[0].PausedAt.IsZero() {
		t.Fatalf("restart did not pause: %+v %v", records, err)
	}
	entry = restarted.track("app", "user", "session")
	_, finish, err = restarted.acquire(ctx, entry)
	if err != nil {
		t.Fatal(err)
	}
	entry.pausedAt = time.Now().Add(-time.Hour)
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	newManager(coordA).Sweep(ctx)
	records, err = coordA.List(ctx)
	if err != nil || !records[0].Deleted {
		t.Fatalf("retention not recovered: %+v %v", records, err)
	}
	if _, err := sessions.Get(ctx, &session.GetRequest{AppName: "app", UserID: "user", SessionID: "session"}); err != nil {
		t.Fatal("sweep deleted ADK history")
	}
}

func TestLifecycleExpiredOwnerCannotRenewOrSave(t *testing.T) {
	store := durableStore(t, filepath.Join(t.TempDir(), "expire.db"))
	ctx := context.Background()
	r, err := store.Claim(ctx, "app", "user", "id", "old", 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Millisecond)
	if _, err := store.Claim(ctx, "app", "user", "id", "new", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Renew(ctx, "app", "user", "id", "old", time.Minute); err == nil {
		t.Fatal("expired owner renewed")
	}
	if err := store.Save(ctx, "old", r); err == nil {
		t.Fatal("expired owner overwrote state")
	}
}

type failingRenewStore struct{ AIOLifecycleStore }

func (failingRenewStore) Renew(context.Context, string, string, string, string, time.Duration) error {
	return errors.New("database unavailable")
}
func TestLifecycleLeaseLossCancelsOperation(t *testing.T) {
	store := failingRenewStore{durableStore(t, filepath.Join(t.TempDir(), "lost.db"))}
	m, err := NewAIOLifecycle(AIOLifecycleConfig{SessionService: session.InMemoryService(), Store: store, LeaseTTL: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, finish, err := m.acquire(context.Background(), m.track("app", "user", "id"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("lease loss did not cancel operation")
	}
	if err := finish(); err == nil {
		t.Fatal("lease failure not reported")
	}
}

func TestLifecycleDiscoversStoredBindingsAndJournalsUploadBeforeTransfer(t *testing.T) {
	ctx := context.Background()
	sessions := session.InMemoryService()
	created, err := sessions.Create(ctx, &session.CreateRequest{AppName: "app", UserID: "user", SessionID: "id", State: map[string]any{AIOSandboxStateKey: bindingValue(AIOBinding{ToolID: "tool", SessionID: "physical", Identity: aioIdentity("app", "user", "id")})}})
	if err != nil {
		t.Fatal(err)
	}
	store := durableStore(t, filepath.Join(t.TempDir(), "journal.db"))
	m, err := NewAIOLifecycle(AIOLifecycleConfig{SessionService: sessions, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	m.RegisterApp("app")
	if err := m.restore(ctx); err != nil {
		t.Fatal(err)
	}
	entry := m.tracked[aioTrackKey("app", "user", "id")]
	if entry == nil || !entry.lastActivity.Equal(created.Session.LastUpdateTime()) {
		t.Fatal("persisted binding was not discovered")
	}
	work, finish, err := m.acquire(ctx, entry)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := finish(); err != nil {
			t.Error(err)
		}
	}()
	if err := m.TrackFileCleanup(work, "/workspace/.veadk-upload-test"); err != nil {
		t.Fatal(err)
	}
	// Read through a separate store instance before the operation releases its
	// lease: a process crash now must not lose the pending upload cleanup.
	records, err := store.List(ctx)
	if err != nil || len(records) != 1 || len(records[0].PendingFiles) != 1 {
		t.Fatalf("cleanup not journaled before transfer: %+v %v", records, err)
	}
}
