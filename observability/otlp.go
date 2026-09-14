// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package observability

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/volcengine/veadk-go/configs"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/net/http/httpguts"
)

// ErrOTLPExport is deliberately free of endpoints, headers and collector responses.
var ErrOTLPExport = errors.New("OTLP trace export failed")

func sdkDisabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true")
}

// traceExportSelection keeps legacy explicit exporters when no standard selector
// is present. With a selector, only the named standard exporters are used.
func traceExportSelection(cfg *configs.OpenTelemetryConfig) (otlp, console, explicit, disabled bool, err error) {
	if sdkDisabled() {
		return false, false, true, true, nil
	}
	selection := strings.TrimSpace(os.Getenv("OTEL_TRACES_EXPORTER"))
	if selection == "" {
		return cfg != nil && cfg.OTLP != nil || os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "", false, false, false, nil
	}
	for _, item := range strings.Split(selection, ",") {
		switch strings.TrimSpace(item) {
		case "otlp":
			otlp = true
		case "console":
			console = true
		case "none":
			disabled = true
		default:
			return false, false, true, false, errors.New("unsupported OTEL_TRACES_EXPORTER (supported: otlp, console, none)")
		}
	}
	if disabled && (otlp || console) {
		return false, false, true, false, errors.New("OTEL_TRACES_EXPORTER none must be used alone")
	}
	return otlp, console, true, disabled, nil
}

func otlpEnv(suffix string) string {
	if v := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_" + suffix); v != "" {
		return v
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_" + suffix)
}

func resolveOTLP(cfg *configs.OTLPConfig) (*configs.OTLPConfig, error) {
	// The underlying SDK parses generic and trace-specific env even when an
	// explicit option overrides them. Validate both before it can log raw input.
	for _, prefix := range []string{"OTEL_EXPORTER_OTLP_", "OTEL_EXPORTER_OTLP_TRACES_"} {
		if raw := os.Getenv(prefix + "ENDPOINT"); raw != "" {
			if _, err := url.Parse(raw); err != nil {
				return nil, errors.New("invalid OTLP endpoint")
			}
		}
		if raw := os.Getenv(prefix + "HEADERS"); raw != "" {
			if _, err := parseOTLPHeaders(raw); err != nil {
				return nil, err
			}
		}
		if raw := os.Getenv(prefix + "TIMEOUT"); raw != "" {
			if _, err := strconv.ParseInt(raw, 10, 64); err != nil {
				return nil, errors.New("invalid OTLP timeout")
			}
		}
	}
	c := cfg.Clone()
	if c == nil {
		c = &configs.OTLPConfig{}
	}
	if v := otlpEnv("PROTOCOL"); v != "" {
		c.Protocol = v
	}
	if c.Protocol == "" {
		c.Protocol = "http/protobuf"
	}
	if c.Protocol != "http/protobuf" && c.Protocol != "grpc" {
		return nil, errors.New("unsupported OTLP trace protocol (supported: http/protobuf, grpc)")
	}
	base := false
	if v := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); v != "" {
		c.Endpoint = v
	} else if v := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); v != "" {
		c.Endpoint, base = v, true
	}
	if c.Endpoint == "" {
		c.Endpoint, base = "http://localhost:4318", true
		if c.Protocol == "grpc" {
			c.Endpoint = "http://localhost:4317"
		}
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("OTLP endpoint must be an HTTP(S) URL without userinfo, query or fragment")
	}
	if c.Protocol == "http/protobuf" && base {
		u.Path = strings.TrimRight(u.Path, "/") + "/v1/traces"
		u.RawPath = ""
	}
	if u.Path == "" {
		u.Path = "/"
	}
	c.Endpoint = u.String()
	if v := otlpEnv("HEADERS"); v != "" {
		c.Headers, err = parseOTLPHeaders(v)
		if err != nil {
			return nil, err
		}
	}
	for key, val := range c.Headers {
		if !httpguts.ValidHeaderFieldName(key) || !httpguts.ValidHeaderFieldValue(val) {
			return nil, errors.New("invalid OTLP headers")
		}
	}
	if v := otlpEnv("TIMEOUT"); v != "" {
		ms, parseErr := strconv.ParseInt(v, 10, 64)
		if parseErr != nil || ms <= 0 || ms > int64((1<<63-1)/time.Millisecond) {
			return nil, errors.New("OTLP timeout must be positive milliseconds")
		}
		c.Timeout = time.Duration(ms) * time.Millisecond
	}
	if c.Timeout < 0 {
		return nil, errors.New("OTLP timeout must be positive")
	}
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
	return c, nil
}

// NewOTLPExporter creates a standard trace exporter without waiting for a
// collector connection. nil uses defaults plus the standard OTEL exporter env.
// SDK disabling and exporter selection are handled by Init/NewMultiExporter.
func NewOTLPExporter(ctx context.Context, cfg *configs.OTLPConfig) (sdktrace.SpanExporter, error) {
	c, err := resolveOTLP(cfg)
	if err != nil {
		return nil, err
	}
	var exp sdktrace.SpanExporter
	if c.Protocol == "grpc" {
		exp, err = otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(c.Endpoint), otlptracegrpc.WithHeaders(maps.Clone(c.Headers)), otlptracegrpc.WithTimeout(c.Timeout))
	} else {
		exp, err = otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(c.Endpoint), otlptracehttp.WithHeaders(maps.Clone(c.Headers)), otlptracehttp.WithTimeout(c.Timeout))
	}
	if err != nil {
		return nil, errors.New("OTLP trace exporter initialization failed")
	}
	return &safeOTLPExporter{SpanExporter: exp}, nil
}

type safeOTLPExporter struct{ sdktrace.SpanExporter }

func (e *safeOTLPExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return safeOTLPError(e.SpanExporter.ExportSpans(ctx, spans))
}
func (e *safeOTLPExporter) Shutdown(ctx context.Context) error {
	return safeOTLPError(e.SpanExporter.Shutdown(ctx))
}
func safeOTLPError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return errors.Join(ErrOTLPExport, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(ErrOTLPExport, context.DeadlineExceeded)
	}
	return ErrOTLPExport
}

func parseOTLPHeaders(raw string) (map[string]string, error) {
	headers := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(pair), "=")
		key = strings.TrimSpace(key)
		decoded, err := url.PathUnescape(strings.TrimSpace(val))
		if !ok || err != nil || !httpguts.ValidHeaderFieldName(key) || !httpguts.ValidHeaderFieldValue(decoded) {
			return nil, errors.New("invalid OTLP headers")
		}
		headers[key] = decoded
	}
	return headers, nil
}
