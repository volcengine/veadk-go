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

package ve_tls

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/utils"
	"github.com/volcengine/volc-sdk-golang/base"
)

const (
	DefaultRegion            = "cn-beijing"
	DefaultProjectName       = "veadk-traces"
	DefaultTraceInstanceName = "veadk"
	DefaultTraceDescription  = "Created by Volcengine Agent Development Kit (VeADK)"
	DefaultTraceTag          = "veadk"
	DefaultProviderTagKey    = "provider"
	DefaultProviderTagValue  = "VeADK"

	ServiceName = "TLS"

	EnvTLSProjectID         = "OBSERVABILITY_OPENTELEMETRY_TLS_PROJECT_ID"
	EnvTLSProjectName       = "OBSERVABILITY_OPENTELEMETRY_TLS_PROJECT_NAME"
	EnvTLSTraceInstanceID   = "OBSERVABILITY_OPENTELEMETRY_TLS_TRACE_INSTANCE_ID"
	EnvTLSTraceInstanceName = "OBSERVABILITY_OPENTELEMETRY_TLS_TRACE_INSTANCE_NAME"
	EnvTLSAPIEndpoint       = "OBSERVABILITY_OPENTELEMETRY_TLS_API_ENDPOINT"
	EnvTLSAutoCreate        = "OBSERVABILITY_OPENTELEMETRY_TLS_AUTO_CREATE"
	EnvTLSSessionToken      = "OBSERVABILITY_OPENTELEMETRY_TLS_SESSION_TOKEN"
)

type Config struct {
	AK           string
	SK           string
	SessionToken string
	Region       string
	Endpoint     string
	HTTPClient   *http.Client
}

type Client struct {
	config     Config
	httpClient *http.Client
}

type TraceInstance struct {
	TraceInstanceID     string `json:"TraceInstanceId"`
	TraceInstanceName   string `json:"TraceInstanceName"`
	TraceInstanceStatus string `json:"TraceInstanceStatus"`
	TraceTopicID        string `json:"TraceTopicId"`
	TraceTopicName      string `json:"TraceTopicName"`
	ProjectID           string `json:"ProjectId"`
	ProjectName         string `json:"ProjectName"`
}

type Project struct {
	ProjectID   string `json:"ProjectId"`
	ProjectName string `json:"ProjectName"`
	Description string `json:"Description"`
}

type APIError struct {
	HTTPCode int
	Code     string
	Message  string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	parts := []string{"TLS OpenAPI request failed"}
	if e.HTTPCode > 0 {
		parts = append(parts, "http="+strconv.Itoa(e.HTTPCode))
	}
	if e.Code != "" {
		parts = append(parts, "code="+e.Code)
	}
	if e.Message != "" {
		parts = append(parts, "message="+e.Message)
	}
	return strings.Join(parts, " ")
}

