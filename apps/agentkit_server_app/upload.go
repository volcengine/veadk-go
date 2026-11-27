// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0
package agentkit_server_app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/v2/apps"
	"github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"google.golang.org/adk/v2/session"
)

type uploadSource struct {
	io.Reader
	close func() error
	size  int64
}

func (s *uploadSource) Read(p []byte) (int, error) {
	n, err := s.Reader.Read(p)
	s.size += int64(n)
	return n, err
}

func (s *uploadSource) Close() error { return s.close() }

type uploadError struct {
	status  int
	message string
}

func (e *uploadError) Error() string { return e.message }
func badUpload(message string) error { return &uploadError{422, message} }
func uploadName(name string) bool {
	return name != "" && name != "." && name != ".." && utf8.ValidString(name) && len(name) <= 255 && !strings.ContainsAny(name, "/\\\x00\r\n")
}
func uploadReadError(err error) error {
	var large *http.MaxBytesError
	if errors.As(err, &large) {
		return &uploadError{413, "upload request exceeds configured body limit"}
	}
	return badUpload("invalid multipart request")
}

func (a *AgentkitServerApp) uploadHandler(cfg *apps.RunConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.aioLifecycle == nil {
			http.Error(w, "SANDBOX_DISABLED", 422)
			return
		}
		v := mux.Vars(r)
		if _, err := cfg.AgentLoader.LoadAgent(v["app_name"]); err != nil {
			http.Error(w, "unknown app", 404)
			return
		}
		var result any
		err := a.aioLifecycle.WithSandbox(r.Context(), v["app_name"], v["user_id"], v["session_id"], func(ctx context.Context, s session.Session, endpoint string) error {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			var once sync.Once
			interrupt := func() error {
				once.Do(func() {
					// A read deadline must never carry over into a reused HTTP/1 connection.
					if r.ProtoMajor == 1 {
						w.Header().Set("Connection", "close")
					}
					_ = http.NewResponseController(w).SetReadDeadline(time.Now())
					_ = r.Body.Close()
				})
				return nil
			}
			interrupted := make(chan struct{})
			stop := context.AfterFunc(ctx, func() { _ = interrupt(); close(interrupted) })
			defer func() {
				if !stop() {
					<-interrupted
				}
			}()

			files, err := agentkit.NewAIOFiles(endpoint)
			if err != nil {
				return err
			}
			defer files.Close()
			mr, err := r.MultipartReader()
			if err != nil {
				return badUpload("multipart/form-data required")
			}
			hash := sha256.Sum256([]byte(s.AppName() + "\x00" + s.UserID() + "\x00" + s.ID()))
			directory := "/workspace/uploads/" + hex.EncodeToString(hash[:16])
			staged := "/workspace/.veadk-upload-" + uuid.NewString()
			stagedUsed := false
			uploadUncertain := false
			defer func() {
				if stagedUsed {
					cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
					defer done()
					if e := files.Delete(cleanup, staged); e == nil && !uploadUncertain {
						a.aioLifecycle.ForgetFileCleanup(ctx, staged)
					}
				}
			}()
			var filename, override, mimeType string
			var size int64
			seen := map[string]bool{}
			for {
				part, e := mr.NextRawPart()
				if e == io.EOF {
					break
				}
				if e != nil {
					return uploadReadError(e)
				}
				_, params, e := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
				if e != nil {
					return badUpload("invalid content disposition")
				}
				field := part.FormName()
				if (field != "file" && field != "name") || seen[field] {
					return badUpload("expected one file and optional name")
				}
				seen[field] = true
				if field == "name" {
					if _, ok := params["filename"]; ok {
						return badUpload("name must be a text field")
					}
					data, e := io.ReadAll(io.LimitReader(part, 257))
					if e != nil {
						return uploadReadError(e)
					}
					override = string(data)
					if !uploadName(override) {
						return badUpload("invalid file name")
					}
					continue
				}
				filename = params["filename"] // Do not use Part.FileName: it silently strips directories.
				if !uploadName(filename) {
					return badUpload("invalid file name")
				}
				header := make([]byte, 512)
				n, e := io.ReadFull(part, header)
				if e != nil && e != io.EOF && e != io.ErrUnexpectedEOF {
					return uploadReadError(e)
				}
				header = header[:n]
				mimeType = http.DetectContentType(header)
				source := &uploadSource{Reader: io.MultiReader(bytes.NewReader(header), part), close: interrupt}
				if err := a.aioLifecycle.TrackFileCleanup(ctx, staged); err != nil {
					return err
				}
				stagedUsed = true
				size, e = files.Upload(ctx, staged, source)
				uploadUncertain = e != nil
				if e != nil {
					cancel()
					var large *http.MaxBytesError
					if errors.As(e, &large) {
						return uploadReadError(e)
					}
					return e
				}
				if size != source.size {
					return errors.New("sandbox upload size mismatch")
				}
			}
			if !seen["file"] {
				return badUpload("one file is required")
			}
			// Consume the epilogue too so any configured global body limit covers it.
			if _, e := io.Copy(io.Discard, r.Body); e != nil {
				return uploadReadError(e)
			}
			if override != "" {
				filename = override
			}
			target := path.Join(directory, filename)
			if err := files.Move(ctx, staged, target); err != nil {
				return err
			}
			stagedUsed = false
			a.aioLifecycle.ForgetFileCleanup(ctx, staged)
			result = map[string]any{"path": target, "name": filename, "mime_type": mimeType, "size_bytes": size}
			return nil
		})
		if err != nil {
			var e *uploadError
			if errors.As(err, &e) {
				http.Error(w, e.message, e.status)
			} else if errors.Is(err, agentkit.ErrAIOFileExists) {
				http.Error(w, "file already exists", 409)
			} else {
				writeAIOError(w, err)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, result)
	}
}
