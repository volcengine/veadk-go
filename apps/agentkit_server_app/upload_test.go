// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package agentkit_server_app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"google.golang.org/adk/v2/session"
)

type fakeFiles struct {
	mu        sync.Mutex
	files     map[string][]byte
	first     chan struct{}
	once      sync.Once
	chunked   bool
	failEarly bool
}

func (f *fakeFiles) serve(w http.ResponseWriter, r *http.Request) {
	success := true
	data := map[string]any{}
	switch r.URL.Path {
	case "/v1/file/upload":
		f.mu.Lock()
		early := f.failEarly
		f.mu.Unlock()
		if early {
			_ = http.NewResponseController(w).EnableFullDuplex()
			w.WriteHeader(503)
			_ = http.NewResponseController(w).Flush()
			return
		}
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "bad multipart", 400)
			return
		}
		var path string
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, "bad multipart", 400)
				return
			}
			if part.FormName() == "path" {
				raw, _ := io.ReadAll(part)
				path = string(raw)
				continue
			}
			first := make([]byte, 1024)
			n, err := io.ReadFull(part, first)
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				http.Error(w, "read failed", 400)
				return
			}
			f.mu.Lock()
			f.chunked = r.ContentLength < 0
			f.mu.Unlock()
			f.once.Do(func() { close(f.first) })
			rest, err := io.ReadAll(part)
			if err != nil {
				http.Error(w, "read failed", 400)
				return
			}
			body := append(first[:n], rest...)
			f.mu.Lock()
			f.files[path] = body
			f.mu.Unlock()
			data = map[string]any{"file_path": path, "file_size": len(body), "success": true}
		}
	case "/v1/file/move":
		var body struct {
			Source, Destination string
			Overwrite           bool
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, exists := f.files[body.Destination]; exists && !body.Overwrite {
			success = false
			data["error_type"] = "already_exists"
		} else {
			f.files[body.Destination] = f.files[body.Source]
			delete(f.files, body.Source)
			data["destination"] = body.Destination
		}
	case "/v1/file/delete":
		var body struct{ Path string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		delete(f.files, body.Path)
		f.mu.Unlock()
		data["path"] = body.Path
	default:
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"success": success, "data": data})
}
func uploadFixture(t *testing.T) (*httptest.Server, *fakeFiles, session.Service) {
	t.Helper()
	files := &fakeFiles{files: map[string][]byte{}, first: make(chan struct{})}
	sandbox := httptest.NewServer(http.HandlerFunc(files.serve))
	t.Cleanup(sandbox.Close)
	var mu sync.Mutex
	physicalUser := ""
	deleted := false
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		result := map[string]any{}
		switch r.URL.Query().Get("Action") {
		case "ListSessions":
			result["SessionInfos"] = []any{}
		case "CreateSession":
			physicalUser, _ = body["UserSessionId"].(string)
			deleted = false
			result["SessionId"] = "physical"
		case "GetSession":
			status := "Ready"
			if deleted {
				status = "Deleted"
			}
			result = map[string]any{"SessionId": "physical", "UserSessionId": physicalUser, "Status": status, "Endpoint": sandbox.URL}
		case "DeleteSession":
			deleted = true
		default:
			t.Errorf("unexpected action %s", r.URL.Query().Get("Action"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": result})
	}))
	t.Cleanup(control.Close)
	u, _ := url.Parse(control.URL)
	for key, value := range map[string]string{"AGENTKIT_TOOL_ID": "tool-test", "VOLCENGINE_ACCESS_KEY": "test-ak", "VOLCENGINE_SECRET_KEY": "test-sk", "AGENTKIT_TOOL_SCHEME": "http", "AGENTKIT_TOOL_HOST": u.Host, "AGENTKIT_TOOL_SERVICE_CODE": "agentkit", "AGENTKIT_TOOL_REGION": "cn-beijing"} {
		t.Setenv(key, value)
	}
	store := session.InMemoryService()
	manager, err := agentkit.NewAIOLifecycle(agentkit.AIOLifecycleConfig{SessionService: store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(context.Background(), "test", "user", "session", nil); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(sessionRouter(t, store, WithAIOLifecycle(manager)))
	t.Cleanup(api.Close)
	return api, files, store
}
func postUpload(t *testing.T, api *httptest.Server, makeBody func(*multipart.Writer)) (*http.Response, []byte) {
	return postUploadPath(t, api, sessionBase+"/session/files", makeBody)
}
func postUploadPath(t *testing.T, api *httptest.Server, path string, makeBody func(*multipart.Writer)) (*http.Response, []byte) {
	t.Helper()
	reader, writer := io.Pipe()
	mw := multipart.NewWriter(writer)
	go func() { makeBody(mw); _ = mw.Close(); _ = writer.Close() }()
	req, _ := http.NewRequest("POST", api.URL+path, reader)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}
func TestUploadStreamsBeforeClientEOFAndHoldsLock(t *testing.T) {
	api, files, store := uploadFixture(t)
	reader, writer := io.Pipe()
	mw := multipart.NewWriter(writer)
	allowEOF := make(chan struct{})
	written := make(chan struct{})
	go func() {
		defer close(written)
		part, _ := mw.CreateFormFile("file", "original.txt")
		_, _ = part.Write(bytes.Repeat([]byte("a"), 64<<10))
		<-allowEOF
		_, _ = part.Write([]byte("tail"))
		_ = mw.WriteField("name", "final.txt")
		_ = mw.Close()
		_ = writer.Close()
	}()
	req, _ := http.NewRequest("POST", api.URL+sessionBase+"/session/files", reader)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	done := make(chan *http.Response, 1)
	errs := make(chan error, 1)
	go func() {
		r, e := api.Client().Do(req)
		if e != nil {
			errs <- e
			return
		}
		done <- r
	}()
	select {
	case <-files.first:
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		close(allowEOF)
		t.Fatal("sandbox did not receive data before client EOF")
	}
	// Same-session deletion cannot race the in-progress file transfer.
	del, _ := http.NewRequest("DELETE", api.URL+sessionBase+"/session", nil)
	resp, err := api.Client().Do(del)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Errorf("delete during upload = %d", resp.StatusCode)
	}
	close(allowEOF)
	<-written
	select {
	case resp = <-done:
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("upload stalled")
	}
	defer resp.Body.Close()
	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != 201 || result["name"] != "final.txt" || result["size_bytes"] != float64((64<<10)+4) {
		t.Fatalf("%d %+v", resp.StatusCode, result)
	}
	files.mu.Lock()
	if !files.chunked || len(files.files) != 1 || len(files.files[result["path"].(string)]) != (64<<10)+4 {
		t.Error("incorrect streaming/commit")
	}
	files.mu.Unlock()
	s, _ := store.Get(context.Background(), &session.GetRequest{AppName: "test", UserID: "user", SessionID: "session"})
	if s.Session.Events().Len() != 1 || s.Session.Events().At(0).Author != "veadk_lifecycle" {
		t.Fatal("upload must only persist its lazy sandbox binding")
	}
	if _, err := s.Session.State().Get(agentkit.AIOSandboxStateKey); err != nil {
		t.Fatal("upload did not persist sandbox binding")
	}
}

type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) { clear(b); return len(b), nil }
func TestUploadLargeFileNamesAndNoOverwrite(t *testing.T) {
	api, files, _ := uploadFixture(t)
	writeFile := func(mw *multipart.Writer, name string, n int64) {
		part, _ := mw.CreateFormFile("file", name)
		_, _ = io.CopyN(part, zeroReader{}, n)
	}
	for _, tc := range []struct {
		name   string
		body   func(*multipart.Writer)
		status int
	}{
		{"large file", func(m *multipart.Writer) { writeFile(m, "large.bin", 32<<20) }, 201},
		{"duplicate", func(m *multipart.Writer) { writeFile(m, "large.bin", 5) }, 409},
		{"two files", func(m *multipart.Writer) { writeFile(m, "one.bin", 5); writeFile(m, "two.bin", 5) }, 422},
		{"late invalid name", func(m *multipart.Writer) { writeFile(m, "valid.bin", 5); _ = m.WriteField("name", "../escape") }, 422},
		{"path filename", func(m *multipart.Writer) { writeFile(m, "../escape", 5) }, 422},
		{"missing file", func(m *multipart.Writer) { _ = m.WriteField("name", "x") }, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := postUpload(t, api, tc.body)
			if resp.StatusCode != tc.status {
				t.Fatalf("%d %s", resp.StatusCode, body)
			}
		})
	}
	files.mu.Lock()
	defer files.mu.Unlock()
	if len(files.files) != 1 {
		t.Fatalf("staging files leaked: %d", len(files.files))
	}
	for p, b := range files.files {
		if !strings.HasSuffix(p, "/large.bin") || int64(len(b)) != 32<<20 {
			t.Fatal("existing file changed")
		}
	}
}

