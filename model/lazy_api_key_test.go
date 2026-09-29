// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0

package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type rotatingTestAPIKeyProvider struct {
	mu          sync.Mutex
	keys        []string
	index       int
	calls       atomic.Int32
	invalidates atomic.Int32
}

type testAPIKeyProviderFunc func(context.Context) (string, error)

func (f testAPIKeyProviderFunc) APIKey(ctx context.Context) (string, error) {
	return f(ctx)
}

func (p *rotatingTestAPIKeyProvider) APIKey(context.Context) (string, error) {
	p.calls.Add(1)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.keys) == 0 {
		return "", nil
	}
	return p.keys[p.index], nil
}

func (p *rotatingTestAPIKeyProvider) Invalidate() {
	p.invalidates.Add(1)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.index+1 < len(p.keys) {
		p.index++
	}
}

func TestOpenAIModelResolvesAPIKeyLazily(t *testing.T) {
	provider := &rotatingTestAPIKeyProvider{keys: []string{"lazy-key"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer lazy-key" {
			t.Errorf("Authorization = %q, want Bearer lazy-key", got)
		}
		if got := r.Header.Get("X-Test"); got != "value" {
			t.Errorf("X-Test = %q, want value", got)
		}
		_ = json.NewEncoder(w).Encode(mockOpenAIResponse("hello", "stop"))
	}))
	defer server.Close()

	llm, err := NewOpenAIModel(context.Background(), "test-model", &ClientConfig{
		APIKeyProvider: provider,
		BaseURL:        server.URL,
		ExtraHeaders:   map[string]string{"X-Test": "value", "Authorization": "must-not-win"},
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 0 {
		t.Fatal("provider called during model construction")
	}
	if err := consumeModelResponse(llm, false); err != nil {
		t.Fatal(err)
	}
	if got := provider.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestOpenAIModelInvalidatesAndRetriesAuthFailureOnce(t *testing.T) {
	provider := &rotatingTestAPIKeyProvider{keys: []string{"expired-key", "rotated-key"}}
	var requests atomic.Int32
	var addressMu sync.Mutex
	var remoteAddresses []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addressMu.Lock()
		remoteAddresses = append(remoteAddresses, r.RemoteAddr)
		addressMu.Unlock()
		request := requests.Add(1)
		if request == 1 {
			if got := r.Header.Get("Authorization"); got != "Bearer expired-key" {
				t.Errorf("first Authorization = %q", got)
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("expired"))
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer rotated-key" {
			t.Errorf("retry Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(mockOpenAIResponse("retried", "stop"))
	}))
	defer server.Close()

	llm, err := NewOpenAIModel(context.Background(), "test-model", &ClientConfig{
		APIKeyProvider: provider, BaseURL: server.URL, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumeModelResponse(llm, false); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || provider.calls.Load() != 2 || provider.invalidates.Load() != 1 {
		t.Fatalf("requests/provider calls/invalidates = %d/%d/%d, want 2/2/1", requests.Load(), provider.calls.Load(), provider.invalidates.Load())
	}
	addressMu.Lock()
	defer addressMu.Unlock()
	if len(remoteAddresses) != 2 || remoteAddresses[0] != remoteAddresses[1] {
		t.Fatalf("request connections = %v, want one reused connection", remoteAddresses)
	}
}

func TestOpenAIModelRetriesAuthFailureForStream(t *testing.T) {
	provider := &rotatingTestAPIKeyProvider{keys: []string{"expired-key", "rotated-key"}}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		writeOpenAIStream(t, w, "streamed")
	}))
	defer server.Close()

	llm, err := NewOpenAIModel(context.Background(), "test-model", &ClientConfig{
		APIKeyProvider: provider, BaseURL: server.URL, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumeModelResponse(llm, true); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || provider.invalidates.Load() != 1 {
		t.Fatalf("requests/invalidates = %d/%d, want 2/1", requests.Load(), provider.invalidates.Load())
	}
}

func TestOpenAIModelStaticKeyTakesPriority(t *testing.T) {
	provider := &rotatingTestAPIKeyProvider{keys: []string{"must-not-be-used"}}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer static-key" {
			t.Errorf("Authorization = %q, want static key", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	llm, err := NewOpenAIModel(context.Background(), "test-model", &ClientConfig{
		APIKey: "static-key", APIKeyProvider: provider, BaseURL: server.URL, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumeModelResponse(llm, false); err == nil {
		t.Fatal("GenerateContent() error = nil")
	}
	if requests.Load() != 1 || provider.calls.Load() != 0 || provider.invalidates.Load() != 0 {
		t.Fatalf("requests/provider calls/invalidates = %d/%d/%d, want 1/0/0", requests.Load(), provider.calls.Load(), provider.invalidates.Load())
	}
}

func TestOpenAIModelBoundsErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", maxAPIErrorBodyBytes+1024)))
	}))
	defer server.Close()
	llm, err := NewOpenAIModel(context.Background(), "test-model", &ClientConfig{
		APIKey: "key", BaseURL: server.URL, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	err = consumeModelResponse(llm, false)
	if err == nil || !strings.Contains(err.Error(), "[truncated]") {
		t.Fatalf("GenerateContent() error = %v, want truncated marker", err)
	}
}

func TestArkModelResolvesAPIKeyLazily(t *testing.T) {
	provider := &rotatingTestAPIKeyProvider{keys: []string{"lazy-key"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer lazy-key" {
			t.Errorf("Authorization = %q, want Bearer lazy-key", got)
		}
		if got := r.Header.Get("X-Test"); got != "value" {
			t.Errorf("X-Test = %q, want value", got)
		}
		writeArkResponse(w, "hello")
	}))
	defer server.Close()

	llm, err := NewArkModel(context.Background(), "test-model", &ArkClientConfig{
		APIKeyProvider: provider,
		BaseURL:        server.URL,
		ExtraHeaders:   map[string]string{"X-Test": "value", "Authorization": "must-not-win"},
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 0 {
		t.Fatal("provider called during model construction")
	}
	if err := consumeModelResponse(llm, false); err != nil {
		t.Fatal(err)
	}
	if got := provider.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestArkModelInvalidatesAndRetriesAuthFailureOnce(t *testing.T) {
	provider := &rotatingTestAPIKeyProvider{keys: []string{"expired-key", "rotated-key"}}
	var requests atomic.Int32
	var addressMu sync.Mutex
	var remoteAddresses []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addressMu.Lock()
		remoteAddresses = append(remoteAddresses, r.RemoteAddr)
		addressMu.Unlock()
		request := requests.Add(1)
		if request == 1 {
			if got := r.Header.Get("Authorization"); got != "Bearer expired-key" {
				t.Errorf("first Authorization = %q", got)
			}
			writeArkError(w, http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer rotated-key" {
			t.Errorf("retry Authorization = %q", got)
		}
		writeArkResponse(w, "retried")
	}))
	defer server.Close()

	llm, err := NewArkModel(context.Background(), "test-model", &ArkClientConfig{
		APIKeyProvider: provider, BaseURL: server.URL, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumeModelResponse(llm, false); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || provider.calls.Load() != 2 || provider.invalidates.Load() != 1 {
		t.Fatalf("requests/provider calls/invalidates = %d/%d/%d, want 2/2/1", requests.Load(), provider.calls.Load(), provider.invalidates.Load())
	}
	addressMu.Lock()
	defer addressMu.Unlock()
	if len(remoteAddresses) != 2 || remoteAddresses[0] != remoteAddresses[1] {
		t.Fatalf("request connections = %v, want one reused connection", remoteAddresses)
	}
}

func TestArkModelRetriesAuthFailureForStream(t *testing.T) {
	provider := &rotatingTestAPIKeyProvider{keys: []string{"expired-key", "rotated-key"}}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			writeArkError(w, http.StatusForbidden)
			return
		}
		writeArkStream(t, w, "streamed")
	}))
	defer server.Close()

	llm, err := NewArkModel(context.Background(), "test-model", &ArkClientConfig{
		APIKeyProvider: provider, BaseURL: server.URL, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumeModelResponse(llm, true); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || provider.invalidates.Load() != 1 {
		t.Fatalf("requests/invalidates = %d/%d, want 2/1", requests.Load(), provider.invalidates.Load())
	}
}

func TestArkModelStaticKeyTakesPriority(t *testing.T) {
	provider := &rotatingTestAPIKeyProvider{keys: []string{"must-not-be-used"}}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer static-key" {
			t.Errorf("Authorization = %q, want static key", got)
		}
		writeArkError(w, http.StatusUnauthorized)
	}))
	defer server.Close()

	llm, err := NewArkModel(context.Background(), "test-model", &ArkClientConfig{
		APIKey: "static-key", APIKeyProvider: provider, BaseURL: server.URL, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := consumeModelResponse(llm, false); err == nil {
		t.Fatal("GenerateContent() error = nil")
	}
	if requests.Load() != 1 || provider.calls.Load() != 0 || provider.invalidates.Load() != 0 {
		t.Fatalf("requests/provider calls/invalidates = %d/%d/%d, want 1/0/0", requests.Load(), provider.calls.Load(), provider.invalidates.Load())
	}
}

func TestArkModelDirectAKSKTakesPriorityOverAPIKeyProvider(t *testing.T) {
	provider := &rotatingTestAPIKeyProvider{keys: []string{"must-not-be-used"}}
	llm, err := NewArkModel(context.Background(), "test-model", &ArkClientConfig{
		AK: "static-ak", SK: "static-sk", APIKeyProvider: provider,
	})
	if err != nil {
		t.Fatal(err)
	}
	arkLLM := llm.(*arkModel)
	client, _, err := arkLLM.clientForRequest(context.Background())
	if err != nil || client == nil {
		t.Fatalf("clientForRequest() = %v, %v", client, err)
	}
	if provider.calls.Load() != 0 || provider.invalidates.Load() != 0 {
		t.Fatalf("provider calls/invalidates = %d/%d, want 0/0", provider.calls.Load(), provider.invalidates.Load())
	}
}

func TestLazyModelsRetryPersistentAuthFailureAtMostOnce(t *testing.T) {
	tests := []struct {
		name string
		new  func(*httptest.Server, APIKeyProvider) (adkmodel.LLM, error)
	}{
		{
			name: "openai",
			new: func(server *httptest.Server, provider APIKeyProvider) (adkmodel.LLM, error) {
				return NewOpenAIModel(context.Background(), "test-model", &ClientConfig{
					APIKeyProvider: provider, BaseURL: server.URL, HTTPClient: server.Client(),
				})
			},
		},
		{
			name: "ark",
			new: func(server *httptest.Server, provider APIKeyProvider) (adkmodel.LLM, error) {
				return NewArkModel(context.Background(), "test-model", &ArkClientConfig{
					APIKeyProvider: provider, BaseURL: server.URL, HTTPClient: server.Client(),
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &rotatingTestAPIKeyProvider{keys: []string{"first-key", "second-key", "must-not-be-used"}}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				writeArkError(w, http.StatusUnauthorized)
			}))
			defer server.Close()

			llm, err := test.new(server, provider)
			if err != nil {
				t.Fatal(err)
			}
			if err := consumeModelResponse(llm, false); err == nil {
				t.Fatal("GenerateContent() error = nil")
			}
			if requests.Load() != 2 || provider.calls.Load() != 2 || provider.invalidates.Load() != 1 {
				t.Fatalf("requests/provider calls/invalidates = %d/%d/%d, want 2/2/1", requests.Load(), provider.calls.Load(), provider.invalidates.Load())
			}
		})
	}
}

