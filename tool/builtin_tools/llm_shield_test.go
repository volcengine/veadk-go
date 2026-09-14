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

package builtin_tools

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine/veadk-go/auth/veauth"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

const shieldPermit = `{"Result":{"Decision":{"DecisionType":1}}}`
const shieldBlock = `{"Result":{"Decision":{"DecisionType":2},"RiskInfo":{"Risks":[{"Category":104}]}}}`

// Embedded interfaces supply the unused ADK metadata methods. Calls to those
// methods would panic, ensuring moderation depends only on request context.
type shieldCallbackContext struct {
	agent.CallbackContext
	context.Context
}

func (c shieldCallbackContext) Deadline() (time.Time, bool) { return c.Context.Deadline() }
func (c shieldCallbackContext) Done() <-chan struct{}       { return c.Context.Done() }
func (c shieldCallbackContext) Err() error                  { return c.Context.Err() }
func (c shieldCallbackContext) Value(key any) any           { return c.Context.Value(key) }

type shieldToolContext struct {
	tool.Context
	base context.Context
}

func (c shieldToolContext) Deadline() (time.Time, bool) { return c.base.Deadline() }
func (c shieldToolContext) Done() <-chan struct{}       { return c.base.Done() }
func (c shieldToolContext) Err() error                  { return c.base.Err() }
func (c shieldToolContext) Value(key any) any           { return c.base.Value(key) }

func shieldTestClient(t *testing.T, handler http.HandlerFunc, policy LLMShieldFailurePolicy) *LLMShieldClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := NewLLMShieldClientWithConfig(LLMShieldConfig{AppID: "test-app", APIKey: "test-key", URL: server.URL, FailurePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLLMShieldPythonDecisions(t *testing.T) {
	cases := []struct {
		name, body, want string
		wantErr          bool
	}{
		{"permit", shieldPermit, "", false},
		{"block numeric", shieldBlock, "Prompt Injection", false},
		{"block strings", `{"Result":{"Decision":{"DecisionType":"2"},"RiskInfo":{"Risks":[{"Category":"103"},{"Category":"103"},{"Category":999}]}}}`, "Category 999, Sensitive Information", false},
		{"no risks permits like Python", `{"Result":{"Decision":{"DecisionType":2}}}`, "", false},
		{"empty risks permits like Python", `{"Result":{"Decision":{"DecisionType":2},"RiskInfo":{"Risks":[]}}}`, "", false},
		{"no category", `{"Result":{"Decision":{"DecisionType":2},"RiskInfo":{"Risks":[{}]}}}`, "security policy violation", false},
		{"untrusted category", `{"Result":{"Decision":{"DecisionType":2},"RiskInfo":{"Risks":[null,{"Category":"secret-marker"}]}}}`, "security policy violation", false},
		{"degraded", `{"Result":{"Degraded":true,"DegradeReason":"secret-marker"}}`, "", true},
		{"degraded block", `{"Result":{"Degraded":true,"Decision":{"DecisionType":2},"RiskInfo":{"Risks":[{"Category":104}]}}}`, "Prompt Injection", false},
		{"missing result", `{}`, "", true}, {"null", `null`, "", true},
		{"nil decision", `{"Result":{}}`, "", true},
		{"bad json", `secret-marker`, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := shieldDecision([]byte(tc.body))
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if tc.want == "" && got != "" || tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("block = %q", got)
			}
			if strings.Contains(fmt.Sprint(got, err), "secret-marker") {
				t.Fatal("response content leaked")
			}
		})
	}
}

