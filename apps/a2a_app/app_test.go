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
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/eventqueue"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/gorilla/mux"
	"github.com/volcengine/veadk-go/v2/apps"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestAgentCardsUseExplicitPublicRoutingAndIgnoreUntrustedHeaders(t *testing.T) {
	rootAgent := newContractAgent(t, func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return singleResponse("ok")
	})
	config := apps.DefaultApiConfig().
		SetPublicURL("https://agents.example/sandbox").
		SetA2APath("/internal/a2a").
		SetA2APublicPath("/public/a2a").
		SetSimpleAPIEnabled(false).
		SetWebUIEnabled(false)
	router := setupRouter(t, rootAgent, session.InMemoryService(), config)

	var cards []a2a.AgentCard
	for _, path := range []string{a2asrv.WellKnownAgentCardPath, legacyAgentCardPath} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Host = "attacker.invalid"
		request.Header.Set("Forwarded", `for=192.0.2.1;proto=http;host="attacker.invalid"`)
		request.Header.Set("X-Forwarded-Proto", "http")
		request.Header.Set("X-Forwarded-Host", "attacker.invalid")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if got, want := response.Code, http.StatusOK; got != want {
			t.Fatalf("GET %s status = %d, want %d: %s", path, got, want, response.Body.String())
		}
		if got, want := response.Header().Get("Content-Type"), "application/json"; got != want {
			t.Fatalf("GET %s Content-Type = %q, want %q", path, got, want)
		}
		var card a2a.AgentCard
		if err := decodeJSON(response, &card); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		cards = append(cards, card)
	}

	for i, card := range cards {
		if got, want := primaryAgentCardURL(&card), "https://agents.example/sandbox/public/a2a"; got != want {
			t.Fatalf("card[%d] primary URL = %q, want %q", i, got, want)
		}
		if !card.Capabilities.Streaming {
			t.Fatalf("card[%d] does not declare verified streaming support", i)
		}
		if card.Name != rootAgent.Name() || card.Description != rootAgent.Description() {
			t.Fatalf("card[%d] identity = %q/%q", i, card.Name, card.Description)
		}
	}

	publicRequest := httptest.NewRequest(http.MethodPost, "/public/a2a", strings.NewReader(`{}`))
	publicResponse := httptest.NewRecorder()
	router.ServeHTTP(publicResponse, publicRequest)
	if got, want := publicResponse.Code, http.StatusNotFound; got != want {
		t.Fatalf("public path status = %d, want %d", got, want)
	}

	internalRequest := httptest.NewRequest(http.MethodPost, "/internal/a2a", strings.NewReader(`{"jsonrpc":"2.0","id":"1","method":"unknown"}`))
	internalResponse := httptest.NewRecorder()
	router.ServeHTTP(internalResponse, internalRequest)
	if got, want := internalResponse.Code, http.StatusOK; got != want {
		t.Fatalf("internal path status = %d, want %d: %s", got, want, internalResponse.Body.String())
	}
}

func TestSetupRoutersRejectsMissingHostDependencies(t *testing.T) {
	app := NewAgentkitA2AServerApp(nil)
	if err := app.SetupRouters(nil, &apps.RunConfig{}); err == nil {
		t.Fatal("SetupRouters(nil router) error = nil")
	}
	if err := app.SetupRouters(mux.NewRouter(), nil); err == nil {
		t.Fatal("SetupRouters(nil config) error = nil")
	}
	if err := app.SetupRouters(mux.NewRouter(), &apps.RunConfig{}); err == nil {
		t.Fatal("SetupRouters(missing agent) error = nil")
	}
}