func New(cfg *Config) (*Client, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	out := *cfg
	out.AK = strings.TrimSpace(out.AK)
	out.SK = strings.TrimSpace(out.SK)
	if out.AK != "" || out.SK != "" {
		if out.AK == "" || out.SK == "" {
			return nil, fmt.Errorf("TLS access key and secret key must be configured together")
		}
	}
	if out.AK == "" {
		out.AK = utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	}
	if out.SK == "" {
		out.SK = utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	}
	if out.AK == "" || out.SK == "" {
		iam, err := veauth.GetCredentialFromVeFaaSIAM()
		if err != nil {
			return nil, fmt.Errorf("load TLS credentials failed: %w", err)
		}
		out.AK = iam.AccessKeyID
		out.SK = iam.SecretAccessKey
		out.SessionToken = iam.SessionToken
	}
	if out.SessionToken == "" {
		out.SessionToken = strings.TrimSpace(os.Getenv(EnvTLSSessionToken))
	}
	if out.Region == "" {
		out.Region = firstNonEmpty(os.Getenv("REGION"), os.Getenv("AGENTKIT_TOOL_REGION"), DefaultRegion)
	}
	if out.Endpoint == "" {
		out.Endpoint = firstNonEmpty(os.Getenv(EnvTLSAPIEndpoint), fmt.Sprintf("https://tls-%s.volces.com", out.Region))
	}
	out.Endpoint = NormalizeAPIEndpoint(out.Endpoint, out.Region)
	if out.HTTPClient == nil {
		out.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if strings.TrimSpace(out.AK) == "" || strings.TrimSpace(out.SK) == "" {
		return nil, fmt.Errorf("TLS access key and secret key are required")
	}
	if strings.TrimSpace(out.Region) == "" {
		return nil, fmt.Errorf("TLS region is required")
	}
	if strings.TrimSpace(out.Endpoint) == "" {
		return nil, fmt.Errorf("TLS endpoint is required")
	}
	return &Client{config: out, httpClient: out.HTTPClient}, nil
}

func (c *Client) CreateLogProject(projectName string) (string, error) {
	return c.CreateLogProjectContext(context.Background(), projectName)
}

func (c *Client) CreateLogProjectContext(ctx context.Context, projectName string) (string, error) {
	var out struct {
		ProjectID string `json:"ProjectId"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/CreateProject", nil, map[string]any{
		"ProjectName": strings.TrimSpace(projectName),
		"Region":      c.config.Region,
		"Description": DefaultTraceDescription,
		"Tags": []map[string]string{
			{
				"Key":   DefaultProviderTagKey,
				"Value": DefaultProviderTagValue,
			},
		},
	}, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.ProjectID) == "" {
		return "", fmt.Errorf("CreateProject returned empty ProjectId")
	}
	return strings.TrimSpace(out.ProjectID), nil
}

func (c *Client) EnsureLogProject(projectName string) (string, error) {
	return c.EnsureLogProjectContext(context.Background(), projectName)
}

func (c *Client) EnsureLogProjectContext(ctx context.Context, projectName string) (string, error) {
	projectName = strings.TrimSpace(projectName)
	if projectName == "" {
		return "", fmt.Errorf("project name is required")
	}
	project, ok, err := c.FindLogProjectByNameContext(ctx, projectName)
	if err != nil {
		return "", err
	}
	if ok {
		return project.ProjectID, nil
	}
	projectID, err := c.CreateLogProjectContext(ctx, projectName)
	if err != nil {
		if IsAlreadyExists(err) {
			project, ok, findErr := c.FindLogProjectByNameContext(ctx, projectName)
			if findErr != nil {
				return "", findErr
			}
			if ok {
				return project.ProjectID, nil
			}
		}
		return "", err
	}
	return projectID, nil
}

func (c *Client) FindLogProjectByNameContext(ctx context.Context, projectName string) (Project, bool, error) {
	var out struct {
		Projects []Project `json:"Projects"`
		Total    int64     `json:"Total"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/DescribeProjects", map[string]string{
		"ProjectName": strings.TrimSpace(projectName),
		"IsFullName":  "true",
		"PageNumber":  "1",
		"PageSize":    "100",
	}, nil, &out); err != nil {
		if IsNotFound(err) {
			return Project{}, false, nil
		}
		return Project{}, false, err
	}
	for _, project := range out.Projects {
		if strings.TrimSpace(project.ProjectName) == projectName && strings.TrimSpace(project.ProjectID) != "" {
			return project, true, nil
		}
	}
	return Project{}, false, nil
}

func (c *Client) CreateTracingInstance(projectID, traceInstanceName string) (TraceInstance, error) {
	return c.CreateTracingInstanceContext(context.Background(), projectID, traceInstanceName)
}

