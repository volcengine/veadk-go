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

package a2a_app

import (
	"context"
	"iter"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/volcengine/veadk-go/apps"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// streamingProbe follows the model streaming flag and can wait for the client
// to receive its first delta before producing the rest of the response.
type streamingProbe struct {
	called    atomic.Bool
	streaming atomic.Bool
	completed atomic.Bool
	release   <-chan struct{}
}

func (*streamingProbe) Name() string { return "streaming_probe" }

func (m *streamingProbe) GenerateContent(ctx context.Context, _ *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.called.Store(true)
		m.streaming.Store(stream)
		if stream {
			if !yield(&model.LLMResponse{Content: genai.NewContentFromText("hel", genai.RoleModel), Partial: true}, nil) {
				return
			}
			if m.release != nil {
				select {
				case <-m.release:
				case <-ctx.Done():
					return
				}
			}
			if !yield(&model.LLMResponse{Content: genai.NewContentFromText("lo", genai.RoleModel), Partial: true}, nil) {
				return
			}
		}
		m.completed.Store(true)
		yield(&model.LLMResponse{Content: genai.NewContentFromText("hello", genai.RoleModel), TurnComplete: true}, nil)
	}
}

func TestLLMModelStreamingThroughA2A(t *testing.T) {
	for _, method := range []string{"message/stream", "message/send"} {
		t.Run(method, func(t *testing.T) {
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			m := &streamingProbe{}
			if method == "message/stream" {
				m.release = release
			}
			rootAgent, err := llmagent.New(llmagent.Config{Name: "streaming_agent", Model: m})
			if err != nil {
				t.Fatal(err)
			}
			_, client := setupHTTPServer(t, rootAgent, session.InMemoryService(), apps.DefaultApiConfig())
			// Release the model before server cleanup even if an assertion fails.
			defer unblock()
			params := &a2a.MessageSendParams{Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "hello"})}
			var taskID a2a.TaskID
			if method == "message/stream" {
				var sawDelta, sawCompleted bool
				for event, err := range client.SendStreamingMessage(t.Context(), params) {
					if err != nil {
						t.Fatal(err)
					}
					taskID = event.TaskInfo().TaskID
					switch v := event.(type) {
					case *a2a.TaskArtifactUpdateEvent:
						if partsText(v.Artifact.Parts) == "hel" {
							if m.completed.Load() {
								t.Fatal("first delta arrived after model completion")
							}
							sawDelta = true
							unblock()
						}
					case *a2a.TaskStatusUpdateEvent:
						if v.Final {
							sawCompleted = v.Status.State == a2a.TaskStateCompleted
						}
					}
				}
				if !sawDelta {
					t.Error("message/stream did not deliver a model delta")
				}
				if !sawCompleted {
					t.Error("message/stream did not complete successfully")
				}
			} else {
				result, err := client.SendMessage(t.Context(), params)
				if err != nil {
					t.Fatal(err)
				}
				taskID = result.TaskInfo().TaskID
			}
			if !m.called.Load() {
				t.Fatal("model was not called")
			}
			if !m.streaming.Load() {
				t.Error("A2A executor did not enable model streaming")
			}
			task, err := waitForTask(t.Context(), client, taskID)
			if err != nil {
				t.Fatal(err)
			}
			if task.Status.State != a2a.TaskStateCompleted {
				t.Fatalf("task state = %v, want completed", task.Status.State)
			}
			if got := taskText(task); got != "hello" {
				t.Fatalf("final artifact text = %q, want hello without duplicated deltas", got)
			}
		})
	}
}