func TestAgentCardForwardedURLIsExplicitOptIn(t *testing.T) {
	rootAgent := newContractAgent(t, func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return singleResponse("ok")
	})
	config := apps.DefaultApiConfig().
		SetPublicURL("https://configured.example/base").
		SetA2APublicPath("/edge/a2a").
		SetAgentCardURLResolver(apps.ForwardedAgentCardURL)
	router := setupRouter(t, rootAgent, session.InMemoryService(), config)

	request := httptest.NewRequest(http.MethodGet, a2asrv.WellKnownAgentCardPath, nil)
	request.Host = "internal:8024"
	request.Header.Set("Forwarded", `for=192.0.2.1;proto=https;host="gateway.example:8443"`)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var card a2a.AgentCard
	if err := decodeJSON(response, &card); err != nil {
		t.Fatal(err)
	}
	if got, want := primaryAgentCardURL(&card), "https://gateway.example:8443/edge/a2a"; got != want {
		t.Fatalf("card primary URL = %q, want %q", got, want)
	}

	request = httptest.NewRequest(http.MethodGet, a2asrv.WellKnownAgentCardPath, nil)
	request.Host = "bad host"
	request.Header.Set("Forwarded", `proto=javascript;host="user@gateway.example"`)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if err := decodeJSON(response, &card); err != nil {
		t.Fatal(err)
	}
	if got, want := primaryAgentCardURL(&card), "https://configured.example/base/edge/a2a"; got != want {
		t.Fatalf("invalid forwarded card primary URL = %q, want fallback %q", got, want)
	}
}

