package ve_mem0

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMem0Client_Add(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/memories", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "test-api-key", r.Header.Get("Authorization"))

		var req AddMemoriesRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		assert.NoError(t, err)
		assert.Equal(t, "user123", *req.UserId)
		assert.Len(t, req.Messages, 1)
		assert.Equal(t, "user", req.Messages[0].Role)
		assert.Equal(t, "hello", req.Messages[0].Content)

		// Response
		resp := map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"event_id": "evt1",
					"status":   "PENDING",
					"message":  "queued",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewMem0Client(server.URL, "test-api-key")

	userID := "user123"
	async := false
	req := AddMemoriesRequest{
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		UserId:    &userID,
		AsyncMode: &async,
	}

	resp, err := client.Add(context.Background(), req)
	assert.NoError(t, err)
	assert.Len(t, resp.Results, 1)
	assert.Equal(t, "evt1", resp.Results[0].EventId)
	assert.Equal(t, "PENDING", resp.Results[0].Status)
}

func TestMem0Client_Search(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/search", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "test-api-key", r.Header.Get("Authorization"))

		var req SearchMemoriesRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		assert.NoError(t, err)
		assert.Equal(t, "find something", req.Query)
		assert.Equal(t, "user123", *req.UserId)

		// Response
		resp := map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"id":         "mem2",
					"memory":     "found something",
					"created_at": time.Now().Format(time.RFC3339),
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewMem0Client(server.URL, "test-api-key")

	userID := "user123"
	topK := 3
	req := SearchMemoriesRequest{
		Query:  "find something",
		UserId: &userID,
		TopK:   &topK,
	}

	resp, err := client.Search(context.Background(), req)
	assert.NoError(t, err)
	assert.Len(t, resp.Results, 1)
	assert.Equal(t, "mem2", resp.Results[0].Id)
	assert.Equal(t, "found something", resp.Results[0].Memory)
}
