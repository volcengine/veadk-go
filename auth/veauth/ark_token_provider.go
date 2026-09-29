// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package veauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/volcengine/veadk-go/v2/common"
	"github.com/volcengine/veadk-go/v2/integrations/ve_sign"
	"golang.org/x/sync/singleflight"
)

const (
	defaultArkControlPlaneEndpoint = "https://open.volcengineapi.com"
	defaultArkProjectName          = "default"
	defaultArkTokenTimeout         = 10 * time.Second
	defaultArkTokenMaxAttempts     = 3
)

var errArkTokenInvalidated = errors.New("ARK token invalidated during refresh")

// ArkTokenProviderConfig configures a lazy, process-local ARK API key provider.
type ArkTokenProviderConfig struct {
	Region           string
	CredentialSource CredentialSource
	CredentialPath   string
	Endpoint         string
	ProjectName      string
	HTTPClient       *http.Client
	RequestTimeout   time.Duration
	MaxAttempts      int
	InitialBackoff   time.Duration
}

// ArkTokenProvider exchanges a Role credential for an ARK API key on the first
// model request. The key is cached and concurrent callers share one exchange.
type ArkTokenProvider struct {
	config ArkTokenProviderConfig

	mu         sync.RWMutex
	apiKey     string
	generation uint64
	group      singleflight.Group
}

// NewArkTokenProvider constructs a provider without reading credentials or
// making network requests.
func NewArkTokenProvider(config ArkTokenProviderConfig) *ArkTokenProvider {
	if strings.TrimSpace(config.Region) == "" {
		config.Region = common.DEFAULT_MODEL_REGION
	}
	if config.CredentialSource == nil {
		config.CredentialSource = NewRoleCredentialSource(RoleCredentialSourceConfig{
			CredentialPath: config.CredentialPath,
		})
	}
	if strings.TrimSpace(config.Endpoint) == "" {
		config.Endpoint = defaultArkControlPlaneEndpoint
	}
	if strings.TrimSpace(config.ProjectName) == "" {
		config.ProjectName = defaultArkProjectName
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = defaultArkTokenTimeout
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = defaultArkTokenMaxAttempts
	}
	if config.InitialBackoff <= 0 {
		config.InitialBackoff = 100 * time.Millisecond
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: config.RequestTimeout}
	}
	return &ArkTokenProvider{config: config}
}

// APIKey returns a cached key or lazily exchanges the current Role credential.
// Each waiter may cancel independently; the shared exchange remains bounded by
// per-request timeouts and the configured retry count.
func (p *ArkTokenProvider) APIKey(ctx context.Context) (string, error) {
	for {
		key, generation := p.cachedAPIKey()
		if key != "" {
			return key, nil
		}

		groupKey := fmt.Sprintf("ark-api-key-%d", generation)
		result := p.group.DoChan(groupKey, func() (any, error) {
			if cached, _ := p.cachedAPIKey(); cached != "" {
				return cached, nil
			}
			key, err := p.fetchWithRetry(context.Background())
			if err != nil {
				return "", err
			}
			p.mu.Lock()
			if p.generation != generation {
				p.mu.Unlock()
				return "", errArkTokenInvalidated
			}
			p.apiKey = key
			p.mu.Unlock()
			return key, nil
		})

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case resolved := <-result:
			if errors.Is(resolved.Err, errArkTokenInvalidated) {
				continue
			}
			if resolved.Err != nil {
				return "", resolved.Err
			}
			key, ok := resolved.Val.(string)
			if !ok || strings.TrimSpace(key) == "" {
				return "", errors.New("ARK token provider returned an empty API key")
			}
			return key, nil
		}
	}
}

// Invalidate clears the cached API key. The next call re-reads the Role
// credential, which supports credential and API key rotation.
func (p *ArkTokenProvider) Invalidate() {
	p.mu.Lock()
	p.apiKey = ""
	p.generation++
	p.mu.Unlock()
}

func (p *ArkTokenProvider) cachedAPIKey() (string, uint64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.apiKey, p.generation
}

