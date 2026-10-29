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

package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func testArkResponses(t *testing.T, config *ArkClientConfig) *arkResponsesModel {
	t.Helper()
	if config == nil {
		config = &ArkClientConfig{APIKey: "key"}
	}
	m, err := NewArkResponsesModel(context.Background(), "vision", config)
	if err != nil {
		t.Fatal(err)
	}
	return m.(*arkResponsesModel)
}
func responsesBody(t *testing.T, m *arkResponsesModel, req *adkmodel.LLMRequest) map[string]any {
	t.Helper()
	r, err := m.convertRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestArkResponsesMedia(t *testing.T) {
	for _, mime := range []string{"image/png", "video/mp4", "application/pdf"} {
		for _, form := range []string{"inline", "url", "data", "file_id"} {
			for _, tool := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/tool=%v", mime, form, tool), func(t *testing.T) {
					p := &genai.Part{}
					uri := "https://example.com/media"
					switch form {
					case "data":
						uri = "data:" + mime + ";base64,AQID"
					case "file_id":
						uri = "file_id://file-example"
					}
					if form == "inline" {
						p.InlineData = &genai.Blob{Data: []byte{1, 2, 3}, MIMEType: mime}
					} else {
						p.FileData = &genai.FileData{FileURI: uri, MIMEType: mime}
					}
					if tool {
						attachment := &genai.FunctionResponsePart{}
						if form == "inline" {
							attachment.InlineData = &genai.FunctionResponseBlob{Data: []byte{1, 2, 3}, MIMEType: mime}
						} else {
							attachment.FileData = &genai.FunctionResponseFileData{FileURI: uri, MIMEType: mime}
						}
						p = &genai.Part{FunctionResponse: &genai.FunctionResponse{ID: "call1", Name: "read_media", Response: map[string]any{"ok": true}, Parts: []*genai.FunctionResponsePart{attachment}}}
					}
					input := responsesBody(t, testArkResponses(t, nil), &adkmodel.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: []*genai.Part{p}}}})["input"].([]any)
					index := 0
					if tool {
						index = 1
						output := input[0].(map[string]any)
						if output["call_id"] != "call1" || output["output"] != `{"ok":true}` {
							t.Fatal(output)
						}
					}
					content := input[index].(map[string]any)["content"].([]any)
					media := content[len(content)-1].(map[string]any)
					kind, key := "input_file", "file_url"
					if strings.HasPrefix(mime, "image/") {
						kind, key = "input_image", "image_url"
					}
					if strings.HasPrefix(mime, "video/") {
						kind, key = "input_video", "video_url"
					}
					want := uri
					if form == "inline" {
						want = "data:" + mime + ";base64,AQID"
						if kind == "input_file" {
							key = "file_data"
						}
					}
					if form == "data" && kind == "input_file" {
						key = "file_data"
					}
					if form == "file_id" {
						key, want = "file_id", "file-example"
					}
					if media["type"] != kind || media[key] != want {
						t.Fatalf("media=%v want %s %s=%s", media, kind, key, want)
					}
				})
			}
		}
	}
}

