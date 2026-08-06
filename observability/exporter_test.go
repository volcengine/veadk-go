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

package observability

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
)

func TestCreateLogClient(t *testing.T) {
	ctx := context.Background()
	url := "http://localhost:4317"
	headers := map[string]string{"test": "header"}

	t.Run("default protocol", func(t *testing.T) {
		exporter, err := createLogClient(ctx, url, "", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})

	t.Run("http protocol", func(t *testing.T) {
		exporter, err := createLogClient(ctx, url, "http/protobuf", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})

	t.Run("env protocol", func(t *testing.T) {
		os.Setenv(OTELExporterOTLPProtocolEnvKey, "http/json")
		defer os.Unsetenv(OTELExporterOTLPProtocolEnvKey)

		exporter, err := createLogClient(ctx, url, "", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})

	t.Run("missing url", func(t *testing.T) {
		exporter, err := createLogClient(ctx, "", "", headers)
		assert.Error(t, err)
		assert.Nil(t, exporter)
		assert.Equal(t, "OTEL_EXPORTER_OTLP_ENDPOINT is not set", err.Error())
	})
}

func TestCreateTraceClient(t *testing.T) {
	ctx := context.Background()
	url := "http://localhost:4317"
	headers := map[string]string{"test": "header"}

	t.Run("http protocol", func(t *testing.T) {
		exporter, err := createTraceClient(ctx, url, "http/protobuf", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})

	t.Run("grpc protocol", func(t *testing.T) {
		exporter, err := createTraceClient(ctx, url, "grpc", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})
}

func TestCreateMetricClient(t *testing.T) {
	ctx := context.Background()
	url := "http://localhost:4317"
	headers := map[string]string{"test": "header"}

	t.Run("http protocol", func(t *testing.T) {
		exporter, err := createMetricClient(ctx, url, "http/protobuf", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})

	t.Run("grpc protocol", func(t *testing.T) {
		exporter, err := createMetricClient(ctx, url, "grpc", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})
}

func TestPrepareTLSExporterConfigAutoCreate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/DescribeProjects":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Projects": []any{},
				"Total":    0,
			})
		case "/CreateProject":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ProjectId": "project-1",
			})
		case "/DescribeTraceInstances":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"TraceInstances": []any{},
				"Total":          0,
			})
		case "/CreateTraceInstance":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"TraceInstanceId": "trace-1",
			})
		case "/DescribeTraceInstance":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"TraceInstanceId":   "trace-1",
				"TraceInstanceName": "tls-copilot",
				"TraceTopicId":      "topic-1",
				"ProjectId":         "project-1",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prepared, err := PrepareTLSExporterConfig(context.Background(), &configs.TLSExporterConfig{
		Endpoint:    "https://tls-cn-beijing.volces.com:4318/v1/traces",
		APIEndpoint: server.URL,
		AccessKey:   "ak",
		SecretKey:   "sk",
		Region:      "cn-beijing",
		ServiceName: "TLS Copilot",
	})
	require.NoError(t, err)

	assert.Equal(t, "topic-1", prepared.TopicID)
	assert.Equal(t, "project-1", prepared.ProjectID)
	assert.Equal(t, "tls-copilot", prepared.TraceInstanceName)
	assert.Equal(t, server.URL, prepared.APIEndpoint)
}

func TestPrepareTLSExporterConfigRequiresTopicWhenAutoCreateDisabled(t *testing.T) {
	autoCreate := false
	prepared, err := PrepareTLSExporterConfig(context.Background(), &configs.TLSExporterConfig{
		AutoCreate: &autoCreate,
	})

	assert.Nil(t, prepared)
	assert.ErrorContains(t, err, "TLS trace topic is required")
}

func TestPrepareTLSExporterConfigRejectsPartialTLSCredentials(t *testing.T) {
	prepared, err := PrepareTLSExporterConfig(context.Background(), &configs.TLSExporterConfig{
		TopicID:   "topic-1",
		AccessKey: " ",
		SecretKey: " ==",
	})

	assert.Nil(t, prepared)
	assert.ErrorContains(t, err, "must be configured together")
}

func TestNewMultiExporterAllowsTLSDynamicAuth(t *testing.T) {
	exp, err := NewMultiExporter(context.Background(), &configs.OpenTelemetryConfig{
		TLS: &configs.TLSExporterConfig{
			Endpoint: "http://localhost:4318/v1/traces",
			Region:   "cn-beijing",
			TopicID:  "topic-1",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, exp)
}

func TestTLSOTLPAuthRoundTripperUsesAuthInfo(t *testing.T) {
	t.Setenv(common.VOLCENGINE_ACCESS_KEY, "env-ak")
	t.Setenv(common.VOLCENGINE_SECRET_KEY, "env-sk")

	rt := &tlsOTLPAuthRoundTripper{base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "env-ak", req.Header.Get("x-tls-otel-ak"))
		assert.Equal(t, "env-sk", req.Header.Get("x-tls-otel-sk"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
	req, err := http.NewRequest(http.MethodPost, "http://localhost/v1/traces", nil)
	require.NoError(t, err)

	resp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
