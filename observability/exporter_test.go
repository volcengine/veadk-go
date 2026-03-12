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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestCreateLogClient(t *testing.T) {
	ctx := context.Background()
	url := "http://localhost:4317"
	headers := map[string]string{"test": "header"}

	t.Run("default protocol", func(t *testing.T) {
		exporter, err := createLogClient(ctx, url, "", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})

	t.Run("http protocol", func(t *testing.T) {
		exporter, err := createLogClient(ctx, url, "http/protobuf", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})

	t.Run("env protocol", func(t *testing.T) {
		os.Setenv(OTELExporterOTLPProtocolEnvKey, "http/json")
		defer os.Unsetenv(OTELExporterOTLPProtocolEnvKey)

		exporter, err := createLogClient(ctx, url, "", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})

	t.Run("missing url", func(t *testing.T) {
		exporter, err := createLogClient(ctx, "", "", headers)
		assert.Error(t, err)
		assert.Nil(t, exporter)
		assert.Equal(t, "OTEL_EXPORTER_OTLP_ENDPOINT is not set", err.Error())
	})
}

func TestCreateTraceClient(t *testing.T) {
	ctx := context.Background()
	url := "http://localhost:4317"
	headers := map[string]string{"test": "header"}

	t.Run("http protocol", func(t *testing.T) {
		exporter, err := createTraceClient(ctx, url, "http/protobuf", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})

	t.Run("grpc protocol", func(t *testing.T) {
		exporter, err := createTraceClient(ctx, url, "grpc", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})
}

func TestCreateMetricClient(t *testing.T) {
	ctx := context.Background()
	url := "http://localhost:4317"
	headers := map[string]string{"test": "header"}

	t.Run("http protocol", func(t *testing.T) {
		exporter, err := createMetricClient(ctx, url, "http/protobuf", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})

	t.Run("grpc protocol", func(t *testing.T) {
		exporter, err := createMetricClient(ctx, url, "grpc", headers)
		assert.NoError(t, err)
		assert.NotNil(t, exporter)
	})
}

// makeSpans creates ReadOnlySpan instances from SpanStubs for testing.
func makeSpans(stubs tracetest.SpanStubs) []sdktrace.ReadOnlySpan {
	return stubs.Snapshots()
}

func TestInMemoryExporter(t *testing.T) {
	t.Run("new exporter is empty", func(t *testing.T) {
		exp := NewInMemoryExporter()
		assert.NotNil(t, exp)
		assert.Empty(t, exp.GetSpans())
	})

	t.Run("export and retrieve spans", func(t *testing.T) {
		exp := NewInMemoryExporter()

		traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
		spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
			TraceFlags: trace.FlagsSampled,
		})

		spans := makeSpans(tracetest.SpanStubs{
			{Name: "test_span", SpanContext: spanCtx},
		})

		err := exp.ExportSpans(context.Background(), spans)
		assert.NoError(t, err)

		retrieved := exp.GetSpans()
		assert.Len(t, retrieved, 1)
		assert.Equal(t, "test_span", retrieved[0].Name())
		assert.Equal(t, traceID, exp.TraceID())
	})

	t.Run("session tracking", func(t *testing.T) {
		exp := NewInMemoryExporter()

		traceID, _ := trace.TraceIDFromHex("abcdef0123456789abcdef0123456789")
		spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
			TraceFlags: trace.FlagsSampled,
		})

		spans := makeSpans(tracetest.SpanStubs{
			{
				Name:        "call_llm",
				SpanContext: spanCtx,
				Attributes: []attribute.KeyValue{
					attribute.String("gen_ai.session.id", "session-123"),
				},
			},
		})

		err := exp.ExportSpans(context.Background(), spans)
		assert.NoError(t, err)

		sessionSpans := exp.GetSpansBySession("session-123")
		assert.Len(t, sessionSpans, 1)
		assert.Equal(t, "call_llm", sessionSpans[0].Name())

		noSpans := exp.GetSpansBySession("non-existent")
		assert.Nil(t, noSpans)
	})

	t.Run("reset clears all data", func(t *testing.T) {
		exp := NewInMemoryExporter()

		traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
		spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
			TraceFlags: trace.FlagsSampled,
		})

		spans := makeSpans(tracetest.SpanStubs{
			{
				Name:        "test_span",
				SpanContext: spanCtx,
				Attributes: []attribute.KeyValue{
					attribute.String("gen_ai.session.id", "session-123"),
				},
			},
		})

		err := exp.ExportSpans(context.Background(), spans)
		assert.NoError(t, err)
		assert.Len(t, exp.GetSpans(), 1)

		exp.Reset()
		assert.Empty(t, exp.GetSpans())
		assert.Nil(t, exp.GetSpansBySession("session-123"))
	})

	t.Run("shutdown clears all data", func(t *testing.T) {
		exp := NewInMemoryExporter()

		traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
		spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
			TraceFlags: trace.FlagsSampled,
		})

		spans := makeSpans(tracetest.SpanStubs{
			{Name: "test_span", SpanContext: spanCtx},
		})

		_ = exp.ExportSpans(context.Background(), spans)
		assert.Len(t, exp.GetSpans(), 1)

		_ = exp.Shutdown(context.Background())
		assert.Empty(t, exp.GetSpans())
	})

	t.Run("force flush is no-op", func(t *testing.T) {
		exp := NewInMemoryExporter()
		err := exp.ForceFlush(context.Background())
		assert.NoError(t, err)
	})

	t.Run("dump to file", func(t *testing.T) {
		exp := NewInMemoryExporter()

		traceID, _ := trace.TraceIDFromHex("abcdef0123456789abcdef0123456789")
		spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
			TraceFlags: trace.FlagsSampled,
		})

		parentSpanCtx := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     trace.SpanID{8, 7, 6, 5, 4, 3, 2, 1},
			TraceFlags: trace.FlagsSampled,
		})

		spans := makeSpans(tracetest.SpanStubs{
			{
				Name:        "call_llm",
				SpanContext: spanCtx,
				Parent:      parentSpanCtx,
				Attributes: []attribute.KeyValue{
					attribute.String("gen_ai.session.id", "session-dump"),
					attribute.String("gen_ai.model", "test-model"),
				},
			},
		})

		err := exp.ExportSpans(context.Background(), spans)
		require.NoError(t, err)

		dir := t.TempDir()
		filePath, err := exp.Dump("session-dump", dir)
		require.NoError(t, err)
		assert.Contains(t, filePath, "veadk_trace_session-dump_")
		assert.Contains(t, filePath, ".json")

		// Verify file content
		data, err := os.ReadFile(filePath)
		require.NoError(t, err)

		var records []map[string]interface{}
		err = json.Unmarshal(data, &records)
		require.NoError(t, err)
		assert.Len(t, records, 1)
		assert.Equal(t, "call_llm", records[0]["name"])
		assert.NotEmpty(t, records[0]["span_id"])
		assert.NotEmpty(t, records[0]["trace_id"])
	})

	t.Run("dump all spans when session is empty", func(t *testing.T) {
		exp := NewInMemoryExporter()

		traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
		spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
			TraceFlags: trace.FlagsSampled,
		})

		spans := makeSpans(tracetest.SpanStubs{
			{Name: "span1", SpanContext: spanCtx},
		})

		_ = exp.ExportSpans(context.Background(), spans)

		dir := t.TempDir()
		filePath, err := exp.Dump("", dir)
		require.NoError(t, err)

		// Verify filename does not include session prefix
		assert.Equal(t, "veadk_trace_"+traceID.String()+".json", filepath.Base(filePath))

		data, err := os.ReadFile(filePath)
		require.NoError(t, err)

		var records []map[string]interface{}
		err = json.Unmarshal(data, &records)
		require.NoError(t, err)
		assert.Len(t, records, 1)
	})
}
