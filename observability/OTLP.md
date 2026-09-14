# Standard OTLP trace export

VeADK can send its existing Agent/model/tool spans to an OpenTelemetry Collector
using HTTP protobuf or gRPC. It reuses the existing plugin, ADK span translator,
and batch processor. No model credentials are needed to try the self-contained
[example](../examples/observability/otlp/main.go).

```sh
OTEL_TRACES_EXPORTER=otlp \
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 \
OTEL_SERVICE_NAME=my-agent \
go run ./examples/observability/otlp -port 8000

curl -H 'Content-Type: application/json' \
  -d '{"prompt":"hello"}' http://localhost:8000/invoke
```

The example uses an in-process fixture model and VeADK's real HTTP Server and
observability plugin. A Collector receives actual Agent/model spans. Send
SIGTERM to flush queued spans and stop the server.

## Activation and precedence

Configuration is read once, when `observability.Init` or `NewPlugin` first runs.
Set it before starting the Server. Runtime environment changes do not reconfigure
a running provider.

| Configuration | Behavior |
|---|---|
| No exporter config, selector or OTLP endpoint | No export; preserves existing VeADK defaults |
| `OTEL_TRACES_EXPORTER=otlp` | Standard OTLP, defaults to HTTP protobuf and `http://localhost:4318/v1/traces` |
| OTLP endpoint set, selector absent | Enables standard OTLP alongside any explicit legacy exporters |
| `OTEL_TRACES_EXPORTER=console` | Stdout traces |
| `OTEL_TRACES_EXPORTER=otlp,console` | Both standard exporters |
| `OTEL_TRACES_EXPORTER=none` | No trace exporters, including configured legacy exporters; metrics remain independently configured |
| `OTEL_SDK_DISABLED=true` | Skips VeADK trace and metric initialization |

A nonempty selector explicitly selects the trace exporters, replacing the legacy
APMPlus/CozeLoop/TLS/file/stdout selection. Without it, legacy exporters remain
compatible and can coexist with OTLP. `none` must be used alone. Unsupported
selector/protocol values produce a configuration error without echoing the value;
`NewPlugin` logs the error and returns a noop plugin, allowing the Server to run.
Applications calling `Init` directly receive the error. `ErrNoExporters` identifies
disabled/unconfigured operation.

## Transport and resource configuration

For endpoint, protocol, headers and timeout, a nonempty
`OTEL_EXPORTER_OTLP_TRACES_*` variable overrides the corresponding
`OTEL_EXPORTER_OTLP_*` variable; both override programmatic fields. Malformed generic endpoint/header/timeout
syntax is rejected even when trace-specific values override it, because the
underlying SDK also parses the generic variables and would otherwise log raw
invalid input.

| Variable | Meaning |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | HTTP(S) base URL; HTTP appends `/v1/traces` to its path |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | Complete HTTP(S) trace URL, used as-is; an absent path becomes `/` |
| `OTEL_EXPORTER_OTLP[_TRACES]_PROTOCOL` | `http/protobuf` (default) or `grpc`; gRPC default endpoint is `http://localhost:4317` |
| `OTEL_EXPORTER_OTLP[_TRACES]_HEADERS` | Comma-separated `key=value` pairs; percent-encode values, e.g. `authorization=Bearer%20TOKEN`; the trace-specific set replaces the generic set |
| `OTEL_EXPORTER_OTLP[_TRACES]_TIMEOUT` | Positive milliseconds per export, default 10000; caller deadlines can shorten it |
| `OTEL_SERVICE_NAME` | `service.name`, overrides the same key in resource attributes |
| `OTEL_RESOURCE_ATTRIBUTES` | Comma-separated resource attributes, e.g. `deployment.environment.name=test` |
| `OTEL_BSP_*` | Batch queue, batch size, schedule delay and export timeout use the existing OTel Go SDK settings |

HTTP endpoint construction follows the [OTLP specification](https://opentelemetry.io/docs/specs/otel/protocol/exporter/).
For example, a generic `http://collector:4318/prefix/` becomes
`http://collector:4318/prefix/v1/traces`; a trace-specific endpoint with the same
value retains `/prefix/`. Even a generic path already ending in `/v1/traces` gets
the suffix appended: use the trace-specific variable for complete URLs.

Endpoints require an `http` or `https` scheme. Userinfo, query strings and
fragments are rejected; use headers for authentication. `http/json` is not
supported. TLS/certificate and compression options remain those of the underlying
OTel Go exporters; this change does not introduce a separate TLS stack.

Resource attributes are installed when VeADK creates a provider. If a host already
installed an SDK TracerProvider, VeADK registers its existing processors on that
provider and preserves the host's resource and sampler. Configure that provider's
resource in the host application. Disabling VeADK does not disable independently
installed host instrumentation.

Programmatic setup:

```go
err := observability.Init(ctx, &configs.ObservabilityConfig{
    OpenTelemetry: &configs.OpenTelemetryConfig{
        OTLP: &configs.OTLPConfig{
            Endpoint: "http://localhost:4318/v1/traces", // complete trace URL
            Protocol: "http/protobuf",
            Timeout:  3 * time.Second,
        },
    },
})
```

The YAML loader supports `observability.opentelemetry.otlp.endpoint` and
`observability.opentelemetry.otlp.protocol`; configure headers and timeout through
standard OTEL variables or the Go API. `NewOTLPExporter(ctx, cfg)` is also available
for applications assembling their own provider; this low-level constructor does
not apply SDK disabling or exporter selection.

## Failure and lifecycle contract

Exporter initialization does not wait for Collector connectivity. Export runs in
the bounded, nonblocking SDK batch queue: Collector failures do not fail Agent
requests, and queue exhaustion can drop spans. The OTel exporters handle transient
retries within their export timeout. Later batches can recover after a failure.
Export errors are sanitized to `ErrOTLPExport`, optionally retaining context
cancellation/deadline classification, without collector response bodies or headers.

`observability.ForceFlush(ctx)` and `Shutdown(ctx)` respect the earlier of the
caller deadline and five seconds. `apps.Run` already invokes shutdown on SIGTERM
and waits for active HTTP requests before flushing. Delivery is best effort;
spans still queued at the deadline may be lost. Registry shutdown is safe to repeat
concurrently. Configuration and global provider initialization remain once per
process; shutdown is terminal. Applications managing their own providers should
use `RunConfig.DisableObservability` and manage plugin/provider lifecycle explicitly.

This PR covers standard trace export and delivery. Cross-module W3C propagation,
trace topology and content capture policy are tracked separately in PR10. The
existing plugin's prompt/tool content attributes are still present in exported
spans.

## Validation

```sh
go test -race ./observability ./examples/observability/otlp
```

Tests decode real OTLP HTTP/gRPC requests and verify endpoint/header precedence,
resource attributes, explicit disabling, invalid config, error redaction, timeout,
recovery and host provider reuse. Independent binary tests run the actual Server
and Agent in empty working directories with synthetic environment only, covering
batch export, shutdown flush, Collector outage/hang and bounded SIGTERM exit.

A local official Collector 0.135.0 was also verified with both HTTP protobuf and
gRPC receivers and a debug exporter. Use an isolated Collector with:

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 127.0.0.1:4318
      grpc:
        endpoint: 127.0.0.1:4317
exporters:
  debug:
    verbosity: detailed
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [debug]
```

Run the example once for each protocol, invoke it, then send SIGTERM. Verify the
Collector reports the configured `service.name`, resource attributes and model
spans. The example only uses synthetic prompt/model content.