func TestArkResponsesHistoryAndConfig(t *testing.T) {
	temp := float32(.2)
	fps := float64(.5)
	req := &adkmodel.LLMRequest{Contents: []*genai.Content{
		{Role: "model", Parts: []*genai.Part{nil, {Text: "private reasoning", Thought: true}, {Text: "calling"}, {FunctionCall: &genai.FunctionCall{ID: "c1", Name: "read"}}, {FunctionCall: &genai.FunctionCall{ID: "c2", Name: "read"}}}},
		{Role: "user", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "c1", Name: "read", Response: map[string]any{"ok": true}, Parts: []*genai.FunctionResponsePart{genai.NewFunctionResponsePartFromBytes([]byte{1}, "image/png")}}}, {FunctionResponse: &genai.FunctionResponse{ID: "c2", Name: "read", Response: map[string]any{"ok": false}}}}},
		{Role: "user", Parts: []*genai.Part{{FileData: &genai.FileData{FileURI: "file_id://file-video", MIMEType: "video/mp4"}, VideoMetadata: &genai.VideoMetadata{FPS: &fps}}}},
	}, Config: &genai.GenerateContentConfig{MaxOutputTokens: 123, Temperature: &temp, SystemInstruction: genai.NewContentFromText("system", "system"), Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "read", Parameters: &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"path": {Type: genai.TypeString}}}}}}}}}
	before, _ := json.Marshal(req)
	m := testArkResponses(t, &ArkClientConfig{APIKey: "key", ExtraBody: map[string]any{"extra_body": map[string]any{"store": true, "previous_response_id": "other", "stream": true}}})
	b := responsesBody(t, m, req)
	input := b["input"].([]any)
	if len(input) != 7 || input[3].(map[string]any)["call_id"] != "c1" || input[5].(map[string]any)["call_id"] != "c2" {
		t.Fatalf("order: %#v", input)
	}
	if b["store"] != false || b["stream"] != false || b["previous_response_id"] != nil || b["max_output_tokens"] != float64(123) {
		t.Fatal(b)
	}
	raw, _ := json.Marshal(b)
	if strings.Contains(string(raw), "private reasoning") || !strings.Contains(string(raw), `"type":"object"`) || !strings.Contains(string(raw), `"fps":0.5`) {
		t.Fatal(string(raw))
	}
	after, _ := json.Marshal(req)
	if string(before) != string(after) {
		t.Fatal("mutated request")
	}
}

const responsesFinal = `{"status":"completed","model":"vision","new_provider_field":true,"output":[{"type":"reasoning","encrypted_content":"opaque","summary":[]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"green"}]}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":1}}}`

func TestArkResponsesHTTPAndAuth(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			provider := &rotatingTestAPIKeyProvider{keys: []string{"old", "new"}}
			var count atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/responses" {
					t.Error(r.URL.Path)
				}
				n := count.Add(1)
				if n == 1 {
					w.WriteHeader(401)
					_, _ = fmt.Fprint(w, `{"error":{"code":"expired","message":"expired"}}`)
					return
				}
				if r.Header.Get("Authorization") != "Bearer new" || r.Header.Get("X-Test") != "value" {
					t.Error("headers")
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				if body["store"] != false || body["stream"] != stream {
					t.Error(body)
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"green\"}\n\n")
					_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", responsesFinal)
				} else {
					_, _ = fmt.Fprint(w, responsesFinal)
				}
			}))
			defer srv.Close()
			m := testArkResponses(t, &ArkClientConfig{APIKeyProvider: provider, BaseURL: srv.URL, HTTPClient: srv.Client(), ExtraHeaders: map[string]string{"X-Test": "value"}})
			if provider.calls.Load() != 0 {
				t.Fatal("eager auth")
			}
			events := 0
			for out, err := range m.GenerateContent(context.Background(), &adkmodel.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", "user")}}, stream) {
				if err != nil {
					t.Fatal(err)
				}
				events++
				if !out.Partial && (out.Content.Parts[0].Text != "green" || out.UsageMetadata.CachedContentTokenCount != 4 || !out.TurnComplete) {
					t.Fatal(out)
				}
			}
			if count.Load() != 2 || provider.invalidates.Load() != 1 || events != map[bool]int{true: 2, false: 1}[stream] {
				t.Fatalf("requests=%d invalidations=%d events=%d", count.Load(), provider.invalidates.Load(), events)
			}
		})
	}
}
func TestArkResponsesStreamFailures(t *testing.T) {
	for _, data := range []string{"data: [DONE]\n\n", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n", "data: {\"type\":\"response.failed\"}\n\n", "data: malformed\n\n"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, data)
		}))
		m := testArkResponses(t, &ArkClientConfig{APIKey: "key", BaseURL: srv.URL, HTTPClient: srv.Client()})
		failed := false
		for _, err := range m.GenerateContent(context.Background(), &adkmodel.LLMRequest{}, true) {
			if err != nil {
				failed = true
			}
		}
		srv.Close()
		if !failed {
			t.Fatalf("accepted incomplete stream %q", data)
		}
	}
}
func TestArkResponsesRejectInvalidParts(t *testing.T) {
	for _, p := range []*genai.Part{
		{FileData: &genai.FileData{MIMEType: "image/png", FileURI: "/workspace/a.png"}},
		{FileData: &genai.FileData{MIMEType: "image/png", FileURI: "file:///etc/passwd"}},
		{FileData: &genai.FileData{MIMEType: "image/png", FileURI: "file_id://"}},
		{InlineData: &genai.Blob{MIMEType: "image/png"}},
		{FunctionCall: &genai.FunctionCall{Name: "f"}},
		{FunctionResponse: &genai.FunctionResponse{ID: "c", Parts: []*genai.FunctionResponsePart{nil}}},
		{FunctionResponse: &genai.FunctionResponse{ID: "c", Response: map[string]any{"invalid": make(chan int)}}},
	} {
		if _, err := testArkResponses(t, nil).convertRequest(&adkmodel.LLMRequest{Contents: []*genai.Content{{Parts: []*genai.Part{p}}}}); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
}
func TestArkResponsesFunctionCallConversion(t *testing.T) {
	var r arkResponsesResult
	if err := json.Unmarshal([]byte(`{"status":"completed","output":[{"type":"function_call","call_id":"c","name":"read","arguments":"{\"path\":\"x\"}"}]}`), &r); err != nil {
		t.Fatal(err)
	}
	out, err := convertArkResponsesResponse(&r)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.Content.Parts[0].FunctionCall, &genai.FunctionCall{ID: "c", Name: "read", Args: map[string]any{"path": "x"}}) {
		t.Fatal(out)
	}
	r.Output[0].Arguments = "null"
	if _, err := convertArkResponsesResponse(&r); err == nil {
		t.Fatal("accepted null args")
	}
}