func TestSendPollFailureMetadataHistoryAndSessionIsolation(t *testing.T) {
	sessionService := session.InMemoryService()
	rootAgent := newContractAgent(t, func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		prompt := contentText(ctx.UserContent())
		if strings.HasPrefix(prompt, "fail:") {
			return func(yield func(*session.Event, error) bool) {
				yield(nil, errors.New("expected agent failure"))
			}
		}
		return singleResponse("result:" + prompt)
	})
	metadataCapture := &metadataCaptureInterceptor{}
	_, client := setupHTTPServerWithOptions(
		t,
		rootAgent,
		sessionService,
		apps.DefaultApiConfig().SetA2APath("/rpc"),
		a2asrv.WithExecutorContextInterceptor(metadataCapture),
	)

	const sessionCount = 8
	type result struct {
		contextID string
		task      *a2a.Task
		err       error
	}
	results := make(chan result, sessionCount)
	for i := range sessionCount {
		contextID := fmt.Sprintf("session-%02d", i)
		go func() {
			message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("prompt:"+contextID))
			message.ContextID = contextID
			historyLength := 20
			initial, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{
				Message:  message,
				Metadata: map[string]any{"request_id": contextID},
				Config:   &a2a.SendMessageConfig{HistoryLength: &historyLength},
			})
			if err != nil {
				results <- result{contextID: contextID, err: err}
				return
			}
			if initial.TaskInfo().TaskID == "" {
				results <- result{contextID: contextID, err: errors.New("message/send returned an empty task id")}
				return
			}
			task, err := waitForTask(t.Context(), client, initial.TaskInfo().TaskID)
			results <- result{contextID: contextID, task: task, err: err}
		}()
	}

	for range sessionCount {
		result := <-results
		if result.err != nil {
			t.Fatalf("%s: %v", result.contextID, result.err)
		}
		if result.task.Status.State != a2a.TaskStateCompleted {
			t.Fatalf("%s state = %q, want completed", result.contextID, result.task.Status.State)
		}
		if result.task.ContextID != result.contextID {
			t.Fatalf("%s task context = %q", result.contextID, result.task.ContextID)
		}
		if got, want := taskText(result.task), "result:prompt:"+result.contextID; !strings.Contains(got, want) {
			t.Fatalf("%s artifact text = %q, want %q", result.contextID, got, want)
		}
		if got, want := result.task.Metadata["adk_session_id"], any(result.contextID); got != want {
			t.Fatalf("%s metadata session = %v, want %v", result.contextID, got, want)
		}
		if !metadataCapture.saw(result.contextID) {
			t.Fatalf("%s request metadata did not reach the A2A request context", result.contextID)
		}

		historyLength := 20
		withHistory, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: result.task.ID, HistoryLength: &historyLength})
		if err != nil {
			t.Fatalf("%s tasks/get with history: %v", result.contextID, err)
		}
		if len(withHistory.History) != 1 || messageText(withHistory.History[0]) != "prompt:"+result.contextID {
			t.Fatalf("%s history = %#v, want its input message", result.contextID, withHistory.History)
		}

		historyLength = 0
		withoutHistory, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: result.task.ID, HistoryLength: &historyLength})
		if err != nil {
			t.Fatalf("%s tasks/get without history: %v", result.contextID, err)
		}
		if len(withoutHistory.History) != 0 {
			t.Fatalf("%s history length = %d, want 0", result.contextID, len(withoutHistory.History))
		}

		sessions, err := sessionService.List(t.Context(), &session.ListRequest{
			AppName: rootAgent.Name(),
			UserID:  "A2A_USER_" + result.contextID,
		})
		if err != nil {
			t.Fatalf("%s list session: %v", result.contextID, err)
		}
		if len(sessions.Sessions) != 1 || sessions.Sessions[0].ID() != result.contextID {
			t.Fatalf("%s sessions = %#v, want one isolated session", result.contextID, sessions.Sessions)
		}
		storedSession, err := sessionService.Get(t.Context(), &session.GetRequest{
			AppName:   rootAgent.Name(),
			UserID:    "A2A_USER_" + result.contextID,
			SessionID: result.contextID,
		})
		if err != nil {
			t.Fatalf("%s get session: %v", result.contextID, err)
		}
		if sessionEventsText(storedSession.Session) != "prompt:"+result.contextID+"|result:prompt:"+result.contextID {
			t.Fatalf("%s session events leaked or were incomplete: %q", result.contextID, sessionEventsText(storedSession.Session))
		}
	}

	failureMessage := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("fail:now"))
	failureMessage.ContextID = "failed-session"
	failureResult, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: failureMessage})
	if err != nil {
		t.Fatalf("send failed task: %v", err)
	}
	failedTask, err := waitForTask(t.Context(), client, failureResult.TaskInfo().TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := failedTask.Status.State, a2a.TaskStateFailed; got != want {
		t.Fatalf("failed task state = %q, want %q", got, want)
	}
	if failedTask.Status.Message == nil || !strings.Contains(messageText(failedTask.Status.Message), "expected agent failure") {
		t.Fatalf("failed task message = %#v", failedTask.Status.Message)
	}

	emptyMessage := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(""))
	emptyMessage.ContextID = "empty-text-session"
	emptyResult, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{Message: emptyMessage})
	if err != nil {
		t.Fatalf("send empty text message: %v", err)
	}
	emptyTask, err := waitForTask(t.Context(), client, emptyResult.TaskInfo().TaskID)
	if err != nil || emptyTask.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("empty text task = %#v, err = %v", emptyTask, err)
	}

	_, err = client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: "missing-task"})
	if !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("unknown tasks/get error = %v, want ErrTaskNotFound", err)
	}
}