func (c *Client) CreateTracingInstanceContext(ctx context.Context, projectID, traceInstanceName string) (TraceInstance, error) {
	var out struct {
		TraceInstanceID string `json:"TraceInstanceId"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/CreateTraceInstance", nil, map[string]any{
		"TraceInstanceName": strings.TrimSpace(traceInstanceName),
		"ProjectId":         strings.TrimSpace(projectID),
		"Description":       DefaultTraceDescription,
	}, &out, map[string]string{"TraceTag": DefaultTraceTag}); err != nil {
		return TraceInstance{}, err
	}
	if strings.TrimSpace(out.TraceInstanceID) == "" {
		return TraceInstance{}, fmt.Errorf("CreateTraceInstance returned empty TraceInstanceId")
	}
	return c.DescribeTracingInstanceContext(ctx, out.TraceInstanceID)
}

func (c *Client) EnsureTracingInstance(projectID, traceInstanceName string) (TraceInstance, error) {
	return c.EnsureTracingInstanceContext(context.Background(), projectID, traceInstanceName)
}

func (c *Client) EnsureTracingInstanceContext(ctx context.Context, projectID, traceInstanceName string) (TraceInstance, error) {
	projectID = strings.TrimSpace(projectID)
	traceInstanceName = strings.TrimSpace(traceInstanceName)
	if projectID == "" {
		return TraceInstance{}, fmt.Errorf("project id is required")
	}
	if traceInstanceName == "" {
		return TraceInstance{}, fmt.Errorf("trace instance name is required")
	}
	instance, ok, err := c.FindTracingInstanceByNameContext(ctx, projectID, traceInstanceName)
	if err != nil {
		return TraceInstance{}, err
	}
	if ok {
		return c.ensureTraceTopicID(ctx, instance)
	}
	instance, err = c.CreateTracingInstanceContext(ctx, projectID, traceInstanceName)
	if err != nil {
		if IsAlreadyExists(err) {
			instance, ok, findErr := c.FindTracingInstanceByNameContext(ctx, projectID, traceInstanceName)
			if findErr != nil {
				return TraceInstance{}, findErr
			}
			if ok {
				return c.ensureTraceTopicID(ctx, instance)
			}
		}
		return TraceInstance{}, err
	}
	return c.ensureTraceTopicID(ctx, instance)
}

func (c *Client) FindTracingInstanceByNameContext(ctx context.Context, projectID, traceInstanceName string) (TraceInstance, bool, error) {
	var out struct {
		TraceInstances []TraceInstance `json:"TraceInstances"`
		Total          int64           `json:"Total"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/DescribeTraceInstances", map[string]string{
		"ProjectId":         strings.TrimSpace(projectID),
		"TraceInstanceName": strings.TrimSpace(traceInstanceName),
		"PageNumber":        "1",
		"PageSize":          "100",
	}, nil, &out); err != nil {
		if IsNotFound(err) {
			return TraceInstance{}, false, nil
		}
		return TraceInstance{}, false, err
	}
	for _, instance := range out.TraceInstances {
		if strings.TrimSpace(instance.TraceInstanceName) == traceInstanceName && strings.TrimSpace(instance.TraceInstanceID) != "" {
			return instance, true, nil
		}
	}
	return TraceInstance{}, false, nil
}

func (c *Client) DescribeTracingInstance(traceInstanceID string) (TraceInstance, error) {
	return c.DescribeTracingInstanceContext(context.Background(), traceInstanceID)
}

func (c *Client) DescribeTracingInstanceContext(ctx context.Context, traceInstanceID string) (TraceInstance, error) {
	var out TraceInstance
	if err := c.doJSON(ctx, http.MethodGet, "/DescribeTraceInstance", map[string]string{
		"TraceInstanceId": strings.TrimSpace(traceInstanceID),
	}, nil, &out); err != nil {
		return TraceInstance{}, err
	}
	return out, nil
}

func (c *Client) ensureTraceTopicID(ctx context.Context, instance TraceInstance) (TraceInstance, error) {
	if strings.TrimSpace(instance.TraceTopicID) != "" {
		return instance, nil
	}
	if strings.TrimSpace(instance.TraceInstanceID) == "" {
		return instance, nil
	}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return TraceInstance{}, ctx.Err()
			case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
			}
		}
		described, err := c.DescribeTracingInstanceContext(ctx, instance.TraceInstanceID)
		if err != nil {
			return TraceInstance{}, err
		}
		if strings.TrimSpace(described.TraceTopicID) != "" {
			return described, nil
		}
	}
	return instance, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, params map[string]string, body any, out any, extraHeaders ...map[string]string) error {
	if c == nil {
		return fmt.Errorf("TLS client is nil")
	}
	u, err := url.Parse(c.config.Endpoint)
	if err != nil {
		return err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	query := u.Query()
	for key, value := range params {
		if strings.TrimSpace(value) != "" {
			query.Set(key, value)
		}
	}
	u.RawQuery = query.Encode()

	var payload []byte
	if body == nil {
		payload = []byte("{}")
	} else {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tls-Apiversion", "0.3.0")
	for _, headers := range extraHeaders {
		for key, value := range headers {
			if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
				req.Header.Set(key, value)
			}
		}
	}
	req = c.signRequest(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DecodeAPIError(resp.StatusCode, responseBody)
	}
	if out == nil || len(bytes.TrimSpace(responseBody)) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, out); err != nil {
		return fmt.Errorf("decode TLS OpenAPI response failed: %w", err)
	}
	return nil
}