func TestLLMShieldConfigAndEnvironment(t *testing.T) {
	var reads atomic.Int32
	source := veauth.CredentialSourceFunc(func(context.Context) (veauth.VeIAMCredential, error) {
		reads.Add(1)
		return veauth.VeIAMCredential{}, errors.New("secret-marker")
	})
	p, err := NewLLMShieldPlugin(LLMShieldConfig{CredentialSource: source, URL: "invalid", FailurePolicy: "invalid"})
	if err != nil || p != nil || reads.Load() != 0 {
		t.Fatal("disabled plugin has side effects")
	}
	for _, value := range []string{"", "false", "0", "off", "unexpected"} {
		t.Setenv("ENABLE_LLM_SHIELD", value)
		t.Setenv("TOOL_LLM_SHIELD_APP_ID", "")
		ps, err := LLMShieldPluginsFromEnv()
		if err != nil || len(ps) != 0 {
			t.Fatal("disabled environment initialized plugin")
		}
	}
	for _, value := range []string{"1", " TRUE ", "yes", "On"} {
		t.Setenv("ENABLE_LLM_SHIELD", value)
		t.Setenv("TOOL_LLM_SHIELD_APP_ID", " ")
		if _, err := LLMShieldPluginsFromEnv(); !errors.Is(err, ErrInvalidAppID) {
			t.Fatal(err)
		}
		t.Setenv("TOOL_LLM_SHIELD_APP_ID", "test-app")
		t.Setenv("TOOL_LLM_SHIELD_URL", "http://127.0.0.1:1")
		ps, err := LLMShieldPluginsFromEnv()
		if err != nil || len(ps) != 1 {
			t.Fatalf("enabled = %v, %v", ps, err)
		}
		p := ps[0]
		if p.BeforeModelCallback() == nil || p.AfterModelCallback() != nil || p.BeforeToolCallback() != nil || p.AfterToolCallback() != nil {
			t.Fatal("default callback scope differs from Python Sandbox")
		}
	}
	p, err = NewLLMShieldPlugin(LLMShieldConfig{Enabled: true, AppID: "test", CredentialSource: source, Callbacks: []LLMShieldCallback{LLMShieldAfterModel, LLMShieldBeforeTool, LLMShieldAfterTool}})
	if err != nil || reads.Load() != 0 || p.BeforeModelCallback() != nil || p.AfterModelCallback() == nil || p.BeforeToolCallback() == nil || p.AfterToolCallback() == nil {
		t.Fatal("explicit callback config failed")
	}
	legacy, err := NewLLMShieldPlugins()
	if err != nil || legacy.BeforeModelCallback() == nil || legacy.AfterModelCallback() == nil || legacy.BeforeToolCallback() == nil || legacy.AfterToolCallback() == nil {
		t.Fatal("legacy callback compatibility")
	}
	for _, cfg := range []LLMShieldConfig{
		{AppID: " "}, {AppID: "test", URL: "https://secret-marker@example.com"}, {AppID: "test", URL: "http://example.com/?key=secret-marker"}, {AppID: "test", URL: "file:///secret-marker"}, {AppID: "test", Timeout: -1}, {AppID: "test", FailurePolicy: "secret-marker"},
	} {
		if _, err := NewLLMShieldClientWithConfig(cfg); err == nil || strings.Contains(err.Error(), "secret-marker") {
			t.Fatalf("config validation = %v", err)
		}
	}
	if _, err := NewLLMShieldPlugin(LLMShieldConfig{Enabled: true, AppID: "test", Callbacks: []LLMShieldCallback{"secret-marker"}}); err == nil || strings.Contains(err.Error(), "secret-marker") {
		t.Fatal("scope validation")
	}
}

func TestLLMShieldHTTPProtocolAndAPIKeyPrecedence(t *testing.T) {
	var reads atomic.Int32
	c := shieldTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v2/moderate" || r.URL.Query().Get("Action") != "Moderate" || r.URL.Query().Get("Version") != "2025-08-31" {
			t.Error("incorrect endpoint")
		}
		if r.Header.Get("x-api-key") != "test-key" || r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("incorrect API key headers")
		}
		var payload struct {
			Scene   string
			Message struct {
				Role, Content string
				ContentType   int
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Scene != "test-app" || payload.Message.Role != "user" || payload.Message.Content != "hello" || payload.Message.ContentType != 1 {
			t.Error("incorrect Python payload")
		}
		_, _ = io.WriteString(w, shieldBlock)
	}, LLMShieldFailOpen)
	c.CredentialSource = veauth.CredentialSourceFunc(func(context.Context) (veauth.VeIAMCredential, error) {
		reads.Add(1)
		return veauth.VeIAMCredential{}, errors.New("must not read")
	})
	result, err := c.Moderate(context.Background(), "hello", "user")
	if err != nil || !strings.Contains(result, "Prompt Injection") || reads.Load() != 0 {
		t.Fatalf("result = %q, %v; reads=%d", result, err, reads.Load())
	}
}