func (p *ArkTokenProvider) fetchWithRetry(ctx context.Context) (string, error) {
	var lastErr error
	attempts := 0
	for attempt := 1; attempt <= p.config.MaxAttempts; attempt++ {
		attempts = attempt
		requestCtx, cancel := context.WithTimeout(ctx, p.config.RequestTimeout)
		key, err := p.fetch(requestCtx)
		cancel()
		if err == nil {
			return key, nil
		}
		lastErr = err
		if attempt == p.config.MaxAttempts || !isRetryableArkTokenError(err) || ctx.Err() != nil {
			break
		}

		delay := p.config.InitialBackoff * time.Duration(1<<(attempt-1))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", fmt.Errorf("failed to fetch ARK API key after %d attempt(s): %w", attempts, lastErr)
}

func isRetryableArkTokenError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	var responseErr *ve_sign.HTTPError
	if errors.As(err, &responseErr) {
		return responseErr.StatusCode == http.StatusTooManyRequests || responseErr.StatusCode >= http.StatusInternalServerError
	}
	var networkErr net.Error
	return errors.As(err, &networkErr) && networkErr.Timeout()
}

func (p *ArkTokenProvider) fetch(ctx context.Context) (string, error) {
	endpoint, err := url.Parse(p.config.Endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid ARK control-plane endpoint: %w", err)
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return "", fmt.Errorf("invalid ARK control-plane endpoint scheme %q", endpoint.Scheme)
	}
	if endpoint.Host == "" || endpoint.User != nil || (endpoint.Path != "" && endpoint.Path != "/") || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return "", fmt.Errorf("invalid ARK control-plane endpoint %q", p.config.Endpoint)
	}

	credential, err := p.config.CredentialSource.Credential(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve Role credential: %w", err)
	}

	header := map[string]string{}
	if credential.SessionToken != "" {
		header["X-Security-Token"] = credential.SessionToken
	}

	listRequest := ve_sign.VeRequest{
		AK: credential.AccessKeyID, SK: credential.SecretAccessKey, Method: http.MethodPost,
		Scheme: endpoint.Scheme, Host: endpoint.Host, Path: "/",
		Service: "ark", Region: p.config.Region,
		Action: "ListApiKeys", Version: "2024-01-01", Header: header,
		Body: map[string]any{
			"ProjectName": p.config.ProjectName,
			"Filter":      map[string]any{"AllowAll": true},
			"PageNumber":  1,
			"PageSize":    100,
		},
	}
	listBody, err := listRequest.DoRequestWithContext(ctx, p.config.HTTPClient)
	if err != nil {
		return "", fmt.Errorf("list ARK API keys: %w", err)
	}
	var listResponse listApiKeysResponse
	if err := json.Unmarshal(listBody, &listResponse); err != nil {
		return "", fmt.Errorf("decode ARK API key list: %w", err)
	}
	if len(listResponse.Result.Items) == 0 {
		return "", errors.New("ARK API key list is empty")
	}

	getRequest := ve_sign.VeRequest{
		AK: credential.AccessKeyID, SK: credential.SecretAccessKey, Method: http.MethodPost,
		Scheme: endpoint.Scheme, Host: endpoint.Host, Path: "/",
		Service: "ark", Region: p.config.Region,
		Action: "GetRawApiKey", Version: "2024-01-01", Header: header,
		Body: map[string]any{
			"Id":          listResponse.Result.Items[0].ID,
			"ProjectName": p.config.ProjectName,
		},
	}
	getBody, err := getRequest.DoRequestWithContext(ctx, p.config.HTTPClient)
	if err != nil {
		return "", fmt.Errorf("get raw ARK API key: %w", err)
	}
	var getResponse getRawApiKeyResponse
	if err := json.Unmarshal(getBody, &getResponse); err != nil {
		return "", fmt.Errorf("decode raw ARK API key response: %w", err)
	}
	if strings.TrimSpace(getResponse.Result.ApiKey) == "" {
		return "", errors.New("ARK API key is empty")
	}
	return getResponse.Result.ApiKey, nil
}
