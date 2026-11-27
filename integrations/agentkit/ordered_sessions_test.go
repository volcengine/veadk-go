// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package agentkit

import (
	"context"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

func TestDatabaseAppendOrderSurvivesEqualTimestampsAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	base, err := database.NewSessionService(sqlite.Open(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(base); err != nil {
		t.Fatal(err)
	}
	store, err := NewOrderedSessionService(base, db)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s, err := store.Create(ctx, &session.CreateRequest{AppName: "app", UserID: "user", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	// Non-lexical IDs and a backwards timestamp rule out timestamp/ID sorting.
	stamp := time.Now().Add(time.Second)
	for i, id := range []string{"z", "a", "m"} {
		timestamp := stamp
		if i == 2 {
			timestamp = stamp.Add(-time.Second)
		}
		if err := store.AppendEvent(ctx, s.Session, &session.Event{ID: id, Author: "model", Timestamp: timestamp}); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := NewOrderedSessionService(base, db)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(ctx, &session.GetRequest{AppName: "app", UserID: "user", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"z", "a", "m"} {
		if got.Session.Events().At(i).ID != id {
			t.Fatalf("event %d=%s", i, got.Session.Events().At(i).ID)
		}
	}
	if err := reopened.AppendEvent(ctx, got.Session, &session.Event{ID: "next", Author: "model", Timestamp: stamp}); err != nil {
		t.Fatal(err)
	}
	if got.Session.Events().Len() != 4 {
		t.Fatal("returned session did not advance after append")
	}
	recent, err := reopened.Get(ctx, &session.GetRequest{AppName: "app", UserID: "user", SessionID: "session", NumRecentEvents: 2})
	if err != nil {
		t.Fatal(err)
	}
	if recent.Session.Events().At(0).ID != "m" || recent.Session.Events().At(1).ID != "next" {
		t.Fatal("recent events not in append order")
	}
}