func TestUploadCancellationCleansStagingAndReleasesSession(t *testing.T) {
	api, files, _ := uploadFixture(t)
	reader, writer := io.Pipe()
	mw := multipart.NewWriter(writer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", api.URL+sessionBase+"/session/files", reader)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, _ := api.Client().Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	}()
	go func() {
		p, _ := mw.CreateFormFile("file", "interrupted.bin")
		_, _ = p.Write(bytes.Repeat([]byte("a"), 64<<10))
	}()
	select {
	case <-files.first:
	case <-time.After(5 * time.Second):
		t.Fatal("upload not forwarded")
	}
	cancel()
	writer.CloseWithError(context.Canceled)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not end transfer")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		del, _ := http.NewRequest("DELETE", api.URL+sessionBase+"/session", nil)
		resp, err := api.Client().Do(del)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			break
		}
		if resp.StatusCode != 409 || time.Now().After(deadline) {
			t.Fatalf("session lock not released: %d", resp.StatusCode)
		}
		time.Sleep(10 * time.Millisecond)
	}
	files.mu.Lock()
	defer files.mu.Unlock()
	if len(files.files) != 0 {
		t.Fatal("cancelled upload left a file")
	}
}
func TestUploadAllowsLargeKnownLengthRequest(t *testing.T) {
	api, files, _ := uploadFixture(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "large.bin")
	_, _ = io.CopyN(part, zeroReader{}, 21<<20)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", api.URL+sessionBase+"/session/files", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("large known-length request = %d", resp.StatusCode)
	}
	files.mu.Lock()
	defer files.mu.Unlock()
	if len(files.files) != 1 {
		t.Fatal("large request did not commit file")
	}
}

