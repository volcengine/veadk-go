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
	"iter"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/session"
)

// ObservedLauncher wraps an adk launcher to provide automatic root span and context propagation.
type ObservedLauncher struct {
	launcher.Launcher
}

func NewObservedLauncher(base launcher.Launcher) launcher.Launcher {
	return &ObservedLauncher{Launcher: base}
}

func (l *ObservedLauncher) Execute(ctx context.Context, config *launcher.Config, args []string) error {
	// Attempt to get user/session from config or env if not present in context
	userID := GetUserId(ctx)
	sessionID := GetSessionId(ctx)

	// Since launcher.Execute blocks, we wrap it in a function that yields a dummy event or error.
	tracedEvents := TraceRun(ctx, userID, sessionID, args, func(tracedCtx context.Context) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {
			err := l.Launcher.Execute(tracedCtx, config, args)
			if err != nil {
				yield(nil, err)
			}
		}
	})

	// Consume the events (Execute usually runs a server or a CLI loop)
	for _, err := range tracedEvents {
		return err
	}
	return nil
}

// TraceRun is a helper to wrap runner.Run calls with an 'invocation' span.
// This is the root span for any GenAI request.
func TraceRun(ctx context.Context, userID, sessionID string, msg any, fn func(context.Context) iter.Seq2[*session.Event, error]) iter.Seq2[*session.Event, error] {
	ctx = WithUserId(ctx, userID)
	ctx = WithSessionId(ctx, sessionID)
	tracedCtx, span := StartSpan(ctx, SpanInvocation)

	// Use centralized attribute setting logic
	SetCommonAttributes(tracedCtx, span)

	if jsonIn, err := json.Marshal(msg); err == nil {
		span.SetAttributes(attribute.String(GenAIInputValueKey, string(jsonIn)))
	}

	return func(yield func(*session.Event, error) bool) {
		defer span.End()
		for event, err := range fn(tracedCtx) {
			if err != nil {
				span.RecordError(err)
			}
			if !yield(event, err) {
				return
			}
		}
	}
}

func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	tr := otel.Tracer(InstrumentationName)
	ctx, span := tr.Start(ctx, name)

	// Span is already enriched by SpanEnrichmentProcessor via OnStart
	return ctx, span
}
