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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/integrations/ve_sign"
)

const (
	path                   = "/v2/moderate"
	service                = "llmshield"
	action                 = "Moderate"
	version                = "2025-08-31"
	defaultTimeout         = 50
	maxShieldResponseBytes = 4 << 20
)

var (
	ErrInvalidAppID      = errors.New("LLM Shield requires TOOL_LLM_SHIELD_APP_ID")
	ErrInvalidApiKey     = errors.New("LLM Shield credentials unavailable")
	ErrShieldUnavailable = errors.New("LLM Shield unavailable")
	ErrShieldResponse    = errors.New("LLM Shield invalid or oversized response")
	ErrShieldDegraded    = errors.New("LLM Shield service degraded")
)

// CategoryMap is retained for compatibility. Configure it only before serving requests.
var CategoryMap = map[string]string{
	"101": "Model Misuse", "103": "Sensitive Information", "104": "Prompt Injection",
	"106": "General Topic Control", "107": "Computational Resource Consumption",
}

// LLMShieldClient is safe for concurrent requests when its configuration and
// injected dependencies are not mutated. Timeout is expressed in seconds for
// compatibility; new callers should use LLMShieldConfig.Timeout.
type LLMShieldClient struct {
	URL              string
	Region           string
	AppID            string
	APIKey           string
	Timeout          int
	HTTPClient       *http.Client
	CredentialSource veauth.CredentialSource
	timeout          time.Duration
	failurePolicy    LLMShieldFailurePolicy
}

type LLMShieldResult struct {
	ResponseMetadata *ResponseMetadata `json:"ResponseMetadata"`
	Result           *LLMShieldData    `json:"Result"`
}
type ResponseMetadata struct {
	RequestID string `json:"RequestId"`
	Service   string `json:"Service"`
	Region    string `json:"Region"`
	Action    string `json:"Action"`
	Version   string `json:"Version"`
}
type Matches struct {
	Word   string `json:"Word"`
	Source int    `json:"Source"`
}
type Risks struct {
	Category string     `json:"Category"`
	Label    string     `json:"Label"`
	Prob     float64    `json:"Prob,omitempty"`
	Matches  []*Matches `json:"Matches,omitempty"`
}
type RiskInfo struct {
	Risks []*Risks `json:"Risks"`
}

type ReplaceDetail struct {
	Replacement interface{} `json:"Replacement"`
}
type DecisionDetail struct {
	BlockDetail   map[string]interface{} `json:"BlockDetail"`
	ReplaceDetail *ReplaceDetail         `json:"ReplaceDetail"`
}
type Decision struct {
	DecisionType   int             `json:"DecisionType"`
	DecisionDetail *DecisionDetail `json:"DecisionDetail"`
	HitStrategyIDs []string        `json:"HitStrategyIDs"`
}
type PermitInfo struct {
	Permits interface{} `json:"Permits"`
}
type LLMShieldData struct {
	MsgID         string      `json:"MsgID"`
	RiskInfo      *RiskInfo   `json:"RiskInfo"`
	Decision      *Decision   `json:"Decision"`
	PermitInfo    *PermitInfo `json:"PermitInfo"`
	ContentInfo   string      `json:"ContentInfo"`
	Degraded      bool        `json:"Degraded"`
	DegradeReason string      `json:"DegradeReason"`
}

// NewLLMShieldClient retains environment/config.yaml configuration. Credentials
// are resolved on each request, never at construction.
func NewLLMShieldClient(timeout int) (*LLMShieldClient, error) {
	cfg := shieldEnvironmentConfig()
	if timeout > 0 {
		cfg.Timeout = time.Duration(timeout) * time.Second
	}
	return NewLLMShieldClientWithConfig(cfg)
}

// NewLLMShieldClientWithConfig creates a client without network or credential IO.
// Enabled and Callbacks only apply to plugin construction.
func NewLLMShieldClientWithConfig(cfg LLMShieldConfig) (*LLMShieldClient, error) {
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, ErrInvalidAppID
	}
	if cfg.Timeout < 0 {
		return nil, errors.New("LLM Shield timeout must not be negative")
	}
	if cfg.FailurePolicy != LLMShieldFailOpen && cfg.FailurePolicy != LLMShieldFailClose {
		return nil, errors.New("LLM Shield invalid failure policy")
	}
	if cfg.Region == "" {
		cfg.Region = "cn-beijing"
	}
	if cfg.URL == "" {
		cfg.URL = fmt.Sprintf("https://%s.sdk.access.llm-shield.omini-shield.com", cfg.Region)
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, errors.New("LLM Shield URL must be an HTTP(S) origin")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout * time.Second
	}
	if cfg.CredentialSource == nil {
		cfg.CredentialSource = veauth.NewRoleCredentialSource(veauth.RoleCredentialSourceConfig{})
	}
	// Copy the client so redirect protection does not mutate caller-owned state.
	client := http.Client{}
	if cfg.HTTPClient != nil {
		client = *cfg.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &LLMShieldClient{URL: cfg.URL, Region: cfg.Region, AppID: cfg.AppID,
		APIKey: cfg.APIKey, Timeout: int(cfg.Timeout / time.Second), timeout: cfg.Timeout,
		HTTPClient: &client, CredentialSource: cfg.CredentialSource, failurePolicy: cfg.FailurePolicy}, nil
}