func TestUploadWholeRequestWithOptionalBodyLimit(t *testing.T) {
	for _, limited := range []bool{false, true} {
		name := "unlimited"
		if limited {
			name = "configured limit"
		}
		t.Run(name, func(t *testing.T) {
			api, files, _ := uploadFixture(t)
			if limited {
				next := api.Config.Handler
				wrapped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
					next.ServeHTTP(w, r)
				}))
				defer wrapped.Close()
				api = wrapped
			}
			reader, writer := io.Pipe()
			defer reader.Close()
			mw := multipart.NewWriter(writer)
			go func() {
				p, _ := mw.CreateFormFile("file", "small.bin")
				_, _ = p.Write([]byte("small"))
				_ = mw.Close()
				_, _ = io.CopyN(writer, zeroReader{}, 20<<20)
				_ = writer.Close()
			}()
			req, _ := http.NewRequest("POST", api.URL+sessionBase+"/session/files", reader)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			resp, err := api.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			wantStatus, wantFiles := 201, 1
			if limited {
				wantStatus, wantFiles = 413, 0
			}
			if resp.StatusCode != wantStatus {
				raw, _ := io.ReadAll(resp.Body)
				t.Fatalf("large request rejected: %d %s", resp.StatusCode, raw)
			}
			files.mu.Lock()
			defer files.mu.Unlock()
			if len(files.files) != wantFiles {
				t.Fatalf("committed files = %d, want %d", len(files.files), wantFiles)
			}
		})
	}
}

func TestUploadEarlySandboxFailureInterruptsStalledClient(t *testing.T) {
	api, files, _ := uploadFixture(t)
	files.mu.Lock()
	files.failEarly = true
	files.mu.Unlock()
	reader, writer := io.Pipe()
	mw := multipart.NewWriter(writer)
	go func() {
		p, _ := mw.CreateFormFile("file", "stalled.bin")
		_, _ = p.Write(bytes.Repeat([]byte("x"), 64<<10))
	}()
	req, _ := http.NewRequest("POST", api.URL+sessionBase+"/session/files", reader)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	client := &http.Client{Timeout: 3 * time.Second}
	defer writer.Close()
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("early downstream failure did not interrupt upstream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("waited for client EOF after sandbox failure")
	}
}