func TestLazyModelsPropagateCancellationToAPIKeyProvider(t *testing.T) {
	provider := testAPIKeyProviderFunc(func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	tests := []struct {
		name string
		new  func() (adkmodel.LLM, error)
	}{
		{
			name: "openai",
			new: func() (adkmodel.LLM, error) {
				return NewOpenAIModel(context.Background(), "test-model", &ClientConfig{
					APIKeyProvider: provider, BaseURL: "http://model.invalid/v1",
				})
			},
		},
		{
			name: "ark",
			new: func() (adkmodel.LLM, error) {
				return NewArkModel(context.Background(), "test-model", &ArkClientConfig{
					APIKeyProvider: provider, BaseURL: "http://model.invalid/v1",
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			llm, err := test.new()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var responseErr error
			for _, err := range llm.GenerateContent(ctx, &adkmodel.LLMRequest{Contents: genai.Text("hi")}, false) {
				responseErr = err
			}
			if responseErr == nil || !strings.Contains(responseErr.Error(), context.Canceled.Error()) {
				t.Fatalf("GenerateContent() error = %v, want context canceled", responseErr)
			}
		})
	}
}

func consumeModelResponse(llm adkmodel.LLM, stream bool) error {
	var responseErr error
	for _, err := range llm.GenerateContent(context.Background(), &adkmodel.LLMRequest{Contents: genai.Text("hi")}, stream) {
		if err != nil {
			responseErr = err
		}
	}
	return responseErr
}

func writeArkResponse(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "chatcmpl-test", "model": "test-model",
		"choices": []map[string]any{{
			"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
}

func writeArkError(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": "AuthenticationError", "message": "expired", "type": "auth"},
	})
}

func writeOpenAIStream(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: {\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", content)
	_, _ = fmt.Fprint(w, "data: {\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func writeArkStream(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-test\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%q}}]}\n\n", content)
	_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-test\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}