func TestStreamingSSEEventOrderArtifactsTerminalAndDisconnect(t *testing.T) {
	rootAgent := newContractAgent(t, func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			for _, event := range []*session.Event{
				responseEvent("hel", true),
				responseEvent("lo", true),
				responseEvent("hello", false),
			} {
				if !yield(event, nil) {
					return
				}
			}
		}
	})
	_, client := setupHTTPServer(t, rootAgent, session.InMemoryService(), apps.DefaultApiConfig().SetA2APath("/stream"))

	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("stream"))
	var events []a2a.Event
	for event, err := range client.SendStreamingMessage(t.Context(), &a2a.SendMessageRequest{Message: message}) {
		if err != nil {
			t.Fatalf("message/stream error = %v", err)
		}
		events = append(events, event)
	}
	if len(events) < 7 {
		t.Fatalf("message/stream returned %d events, want submitted/working/artifacts/final", len(events))
	}
	initial, ok := events[0].(*a2a.Task)
	if !ok || initial.Status.State != a2a.TaskStateSubmitted {
		t.Fatalf("events[0] = %#v, want submitted task", events[0])
	}
	working, ok := events[1].(*a2a.TaskStatusUpdateEvent)
	if !ok || working.Status.State != a2a.TaskStateWorking || working.Status.State.Terminal() {
		t.Fatalf("events[1] = %#v, want non-terminal working status", events[1])
	}
	var artifactTexts []string
	var sawLastChunk bool
	for _, event := range events[2 : len(events)-1] {
		if event.TaskInfo() != initial.TaskInfo() {
			t.Fatalf("event task info = %#v, want %#v", event.TaskInfo(), initial.TaskInfo())
		}
		if artifact, ok := event.(*a2a.TaskArtifactUpdateEvent); ok {
			artifactTexts = append(artifactTexts, partsText(artifact.Artifact.Parts))
			sawLastChunk = sawLastChunk || artifact.LastChunk
		}
	}
	if got := strings.Join(artifactTexts, "|"); !strings.Contains(got, "hel") || !strings.Contains(got, "lo") || !strings.Contains(got, "hello") {
		t.Fatalf("stream artifact texts = %q", got)
	}
	if !sawLastChunk {
		t.Fatal("stream did not contain a final artifact chunk")
	}
	terminal, ok := events[len(events)-1].(*a2a.TaskStatusUpdateEvent)
	if !ok || terminal.Status.State != a2a.TaskStateCompleted || !terminal.Status.State.Terminal() {
		t.Fatalf("last event = %#v, want final completed status", events[len(events)-1])
	}

	errorAgent := newContractAgent(t, func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			yield(nil, errors.New("stream execution failed"))
		}
	})
	_, errorClient := setupHTTPServer(t, errorAgent, session.InMemoryService(), apps.DefaultApiConfig().SetA2APath("/stream"))
	var errorEvents []a2a.Event
	for event, err := range errorClient.SendStreamingMessage(t.Context(), &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("fail stream")),
	}) {
		if err != nil {
			t.Fatalf("failed task must be represented as an A2A event: %v", err)
		}
		errorEvents = append(errorEvents, event)
	}
	if len(errorEvents) == 0 {
		t.Fatal("failed stream returned no A2A events")
	}
	failedTerminal, ok := errorEvents[len(errorEvents)-1].(*a2a.TaskStatusUpdateEvent)
	if !ok || failedTerminal.Status.State != a2a.TaskStateFailed || !failedTerminal.Status.State.Terminal() {
		t.Fatalf("failed stream terminal event = %#v", errorEvents[len(errorEvents)-1])
	}
	if failedTerminal.Status.Message == nil || !strings.Contains(messageText(failedTerminal.Status.Message), "stream execution failed") {
		t.Fatalf("failed stream message = %#v", failedTerminal.Status.Message)
	}

	releaseDisconnectedTask := make(chan struct{})
	var releaseDisconnectedTaskOnce sync.Once
	defer releaseDisconnectedTaskOnce.Do(func() { close(releaseDisconnectedTask) })
	disconnectedTaskFinished := make(chan struct{})
	disconnectAgent := newContractAgent(t, func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			defer close(disconnectedTaskFinished)
			if !yield(responseEvent("first-chunk", true), nil) {
				return
			}
			<-releaseDisconnectedTask
			yield(responseEvent("after-disconnect", false), nil)
		}
	})
	_, disconnectClient := setupHTTPServer(t, disconnectAgent, session.InMemoryService(), apps.DefaultApiConfig().SetA2APath("/stream"))
	disconnectCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var disconnectedTaskID a2a.TaskID
	for event, err := range disconnectClient.SendStreamingMessage(disconnectCtx, &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("disconnect")),
	}) {
		if err != nil {
			t.Fatalf("disconnect stream error = %v", err)
		}
		if task, ok := event.(*a2a.Task); ok {
			disconnectedTaskID = task.ID
		}
		if _, ok := event.(*a2a.TaskArtifactUpdateEvent); ok {
			cancel()
			break
		}
	}
	if disconnectedTaskID == "" {
		t.Fatal("disconnected stream did not return a task id")
	}
	releaseDisconnectedTaskOnce.Do(func() { close(releaseDisconnectedTask) })
	select {
	case <-disconnectedTaskFinished:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not finish after the SSE client disconnected")
	}
	disconnectedTask, err := waitForTask(t.Context(), disconnectClient, disconnectedTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if disconnectedTask.Status.State != a2a.TaskStateCompleted || !strings.Contains(taskText(disconnectedTask), "after-disconnect") {
		t.Fatalf("task after stream disconnect = %#v", disconnectedTask)
	}
}