// Moderate returns a blocking message or an empty string for a permitted input.
// Service failures are returned as redacted errors; failure policy is applied by
// the callbacks. Cancellation always propagates, including in fail-open mode.
func (p *LLMShieldClient) Moderate(ctx context.Context, message, role string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if p == nil || strings.TrimSpace(p.AppID) == "" {
		return "", ErrInvalidAppID
	}
	timeout := p.timeout
	if timeout == 0 {
		timeout = time.Duration(p.Timeout) * time.Second
	}
	if timeout <= 0 {
		timeout = defaultTimeout * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body := map[string]any{"Message": map[string]any{"Role": role, "Content": message, "ContentType": 1}, "Scene": p.AppID}
	client := http.Client{}
	if p.HTTPClient != nil {
		client = *p.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var data []byte
	var err error
	if p.APIKey != "" {
		bodyBytes, _ := json.Marshal(body)
		var req *http.Request
		req, err = http.NewRequestWithContext(requestCtx, http.MethodPost, p.URL+path, bytes.NewReader(bodyBytes))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-api-key", p.APIKey)
			q := req.URL.Query()
			q.Set("Action", action)
			q.Set("Version", version)
			req.URL.RawQuery = q.Encode()
			data, err = shieldDoRequest(&client, req)
		}
	} else {
		source := p.CredentialSource
		if source == nil {
			source = veauth.NewRoleCredentialSource(veauth.RoleCredentialSourceConfig{})
		}
		credential, credErr := source.Credential(requestCtx)
		if credErr != nil || credential.AccessKeyID == "" || credential.SecretAccessKey == "" {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", ErrInvalidApiKey
		}
		u, parseErr := url.Parse(p.URL)
		if parseErr != nil {
			return "", ErrShieldUnavailable
		}
		request := ve_sign.VeRequest{AK: credential.AccessKeyID, SK: credential.SecretAccessKey,
			Method: http.MethodPost, Scheme: u.Scheme, Host: u.Host, Path: path, Service: service,
			Region: p.Region, Action: action, Version: version, Body: body,
			Header: map[string]string{"X-Top-Service": service, "X-Top-Region": p.Region, "X-Security-Token": credential.SessionToken},
		}
		data, err = request.DoRequestWithContext(requestCtx, &client)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", ErrShieldUnavailable
	}
	return shieldDecision(data)
}

func shieldDoRequest(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, ErrShieldUnavailable
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxShieldResponseBytes+1))
	if err != nil || len(data) > maxShieldResponseBytes {
		return nil, ErrShieldResponse
	}
	if resp.StatusCode != http.StatusOK {
		return nil, ErrShieldUnavailable
	}
	return data, nil
}

func shieldDecision(data []byte) (string, error) {
	var response LLMShieldResult
	if err := json.Unmarshal(data, &response); err != nil {
		return "", ErrShieldResponse
	}
	if response.Result == nil {
		return "", ErrShieldResponse
	}
	result := response.Result
	// Python blocks only DecisionType=2 with nonempty Risks. Retain that rule,
	// including when a degraded response still contains an actionable block.
	if result.Decision != nil && result.Decision.DecisionType == 2 && result.RiskInfo != nil && len(result.RiskInfo.Risks) > 0 {
		reasons := map[string]bool{}
		for _, risk := range result.RiskInfo.Risks {
			if risk == nil || risk.Category == "" {
				continue
			}
			category, err := strconv.Atoi(risk.Category)
			if err != nil {
				continue
			} // Never echo arbitrary server text.
			key := strconv.Itoa(category)
			label, ok := CategoryMap[key]
			if !ok {
				label = "Category " + key
			}
			reasons[label] = true
		}
		labels := make([]string, 0, len(reasons))
		for label := range reasons {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		reason := "security policy violation"
		if len(labels) > 0 {
			reason = strings.Join(labels, ", ")
		}
		return fmt.Sprintf("Your request has been blocked due to: %s. Please modify your input and try again.", reason), nil
	}
	if result.Degraded {
		return "", ErrShieldDegraded
	}
	if result.Decision == nil {
		return "", ErrShieldResponse
	}
	return "", nil
}

// Accept the numeric and string category/decision values handled by Python's int().
func (r *Risks) UnmarshalJSON(data []byte) error {
	type wire Risks
	var value struct {
		*wire
		Category json.RawMessage `json:"Category"`
	}
	value.wire = (*wire)(r)
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if len(value.Category) == 0 || string(value.Category) == "null" {
		return nil
	}
	if value.Category[0] == '"' {
		return json.Unmarshal(value.Category, &r.Category)
	}
	var number json.Number
	if err := json.Unmarshal(value.Category, &number); err != nil {
		return err
	}
	r.Category = number.String()
	return nil
}

func (d *Decision) UnmarshalJSON(data []byte) error {
	type wire Decision
	var value struct {
		*wire
		DecisionType json.RawMessage `json:"DecisionType"`
	}
	value.wire = (*wire)(d)
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	raw := value.DecisionType
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var text string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return err
		}
	} else {
		text = string(raw)
	}
	number, err := strconv.Atoi(text)
	d.DecisionType = number
	return err
}
