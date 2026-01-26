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
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/adk/cmd/launcher"
)

type MockLauncher struct {
	launcher.Launcher
	Called bool
}

func (m *MockLauncher) Execute(ctx context.Context, config *launcher.Config, args []string) error {
	m.Called = true
	return nil
}

func TestObservedLauncher(t *testing.T) {
	// Setup global tracer to capture span
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	mock := &MockLauncher{}
	obsLauncher := NewObservedLauncher(mock)

	ctx := context.Background()
	// Need to enrich context with some attributes to verify propagation
	ctx = WithUserId(ctx, "user-1")
	ctx = WithSessionId(ctx, "session-1")

	err := obsLauncher.Execute(ctx, nil, []string{"arg"})
	assert.NoError(t, err)
	assert.True(t, mock.Called)

	spans := exporter.GetSpans()
	if assert.Len(t, spans, 1) {
		s := spans[0]
		assert.Equal(t, SpanInvocation, s.Name)

		// Verify attributes propagated
		var foundUser, foundSession bool
		for _, a := range s.Attributes {
			if a.Key == GenAIUserIdKey && a.Value.AsString() == "user-1" {
				foundUser = true
			}
			if a.Key == GenAISessionIdKey && a.Value.AsString() == "session-1" {
				foundSession = true
			}
		}
		assert.True(t, foundUser, "User ID not traced")
		assert.True(t, foundSession, "Session ID not traced")
	}
}
