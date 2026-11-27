// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0
package agentkit

import (
	"context"
	"errors"
	"google.golang.org/adk/v2/session"
)

var ErrAIOSessionNotFound = errors.New("ADK session not found")
var ErrAIOReservedState = errors.New("sandbox binding is server-owned")

// GetADKSession distinguishes missing sessions from unavailable storage without
// matching backend-specific error strings (ADK has no public not-found sentinel).
func GetADKSession(ctx context.Context, store session.Service, app, user, id string) (session.Session, error) {
	got, err := store.Get(ctx, &session.GetRequest{AppName: app, UserID: user, SessionID: id})
	if err == nil && got != nil && got.Session != nil {
		return got.Session, nil
	}
	listed, listErr := store.List(ctx, &session.ListRequest{AppName: app, UserID: user})
	if listErr != nil {
		return nil, listErr
	}
	for _, s := range listed.Sessions {
		if s.ID() == id {
			if err != nil {
				return nil, err
			}
			return nil, errors.New("session store returned an empty session")
		}
	}
	return nil, ErrAIOSessionNotFound
}
