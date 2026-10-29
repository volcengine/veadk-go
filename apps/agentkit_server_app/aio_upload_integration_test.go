// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package agentkit_server_app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"google.golang.org/adk/v2/session"
)

// Explicitly opt in: creates one physical sandbox using the configured Tool,
// exercises the real Runtime HTTP route, and deletes the sandbox on completion.
func TestAIOLiveStreamingUpload(t *testing.T) {
	if os.Getenv("VEADK_TEST_AIO_UPLOAD") != "1" {
		t.Skip("set VEADK_TEST_AIO_UPLOAD=1 to use an authorized Tool")
	}
	if file := os.Getenv("VEADK_TEST_AIO_ENV_FILE"); file != "" {
		values, err := godotenv.Read(file)
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range values {
			t.Setenv(key, value)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	store := session.InMemoryService()
	manager, err := agentkit.NewAIOLifecycle(agentkit.AIOLifecycleConfig{SessionService: store})
	if err != nil {
		t.Fatal(err)
	}
	id := "upload-validation-" + uuid.NewString()
	s, err := manager.Create(ctx, "test", "user", id, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 2*time.Minute)
		defer done()
		if err := manager.Delete(cleanup, "test", "user", id); err != nil {
			t.Errorf("live sandbox cleanup: %v", err)
		} else {
			t.Log("live sandbox deletion confirmed")
		}
	})
	api := httptest.NewServer(sessionRouter(t, store, WithAIOLifecycle(manager)))
	defer api.Close()
	upload := func(name string, size int64) (int, map[string]any) {
		resp, raw := postUploadPath(t, api, sessionBase+"/"+id+"/files", func(m *multipart.Writer) {
			p, _ := m.CreateFormFile("file", "original.bin")
			_, _ = io.CopyN(p, zeroReader{}, size)
			_ = m.WriteField("name", name)
		})
		var v map[string]any
		if resp.StatusCode == 201 {
			if err := json.Unmarshal(raw, &v); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode, v
	}
	const fileSize int64 = 64 << 20
	code, result := upload("live-flow.bin", fileSize)
	if code != 201 || result["size_bytes"] != float64(fileSize) {
		t.Fatalf("live upload = %d %+v", code, result)
	}
	client, err := agentkit.NewClient(nil)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := agentkit.ResolveAIOEndpointFor(ctx, client, s.State(), s.AppName(), s.UserID(), s.ID())
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal("invalid sandbox endpoint")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/file/download"
	q := u.Query()
	q.Set("path", result["path"].(string))
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if key := os.Getenv("SANDBOX_API_KEY"); key != "" {
		req.Header.Set("X-API-Key", key)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	hc := &http.Client{Transport: tr, Timeout: time.Minute}
	defer hc.CloseIdleConnections()
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal("sandbox download failed")
	}
	defer resp.Body.Close()
	actual, expected := sha256.New(), sha256.New()
	n, err := io.Copy(actual, resp.Body)
	if err != nil || resp.StatusCode != 200 || n != fileSize {
		t.Fatalf("download status=%d bytes=%d", resp.StatusCode, n)
	}
	_, _ = io.CopyN(expected, zeroReader{}, fileSize)
	if string(actual.Sum(nil)) != string(expected.Sum(nil)) {
		t.Fatal("download checksum mismatch")
	}
	if code, _ := upload("live-flow.bin", 5); code != 409 {
		t.Fatalf("overwrite = %d", code)
	}
	current, err := store.Get(ctx, &session.GetRequest{AppName: "test", UserID: "user", SessionID: id})
	if err != nil || current.Session.Events().Len() != 0 {
		t.Fatal("upload modified conversation history")
	}
	t.Log("real Runtime -> sandbox: chunked 64 MiB, checksum, trailing name, no overwrite passed")
}