func TestCancelStopsRunningAgentIsIdempotentAndRejectsTerminalOrUnknown(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	var startedOnce sync.Once
	var stoppedOnce sync.Once
	rootAgent := newContractAgent(t, func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			startedOnce.Do(func() { close(started) })
			<-ctx.Done()
			stoppedOnce.Do(func() { close(stopped) })
			yield(nil, ctx.Err())
		}
	})
	_, client := setupHTTPServer(t, rootAgent, session.InMemoryService(), apps.DefaultApiConfig().SetA2APath("/cancel"))

	result, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("wait")),
		Config:  &a2a.SendMessageConfig{ReturnImmediately: true},
	})
	if err != nil {
		t.Fatalf("message/send: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("agent did not start")
	}
	canceled, err := client.CancelTask(t.Context(), &a2a.CancelTaskRequest{ID: result.TaskInfo().TaskID})
	if err != nil {
		t.Fatalf("tasks/cancel: %v", err)
	}
	if got, want := canceled.Status.State, a2a.TaskStateCanceled; got != want {
		t.Fatalf("canceled state = %q, want %q", got, want)
	}
	second, err := client.CancelTask(t.Context(), &a2a.CancelTaskRequest{ID: result.TaskInfo().TaskID})
	if err != nil {
		t.Fatalf("second tasks/cancel: %v", err)
	}
	if second.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("second tasks/cancel state = %q", second.Status.State)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("running agent did not observe cancellation")
	}
	persisted, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: result.TaskInfo().TaskID})
	if err != nil || persisted.Status.State != a2a.TaskStateCanceled {
		t.Fatalf("persisted canceled task = %#v, err = %v", persisted, err)
	}

	quickAgent := newContractAgent(t, func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return singleResponse("done")
	})
	_, quickClient := setupHTTPServer(t, quickAgent, session.InMemoryService(), apps.DefaultApiConfig().SetA2APath("/cancel"))
	quickResult, err := quickClient.SendMessage(t.Context(), &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("finish")),
	})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := waitForTask(t.Context(), quickClient, quickResult.TaskInfo().TaskID)
	if err != nil || completed.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("completed task = %#v, err = %v", completed, err)
	}
	_, err = quickClient.CancelTask(t.Context(), &a2a.CancelTaskRequest{ID: completed.ID})
	if !errors.Is(err, a2a.ErrTaskNotCancelable) {
		t.Fatalf("cancel completed error = %v, want ErrTaskNotCancelable", err)
	}
	_, err = quickClient.CancelTask(t.Context(), &a2a.CancelTaskRequest{ID: "missing-task"})
	if !errors.Is(err, a2a.ErrTaskNotFound) {
		t.Fatalf("cancel unknown error = %v, want ErrTaskNotFound", err)
	}
}

