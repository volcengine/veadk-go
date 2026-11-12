// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0
package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var errAIOFileMissing = errors.New("sandbox file missing")

var ErrAIOFileExists = errors.New("sandbox file already exists")

// AIOFiles accesses only the server-resolved sandbox endpoint. Errors never
// include that endpoint, its query credentials, or the remote response body.
type AIOFiles struct {
	base   *url.URL
	client *http.Client
}

func NewAIOFiles(endpoint string) (*AIOFiles, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("invalid sandbox endpoint")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &AIOFiles{base: u, client: &http.Client{Transport: transport, Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *AIOFiles) Close() { c.client.CloseIdleConnections() }
func (c *AIOFiles) request(ctx context.Context, path, contentType string, body io.Reader) (map[string]any, error) {
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), body)
	if err != nil {
		return nil, errors.New("invalid sandbox request")
	}
	req.Header.Set("Content-Type", contentType)
	if key := os.Getenv("SANDBOX_API_KEY"); key != "" {
		req.Header.Set("X-API-Key", key)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, errors.New("sandbox transport failed; result may be unknown")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sandbox HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("invalid sandbox response")
	}
	var envelope struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, errors.New("invalid sandbox response")
	}
	if !envelope.Success {
		if envelope.Data["error_type"] == "not_found" || envelope.Data["exception_type"] == "FileNotFoundError" {
			return nil, errAIOFileMissing
		}
		if envelope.Data["error_type"] == "already_exists" {
			return nil, ErrAIOFileExists
		}
		return nil, errors.New("sandbox operation failed")
	}
	if envelope.Data == nil {
		return nil, errors.New("missing sandbox result")
	}
	return envelope.Data, nil
}

// Upload streams a multipart file with chunked transfer, without buffering or
// seeking the source. The caller must close its source when ctx is cancelled.
func (c *AIOFiles) Upload(ctx context.Context, path string, source io.ReadCloser) (int64, error) {
	reader, writer := io.Pipe()
	mw := multipart.NewWriter(writer)
	done := make(chan error, 1)
	go func() {
		err := mw.WriteField("path", path)
		if err == nil {
			var part io.Writer
			part, err = mw.CreateFormFile("file", "upload")
			if err == nil {
				_, err = io.CopyBuffer(part, source, make([]byte, 32<<10))
			}
		}
		if err == nil {
			err = mw.Close()
		}
		_ = writer.CloseWithError(err)
		done <- err
	}()
	data, err := c.request(ctx, "/v1/file/upload", mw.FormDataContentType(), reader)
	_ = reader.CloseWithError(err)
	if err != nil {
		_ = source.Close()
	}
	copyErr := <-done
	if copyErr != nil && !errors.Is(copyErr, io.ErrClosedPipe) {
		return 0, copyErr
	}
	if err != nil {
		return 0, err
	}
	if copyErr != nil {
		return 0, copyErr
	}
	if data["success"] != true || data["file_path"] != path {
		return 0, errors.New("sandbox upload not confirmed")
	}
	size, ok := data["file_size"].(float64)
	if !ok || size < 0 || size != float64(int64(size)) {
		return 0, errors.New("invalid sandbox upload size")
	}
	return int64(size), nil
}
func (c *AIOFiles) operation(ctx context.Context, path string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = c.request(ctx, path, "application/json", bytes.NewReader(raw))
	return err
}
func (c *AIOFiles) Move(ctx context.Context, source, destination string) error {
	raw, _ := json.Marshal(map[string]any{"source": source, "destination": destination, "overwrite": false})
	data, err := c.request(ctx, "/v1/file/move", "application/json", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if data["destination"] != destination {
		return errors.New("sandbox move not confirmed")
	}
	return nil
}
func (c *AIOFiles) Delete(ctx context.Context, path string) error {
	err := c.operation(ctx, "/v1/file/delete", map[string]any{"path": path})
	if errors.Is(err, errAIOFileMissing) {
		return nil
	}
	return err
}