func TestLLMShieldFailurePolicies(t *testing.T) {
	for _, signed := range []bool{false, true} {
		for _, mode := range []string{"401", "500", "malformed", "degraded", "oversized", "timeout", "transport", "read", "nil response"} {
			t.Run(fmt.Sprintf("signed=%v/%s", signed, mode), func(t *testing.T) {
				c := shieldTestClient(t, func(w http.ResponseWriter, r *http.Request) {
					switch mode {
					case "401":
						w.WriteHeader(401)
						_, _ = io.WriteString(w, "secret-marker")
					case "500":
						w.WriteHeader(500)
						_, _ = io.WriteString(w, "secret-marker")
					case "malformed":
						_, _ = io.WriteString(w, "secret-marker")
					case "degraded":
						_, _ = io.WriteString(w, `{"Result":{"Degraded":true,"DegradeReason":"secret-marker"}}`)
					case "oversized":
						_, _ = io.WriteString(w, strings.Repeat("x", maxShieldResponseBytes+1))
					case "timeout":
						_, _ = io.Copy(io.Discard, r.Body)
						<-r.Context().Done()
					}
				}, LLMShieldFailOpen)
				if signed {
					c.APIKey = ""
					c.CredentialSource = veauth.NewRoleCredentialSource(veauth.RoleCredentialSourceConfig{AccessKeyID: "test-ak", SecretAccessKey: "test-sk"})
				}
				if mode == "nil response" {
					c.HTTPClient = &http.Client{Transport: shieldRoundTripper(func(*http.Request) (*http.Response, error) { return nil, nil })}
				}
				if mode == "timeout" {
					c.timeout = 20 * time.Millisecond
				}
				if mode == "transport" {
					c.HTTPClient = &http.Client{Transport: shieldRoundTripper(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-marker") })}
				}
				if mode == "read" {
					c.HTTPClient = &http.Client{Transport: shieldRoundTripper(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: 200, Body: io.NopCloser(shieldErrorReader{})}, nil
					})}
				}
				ctx := shieldCallbackContext{Context: context.Background()}
				req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("secret-prompt", "user")}}
				if res, err := c.beforeModelCallBack(ctx, req); res != nil || err != nil {
					t.Fatalf("fail-open = %v, %v", res, err)
				}
				c.failurePolicy = LLMShieldFailClose
				if res, err := c.beforeModelCallBack(ctx, req); res != nil || err == nil || strings.Contains(err.Error(), "secret") {
					t.Fatalf("fail-close = %v, %v", res, err)
				}
			})
		}
	}
}

type shieldRoundTripper func(*http.Request) (*http.Response, error)