func TestTaskPollingPreservesRejectedCanceledAndUnknownStates(t *testing.T) {
	store := &fixedTaskStore{tasks: make(map[a2a.TaskID]*a2a.Task)}
	for _, state := range []a2a.TaskState{a2a.TaskStateRejected, a2a.TaskStateCanceled, a2a.TaskStateUnspecified} {
		id := a2a.TaskID("task-" + state)
		store.tasks[id] = &a2a.Task{ID: id, ContextID: "context-" + string(state), Status: a2a.TaskStatus{State: state}}
	}
	rootAgent := newContractAgent(t, func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return singleResponse("unused")
	})
	_, client := setupHTTPServerWithOptions(
		t,
		rootAgent,
		session.InMemoryService(),
		apps.DefaultApiConfig().SetA2APath("/states"),
		a2asrv.WithTaskStore(store),
	)

	for _, want := range []a2a.TaskState{a2a.TaskStateRejected, a2a.TaskStateCanceled, a2a.TaskStateUnspecified} {
		task, err := client.GetTask(t.Context(), &a2a.GetTaskRequest{ID: a2a.TaskID("task-" + want)})
		if err != nil {
			t.Fatalf("tasks/get %s: %v", want, err)
		}
		if task.Status.State != want {
			t.Fatalf("tasks/get state = %q, want %q", task.Status.State, want)
		}
	}
}

func TestCancelCompleteRaceAlwaysLeavesATerminalTask(t *testing.T) {
	for i := range 12 {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			release := make(chan struct{})
			started := make(chan struct{})
			finished := make(chan struct{})
			rootAgent := newContractAgent(t, func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
				return func(yield func(*session.Event, error) bool) {
					defer close(finished)
					close(started)
					select {
					case <-ctx.Done():
						return
					case <-release:
						yield(responseEvent("won completion race", false), nil)
					}
				}
			})
			_, client := setupHTTPServer(t, rootAgent, session.InMemoryService(), apps.DefaultApiConfig().SetA2APath("/race"))
			result, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{
				Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("race")),
				Config:  &a2a.SendMessageConfig{ReturnImmediately: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("agent did not start")
			}

			startRace := make(chan struct{})
			cancelErr := make(chan error, 1)
			go func() {
				<-startRace
				_, err := client.CancelTask(t.Context(), &a2a.CancelTaskRequest{ID: result.TaskInfo().TaskID})
				cancelErr <- err
			}()
			go func() {
				<-startRace
				close(release)
			}()
			close(startRace)

			err = <-cancelErr
			if err != nil && !errors.Is(err, a2a.ErrTaskNotCancelable) && !errors.Is(err, eventqueue.ErrQueueClosed) {
				t.Fatalf("cancel race error = %v", err)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("racing agent did not stop")
			}
			task, err := waitForTask(t.Context(), client, result.TaskInfo().TaskID)
			if err != nil {
				t.Fatal(err)
			}
			if task.Status.State != a2a.TaskStateCompleted && task.Status.State != a2a.TaskStateCanceled {
				t.Fatalf("cancel/complete race state = %q", task.Status.State)
			}
		})
	}
}

