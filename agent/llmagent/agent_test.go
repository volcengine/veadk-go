// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0

package llmagent

import (
	"context"
	"sync/atomic"
	"testing"

	adkllmagent "google.golang.org/adk/v2/agent/llmagent"
)

type testAPIKeyProvider struct {
	calls atomic.Int32
}

func (p *testAPIKeyProvider) APIKey(context.Context) (string, error) {
	p.calls.Add(1)
	return "test-key", nil
}

func TestNewDoesNotResolveLazyAPIKey(t *testing.T) {
	for _, modelProvider := range []string{"openai", "ark"} {
		t.Run(modelProvider, func(t *testing.T) {
			provider := &testAPIKeyProvider{}
			_, err := New(&Config{
				Config:              adkllmagent.Config{Name: "lazy-agent-" + modelProvider},
				ModelName:           "test-model",
				ModelProvider:       modelProvider,
				ModelAPIBase:        "http://model.invalid/v1",
				ModelAPIKeyProvider: provider,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if got := provider.calls.Load(); got != 0 {
				t.Fatalf("provider calls during New() = %d, want 0", got)
			}
		})
	}
}