func (f shieldRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type shieldErrorReader struct{}

func (shieldErrorReader) Read([]byte) (int, error) { return 0, errors.New("secret-marker") }

func TestLLMShieldCallbackPythonScopeAndNil(t *testing.T) {
	var messages []string
	c := shieldTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Message struct{ Content, Role string }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		messages = append(messages, body.Message.Role+":"+body.Message.Content)
		_, _ = io.WriteString(w, shieldBlock)
	}, LLMShieldFailOpen)
	ctx := shieldCallbackContext{Context: context.Background()}
	for _, req := range []*model.LLMRequest{nil, {}, {Contents: []*genai.Content{nil}}, {Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{nil}}}}, {Contents: []*genai.Content{genai.NewContentFromText("tool result", "tool")}}} {
		if res, err := c.beforeModelCallBack(ctx, req); res != nil || err != nil {
			t.Fatal("empty input not skipped")
		}
	}
	for _, resp := range []*model.LLMResponse{nil, {}, {Content: &genai.Content{Role: "model", Parts: []*genai.Part{nil}}}} {
		if res, err := c.afterModelCallBack(ctx, resp, nil); res != nil || err != nil {
			t.Fatal("nil response not skipped")
		}
	}
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("old", "user"), {Role: "user", Parts: []*genai.Part{{Text: "first"}, {Text: "second"}}}}}
	res, err := c.beforeModelCallBack(ctx, req)
	if err != nil || res == nil || res.Partial || res.Content.Role != "model" {
		t.Fatal("block must be final model response")
	}
	_, _ = c.afterModelCallBack(ctx, &model.LLMResponse{Content: genai.NewContentFromText("output", "model")}, nil)
	tc := shieldToolContext{base: context.Background()}
	result, err := c.beforeToolCallback(tc, nil, map[string]any{"b": "two", "a": "one"})
	if err != nil || result["result"] == nil {
		t.Fatal("tool was not blocked")
	}
	result, err = c.afterToolCallback(tc, nil, nil, map[string]any{"value": "result"}, nil)
	if err != nil || result["result"] == nil {
		t.Fatal("tool result was not blocked")
	}
	want := []string{"user:first", "assistant:output", "user:a: one\nb: two", "assistant:result\n"}
	if fmt.Sprint(messages) != fmt.Sprint(want) {
		t.Fatalf("messages = %q", messages)
	}
	original := errors.New("original")
	if _, err := c.afterModelCallBack(ctx, nil, original); err != original {
		t.Fatal("lost model error")
	}
	if _, err := c.afterToolCallback(tc, nil, nil, nil, original); err != original {
		t.Fatal("lost tool error")
	}
}

func TestLLMShieldCancellationAndRedirect(t *testing.T) {
	started := make(chan struct{})
	c := shieldTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}, LLMShieldFailOpen)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.check(ctx, "input", "user"); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not propagate")
	}
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	for _, key := range []string{"test-key", ""} {
		c, err := NewLLMShieldClientWithConfig(LLMShieldConfig{AppID: "test", APIKey: key, URL: redirect.URL, CredentialSource: veauth.NewRoleCredentialSource(veauth.RoleCredentialSourceConfig{AccessKeyID: "test-ak", SecretAccessKey: "test-sk"})})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.Moderate(context.Background(), "secret-prompt", "user"); err == nil {
			t.Fatal("redirect allowed")
		}
	}
	if redirected.Load() != 0 {
		t.Fatal("credentials/prompt followed redirect")
	}
}