func TestA2AContractThroughRealServerRun(t *testing.T) {
	rootAgent := newContractAgent(t, func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return singleResponse("network-result")
	})
	port := reservePort(t)
	config := apps.DefaultApiConfig().
		SetHost("127.0.0.1").
		SetPort(port).
		SetA2APath("/internal/a2a").
		SetA2APublicPath("/public/a2a").
		SetPublicURL("https://agent.example/sandbox").
		SetSimpleAPIEnabled(false).
		SetWebUIEnabled(false)
	app := NewAgentkitA2AServerApp(config)
	runCtx, cancelRun := context.WithCancel(t.Context())
	runErr := make(chan error, 1)
	go func() {
		runErr <- app.Run(runCtx, &apps.RunConfig{
			AgentLoader:          agent.NewSingleLoader(rootAgent),
			SessionService:       session.InMemoryService(),
			DisableObservability: true,
		})
	}()
	waitForServer(t, config.ListenAddress())

	cardResponse, err := (&http.Client{Timeout: time.Second}).Get(config.GetWebUrl() + a2asrv.WellKnownAgentCardPath)
	if err != nil {
		cancelRun()
		t.Fatalf("get Agent Card: %v", err)
	}
	defer func() {
		if err := cardResponse.Body.Close(); err != nil {
			t.Errorf("close Agent Card response: %v", err)
		}
	}()
	var card a2a.AgentCard
	if err := json.NewDecoder(cardResponse.Body).Decode(&card); err != nil {
		cancelRun()
		t.Fatalf("decode Agent Card: %v", err)
	}
	if got, want := primaryAgentCardURL(&card), "https://agent.example/sandbox/public/a2a"; got != want {
		cancelRun()
		t.Fatalf("card primary URL = %q, want %q", got, want)
	}

	clientCard := &a2a.AgentCard{
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(config.GetWebUrl()+config.GetA2APath(), a2a.TransportProtocolJSONRPC),
		},
		Capabilities: a2a.AgentCapabilities{Streaming: true},
	}
	client, err := a2aclient.NewFromCard(t.Context(), clientCard)
	if err != nil {
		cancelRun()
		t.Fatal(err)
	}
	result, err := client.SendMessage(t.Context(), &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("network")),
	})
	if err != nil {
		cancelRun()
		t.Fatal(err)
	}
	task, err := waitForTask(t.Context(), client, result.TaskInfo().TaskID)
	if err != nil || task.Status.State != a2a.TaskStateCompleted || !strings.Contains(taskText(task), "network-result") {
		cancelRun()
		t.Fatalf("network task = %#v, err = %v", task, err)
	}

	cancelRun()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("app.Run(): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("app.Run() did not stop")
	}
}

func newContractAgent(t *testing.T, run func(agent.InvocationContext) iter.Seq2[*session.Event, error]) agent.Agent {
	t.Helper()
	result, err := agent.New(agent.Config{
		Name:        "a2a_contract_agent",
		Description: "Exercises A2A host contracts.",
		Run:         run,
	})
	if err != nil {
		t.Fatalf("agent.New(): %v", err)
	}
	return result
}

func setupRouter(t *testing.T, rootAgent agent.Agent, sessionService session.Service, config *apps.ApiConfig, options ...a2asrv.RequestHandlerOption) *mux.Router {
	t.Helper()
	router := mux.NewRouter()
	app := NewAgentkitA2AServerApp(config)
	if err := app.SetupRouters(router, &apps.RunConfig{
		AgentLoader:          agent.NewSingleLoader(rootAgent),
		SessionService:       sessionService,
		A2AOptions:           options,
		DisableObservability: true,
	}); err != nil {
		t.Fatalf("SetupRouters(): %v", err)
	}
	return router
}

func setupHTTPServer(t *testing.T, rootAgent agent.Agent, sessionService session.Service, config *apps.ApiConfig) (*httptest.Server, *a2aclient.Client) {
	t.Helper()
	return setupHTTPServerWithOptions(t, rootAgent, sessionService, config)
}

func setupHTTPServerWithOptions(t *testing.T, rootAgent agent.Agent, sessionService session.Service, config *apps.ApiConfig, options ...a2asrv.RequestHandlerOption) (*httptest.Server, *a2aclient.Client) {
	t.Helper()
	router := setupRouter(t, rootAgent, sessionService, config, options...)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	card := &a2a.AgentCard{
		Name: rootAgent.Name(),
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(server.URL+config.GetA2APath(), a2a.TransportProtocolJSONRPC),
		},
		Capabilities: a2a.AgentCapabilities{Streaming: true},
	}
	client, err := a2aclient.NewFromCard(
		t.Context(),
		card,
		a2aclient.WithJSONRPCTransport(&http.Client{Timeout: 2 * time.Second}),
	)
	if err != nil {
		t.Fatalf("a2aclient.NewFromCard(): %v", err)
	}
	t.Cleanup(func() {
		if err := client.Destroy(); err != nil {
			t.Errorf("client.Destroy(): %v", err)
		}
	})
	return server, client
}

