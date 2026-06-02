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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureLogProjectAndTracingInstance(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var signedRequests int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		if strings.TrimSpace(r.Header.Get("Authorization")) != "" && r.Header.Get("X-Security-Token") == "token" {
			signedRequests++
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/DescribeProjects":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Projects": []any{},
				"Total":    0,
			})
		case "/CreateProject":
			var body struct {
				ProjectName string `json:"ProjectName"`
				Tags        []struct {
					Key   string `json:"Key"`
					Value string `json:"Value"`
				} `json:"Tags"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "project-name", body.ProjectName)
			require.Len(t, body.Tags, 1)
			assert.Equal(t, DefaultProviderTagKey, body.Tags[0].Key)
			assert.Equal(t, DefaultProviderTagValue, body.Tags[0].Value)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ProjectId": "project-1",
			})
		case "/DescribeTraceInstances":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"TraceInstances": []any{},
				"Total":          0,
			})
		case "/CreateTraceInstance":
			assert.Equal(t, DefaultTraceTag, r.Header.Get("TraceTag"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"TraceInstanceId": "trace-1",
			})
		case "/DescribeTraceInstance":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"TraceInstanceId":   "trace-1",
				"TraceInstanceName": "trace-name",
				"TraceTopicId":      "topic-1",
				"ProjectId":         "project-1",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(&Config{
		AK:           "ak",
		SK:           "sk",
		SessionToken: "token",
		Region:       "cn-beijing",
		Endpoint:     server.URL,
	})
	require.NoError(t, err)

	projectID, err := client.EnsureLogProjectContext(context.Background(), "project-name")
	require.NoError(t, err)
	assert.Equal(t, "project-1", projectID)

	instance, err := client.EnsureTracingInstanceContext(context.Background(), projectID, "trace-name")
	require.NoError(t, err)
	assert.Equal(t, "topic-1", instance.TraceTopicID)

	assert.Equal(t, []string{
		"/DescribeProjects",
		"/CreateProject",
		"/DescribeTraceInstances",
		"/CreateTraceInstance",
		"/DescribeTraceInstance",
	}, paths)
	assert.Equal(t, len(paths), signedRequests)
}

func TestNewRejectsPartialCredentials(t *testing.T) {
	client, err := New(&Config{
		SK:       "sk",
		Region:   "cn-beijing",
		Endpoint: "http://127.0.0.1:12345",
	})
	assert.Nil(t, client)
	assert.ErrorContains(t, err, "must be configured together")
}

func TestEndpointNormalization(t *testing.T) {
	assert.Equal(t, "https://tls-cn-beijing.volces.com", NormalizeAPIEndpoint("tls-cn-beijing.volces.com", "cn-beijing"))
	assert.Equal(t, "http://127.0.0.1:12345", NormalizeAPIEndpoint("http://127.0.0.1:12345/openapi", "cn-beijing"))
	assert.Equal(t, "https://tls-cn-beijing.volces.com", APIEndpointFromOTLPEndpoint("https://tls-cn-beijing.volces.com:4318/v1/traces", "cn-beijing"))
	assert.Equal(t, "https://tls-cn-beijing.volces.com:8443", APIEndpointFromOTLPEndpoint("https://tls-cn-beijing.volces.com:8443/v1/traces", "cn-beijing"))
}

func TestDefaultNamesForService(t *testing.T) {
	assert.Equal(t, "tls-copilot-traces", DefaultProjectNameForService("TLS Copilot"))
	assert.Equal(t, "tls-copilot", DefaultTraceInstanceNameForService("TLS Copilot"))
	assert.Equal(t, DefaultProjectName, DefaultProjectNameForService("!!!"))
	assert.Equal(t, DefaultTraceInstanceName, DefaultTraceInstanceNameForService("!!!"))
}

func TestIsNotFound(t *testing.T) {
	assert.True(t, IsNotFound(&APIError{HTTPCode: http.StatusNotFound, Code: "ProjectNotExists", Message: "Project does not exist."}))
	assert.True(t, IsNotFound(&APIError{HTTPCode: http.StatusNotFound, Code: "TraceInstanceNotFound"}))
	assert.False(t, IsNotFound(&APIError{HTTPCode: http.StatusBadRequest, Code: "InvalidArgument"}))
}