func TestLLMShieldConcurrentRoleRotationAndSignature(t *testing.T) {
	type requestKey struct{}
	var reads atomic.Int32
	source := veauth.CredentialSourceFunc(func(ctx context.Context) (veauth.VeIAMCredential, error) {
		reads.Add(1)
		id := ctx.Value(requestKey{}).(string)
		return veauth.VeIAMCredential{AccessKeyID: "test-ak-" + id, SecretAccessKey: "test-sk", SessionToken: "test-token-" + id}, nil
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct{ Message struct{ Content string } }
		_ = json.Unmarshal(body, &payload)
		id := payload.Message.Content
		if r.Header.Get("X-Security-Token") != "test-token-"+id || r.Header.Get("X-Top-Service") != "llmshield" || r.Header.Get("X-Top-Region") != "cn-beijing" || r.Header.Get("x-api-key") != "" {
			t.Error("Role headers mixed")
		}
		// Verify the canonical signature independently from VeRequest, including the
		// actual payload, query, host, date, region and service on the wire.
		hash := func(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
		mac := func(key []byte, value string) []byte {
			h := hmac.New(sha256.New, key)
			_, _ = h.Write([]byte(value))
			return h.Sum(nil)
		}
		date := r.Header.Get("X-Date")
		if len(date) < 8 {
			t.Error("missing date")
			w.WriteHeader(400)
			return
		}
		signed := "host;x-date;x-content-sha256;content-type"
		headers := "host:" + r.Host + "\nx-date:" + date + "\nx-content-sha256:" + hash(body) + "\ncontent-type:application/json\n"
		canonical := strings.Join([]string{"POST", "/v2/moderate", r.URL.RawQuery, headers, signed, hash(body)}, "\n")
		scope := date[:8] + "/cn-beijing/llmshield/request"
		key := mac(mac(mac(mac([]byte("test-sk"), date[:8]), "cn-beijing"), "llmshield"), "request")
		signature := hex.EncodeToString(mac(key, "HMAC-SHA256\n"+date+"\n"+scope+"\n"+hash([]byte(canonical))))
		want := "HMAC-SHA256 Credential=test-ak-" + id + "/" + scope + ", SignedHeaders=" + signed + ", Signature=" + signature
		if r.Header.Get("Authorization") != want {
			t.Error("incorrect signature")
		}
		if id == "block" {
			_, _ = io.WriteString(w, shieldBlock)
		} else {
			_, _ = io.WriteString(w, shieldPermit)
		}
	}))
	defer server.Close()
	c, err := NewLLMShieldClientWithConfig(LLMShieldConfig{AppID: "test", URL: server.URL, CredentialSource: source})
	if err != nil || reads.Load() != 0 {
		t.Fatal("construction read credentials")
	}
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprint(i)
			if i%2 == 0 {
				id = "block"
			}
			ctx := context.WithValue(context.Background(), requestKey{}, id)
			block, err := c.Moderate(ctx, id, "user")
			if err != nil || (block != "") != (id == "block") {
				t.Errorf("request %d mixed: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if reads.Load() != 32 {
		t.Fatalf("credential reads = %d", reads.Load())
	}
}

func TestLLMShieldClosesBoundedBodies(t *testing.T) {
	for _, signed := range []bool{false, true} {
		for _, status := range []int{200, 401, 500} {
			body := &shieldTrackedBody{Reader: bytes.NewReader(bytes.Repeat([]byte("x"), maxShieldResponseBytes+100))}
			client := &http.Client{Transport: shieldRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: body}, nil
			})}
			cfg := LLMShieldConfig{AppID: "test", APIKey: "key", HTTPClient: client, CredentialSource: veauth.NewRoleCredentialSource(veauth.RoleCredentialSourceConfig{AccessKeyID: "test-ak", SecretAccessKey: "test-sk"})}
			if signed {
				cfg.APIKey = ""
			}
			c, err := NewLLMShieldClientWithConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Moderate(context.Background(), "message", "user"); err == nil {
				t.Fatal("oversized response accepted")
			}
			if !body.closed || body.read > maxShieldResponseBytes+1 {
				t.Fatalf("body lifecycle: closed=%v read=%d", body.closed, body.read)
			}
		}
	}
}

type shieldTrackedBody struct {
	*bytes.Reader
	closed bool
	read   int
}

func (b *shieldTrackedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}
func (b *shieldTrackedBody) Close() error { b.closed = true; return nil }

func TestLLMShieldCredentialErrorsAndCanceledContext(t *testing.T) {
	var calls atomic.Int32
	source := veauth.CredentialSourceFunc(func(ctx context.Context) (veauth.VeIAMCredential, error) {
		calls.Add(1)
		return veauth.VeIAMCredential{}, errors.New("secret-credential-marker")
	})
	c, err := NewLLMShieldClientWithConfig(LLMShieldConfig{AppID: "test-app", CredentialSource: source})
	if err != nil || calls.Load() != 0 {
		t.Fatal("eager credential lookup")
	}
	if _, err := c.Moderate(context.Background(), "input", "user"); !errors.Is(err, ErrInvalidApiKey) || strings.Contains(err.Error(), "secret-credential-marker") {
		t.Fatalf("credential error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.check(ctx, "input", "user"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("canceled request performed credential lookup")
	}
}