func singleResponse(text string) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		yield(responseEvent(text, false), nil)
	}
}

func responseEvent(text string, partial bool) *session.Event {
	return &session.Event{LLMResponse: model.LLMResponse{
		Content: genai.NewContentFromText(text, genai.RoleModel),
		Partial: partial,
	}}
}

func waitForTask(ctx context.Context, client *a2aclient.Client, id a2a.TaskID) (*a2a.Task, error) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := client.GetTask(ctx, &a2a.GetTaskRequest{ID: id})
		if err != nil {
			return nil, err
		}
		if task.Status.State.Terminal() {
			return task, nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil, fmt.Errorf("task %s did not reach a terminal state", id)
}

func taskText(task *a2a.Task) string {
	var result []string
	for _, artifact := range task.Artifacts {
		if artifact != nil {
			if text := partsText(artifact.Parts); text != "" {
				result = append(result, text)
			}
		}
	}
	return strings.Join(result, "|")
}

func messageText(message *a2a.Message) string {
	if message == nil {
		return ""
	}
	return partsText(message.Parts)
}

func partsText(parts a2a.ContentParts) string {
	var result []string
	for _, part := range parts {
		if part == nil {
			continue
		}
		if text := part.Text(); text != "" {
			result = append(result, text)
		}
	}
	return strings.Join(result, "")
}

func contentText(content *genai.Content) string {
	if content == nil {
		return ""
	}
	var result strings.Builder
	for _, part := range content.Parts {
		result.WriteString(part.Text)
	}
	return result.String()
}

func sessionEventsText(value session.Session) string {
	var result []string
	for event := range value.Events().All() {
		if event.Content != nil {
			if text := contentText(event.Content); text != "" {
				result = append(result, text)
			}
		}
	}
	return strings.Join(result, "|")
}

func decodeJSON(response *httptest.ResponseRecorder, target any) error {
	return json.NewDecoder(response.Body).Decode(target)
}

func reservePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func waitForServer(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 20*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("server did not listen on %s", address)
}

func primaryAgentCardURL(card *a2a.AgentCard) string {
	if card == nil {
		return ""
	}
	for _, iface := range card.SupportedInterfaces {
		if iface != nil && iface.ProtocolBinding == a2a.TransportProtocolJSONRPC {
			return iface.URL
		}
	}
	for _, iface := range card.SupportedInterfaces {
		if iface != nil {
			return iface.URL
		}
	}
	return ""
}

type fixedTaskStore struct {
	tasks map[a2a.TaskID]*a2a.Task
}

type metadataCaptureInterceptor struct {
	seenIDs sync.Map
}

func (i *metadataCaptureInterceptor) Intercept(ctx context.Context, execCtx *a2asrv.ExecutorContext) (context.Context, error) {
	if requestID, ok := execCtx.Metadata["request_id"].(string); ok {
		i.seenIDs.Store(requestID, struct{}{})
	}
	return ctx, nil
}

func (i *metadataCaptureInterceptor) saw(requestID string) bool {
	_, ok := i.seenIDs.Load(requestID)
	return ok
}

func (s *fixedTaskStore) Create(context.Context, *a2a.Task) (taskstore.TaskVersion, error) {
	return taskstore.TaskVersionMissing, errors.New("fixed task store is read-only")
}

func (s *fixedTaskStore) Update(context.Context, *taskstore.UpdateRequest) (taskstore.TaskVersion, error) {
	return taskstore.TaskVersionMissing, errors.New("fixed task store is read-only")
}

func (s *fixedTaskStore) Get(_ context.Context, id a2a.TaskID) (*taskstore.StoredTask, error) {
	task, ok := s.tasks[id]
	if !ok {
		return nil, a2a.ErrTaskNotFound
	}
	copy := *task
	return &taskstore.StoredTask{Task: &copy, Version: 1}, nil
}

func (s *fixedTaskStore) List(context.Context, *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return nil, errors.New("list is not supported by fixed task store")
}