func (c *Client) signRequest(req *http.Request) *http.Request {
	req = base.Credentials{
		AccessKeyID:     c.config.AK,
		SecretAccessKey: c.config.SK,
		SessionToken:    c.config.SessionToken,
		Service:         ServiceName,
		Region:          c.config.Region,
	}.Sign(req)
	return req
}

func DecodeAPIError(status int, body []byte) error {
	body = bytes.TrimSpace(body)
	apiErr := &APIError{HTTPCode: status}
	var raw map[string]any
	if len(body) > 0 && json.Unmarshal(body, &raw) == nil {
		apiErr.Code = stringFromAny(firstPresent(raw, "Code", "code", "errorCode", "ErrorCode"))
		apiErr.Message = stringFromAny(firstPresent(raw, "Message", "message", "errorMessage", "ErrorMessage"))
		if nested, ok := firstPresent(raw, "Error", "error").(map[string]any); ok {
			if apiErr.Code == "" {
				apiErr.Code = stringFromAny(firstPresent(nested, "Code", "code", "errorCode", "ErrorCode"))
			}
			if apiErr.Message == "" {
				apiErr.Message = stringFromAny(firstPresent(nested, "Message", "message", "errorMessage", "ErrorMessage"))
			}
		}
	}
	if apiErr.Code == "" && apiErr.Message == "" && len(body) > 0 {
		apiErr.Message = string(body)
	}
	return apiErr
}

func IsAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		code := strings.ToLower(apiErr.Code)
		message := strings.ToLower(apiErr.Message)
		return strings.Contains(code, "already") ||
			strings.Contains(code, "exist") ||
			strings.Contains(message, "already exist") ||
			strings.Contains(message, "already exists")
	}
	return false
}

func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		code := strings.ToLower(apiErr.Code)
		message := strings.ToLower(apiErr.Message)
		normalizedCode := strings.ReplaceAll(code, "_", "")
		normalizedCode = strings.ReplaceAll(normalizedCode, "-", "")
		return strings.Contains(normalizedCode, "notexist") ||
			strings.Contains(normalizedCode, "notfound") ||
			strings.Contains(message, "does not exist") ||
			strings.Contains(message, "not found")
	}
	return false
}

func NormalizeAPIEndpoint(endpoint, region string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return fmt.Sprintf("https://tls-%s.volces.com", firstNonEmpty(region, DefaultRegion))
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func APIEndpointFromOTLPEndpoint(endpoint, region string) string {
	endpoint = NormalizeOTLPEndpoint(endpoint, region)
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return NormalizeAPIEndpoint(endpoint, region)
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	u.Host = stripDefaultOTLPPort(u.Host)
	return NormalizeAPIEndpoint(u.String(), region)
}

func NormalizeOTLPEndpoint(endpoint, region string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return fmt.Sprintf("https://tls-%s.volces.com:4318/v1/traces", firstNonEmpty(region, DefaultRegion))
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	return endpoint
}

func DefaultProjectNameForService(serviceName string) string {
	name := normalizeResourceName(serviceName, 22)
	if name == "" {
		return DefaultProjectName
	}
	return normalizeResourceName(name+"-traces", 30)
}

func DefaultTraceInstanceNameForService(serviceName string) string {
	name := normalizeResourceName(serviceName, 30)
	if name == "" {
		return DefaultTraceInstanceName
	}
	return name
}

func normalizeResourceName(input string, maxLen int) string {
	input = strings.ToLower(strings.TrimSpace(input))
	var b strings.Builder
	lastHyphen := false
	for _, r := range input {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if maxLen > 0 && len(out) > maxLen {
		out = strings.Trim(out[:maxLen], "-")
	}
	for len(out) > 0 && len(out) < 3 {
		out += "-x"
		out = strings.Trim(out, "-")
	}
	return out
}

func stripDefaultOTLPPort(host string) string {
	if strings.TrimSpace(host) == "" {
		return host
	}
	if h, p, err := net.SplitHostPort(host); err == nil && (p == "4318" || p == "4317") {
		if strings.Contains(h, ":") {
			return "[" + h + "]"
		}
		return h
	}
	return host
}

func firstPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
