package observability

import (
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	adktelemetry "google.golang.org/adk/telemetry"
)

// ADKTelemetryOptions returns launcher telemetry options that force ADK to reuse
// the current global SDK TracerProvider initialized by veadk-go observability.
//
// This prevents launcher telemetry initialization from replacing the global
// provider with a different instance and keeps ADK spans and veadk plugin spans
// in the same pipeline.
func ADKTelemetryOptions() []adktelemetry.Option {
	tc := otel.GetTracerProvider()
	sdkTP, ok := tc.(*sdktrace.TracerProvider)
	if !ok || sdkTP == nil {
		return nil
	}

	return []adktelemetry.Option{adktelemetry.WithTracerProvider(sdkTP)}
}