func TestArkResponsesEarlyStopClosesStream(t *testing.T) {
	closed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m := testArkResponses(t, &ArkClientConfig{APIKey: "key", BaseURL: srv.URL, HTTPClient: srv.Client()})
	for out, err := range m.GenerateContent(ctx, &adkmodel.LLMRequest{}, true) {
		if err != nil {
			t.Fatal(err)
		}
		if !out.Partial {
			t.Fatal("expected delta")
		}
		break
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("early stop did not close stream")
	}
	if ctx.Err() != nil {
		t.Fatal("stream close waited for timeout")
	}
}

func TestArkResponsesStructuredOutputAndToolChoice(t *testing.T) {
	req := &adkmodel.LLMRequest{Config: &genai.GenerateContentConfig{
		ResponseJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}}},
		ToolConfig:         &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny, AllowedFunctionNames: []string{"read"}}},
	}}
	b := responsesBody(t, testArkResponses(t, nil), req)
	if b["tool_choice"].(map[string]any)["name"] != "read" || b["text"].(map[string]any)["format"].(map[string]any)["type"] != "json_schema" {
		t.Fatal(b)
	}
}

func TestArkResponsesInlineDocumentFilename(t *testing.T) {
	for _, part := range []*genai.Part{
		{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("pdf")}},
		{InlineData: &genai.Blob{MIMEType: "application/pdf", Data: []byte("pdf"), DisplayName: "report.pdf"}},
		{FileData: &genai.FileData{MIMEType: "application/pdf", FileURI: "data:application/pdf;base64,cGRm", DisplayName: "report.pdf"}},
	} {
		v, err := arkResponsesMedia(part)
		if err != nil {
			t.Fatal(err)
		}
		want := "report.pdf"
		if part.InlineData != nil && part.InlineData.DisplayName == "" {
			want = "attachment.pdf"
		}
		if v["filename"] != want || v["file_data"] == nil {
			t.Fatalf("document metadata: %v", v)
		}
	}
}
