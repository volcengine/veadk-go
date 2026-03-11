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
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMiddleware returns an HTTP middleware that instruments incoming HTTP requests with OpenTelemetry.
// It creates spans for each HTTP request and propagates trace context.
//
// Usage:
//
//	import (
//		"github.com/volcengine/veadk-go/observability"
//	)
//
//	// Wrap your handler
//	wrappedHandler := observability.HTTPMiddleware(originalHandler)
//	http.Handle("/", wrappedHandler)
func HTTPMiddleware(next http.Handler) http.Handler {
	return otelhttp.NewHandler(
		next,
		InstrumentationName,
		otelhttp.WithTracerProvider(otel.GetTracerProvider()),
		otelhttp.WithPublicEndpoint(),
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			return "HTTP " + r.Method + " " + r.URL.Path
		}),
	)
}
// StartSpan starts a new span as a child of the span in the context.
// This can be used within an HTTP handler to start a span for a specific operation.
//
// Usage:
//
//	func handler(w http.ResponseWriter, r *http.Request) {
//		ctx, span := observability.StartSpan(r.Context(), "operation_name")
//		defer span.End()
//		// ... do work ...
//	}
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	tracer := otel.GetTracerProvider().Tracer(InstrumentationName)
	return tracer.Start(ctx, name, opts...)
}

// SetAttributes adds attributes to a span.
func SetAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	if span == nil {
		return
	}
	span.SetAttributes(attrs...)
}

// SetHTTPAttributes adds HTTP-specific attributes to a span.
func SetHTTPAttributes(span trace.Span, method, path, route string) {
	if span == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("http.method", method),
		attribute.String("http.route", route),
		attribute.String("http.target", path),
	}
	span.SetAttributes(attrs...)
}

// GetSpanFromContext extracts the span from a context.
func GetSpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}
