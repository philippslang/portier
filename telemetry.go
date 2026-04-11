package portier

import (
	"context"
	"fmt"
	"log"

	"github.com/mark3labs/mcp-go/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "mcp-api-gateway"

// ToolHandlerFunc is the mcp-go handler signature.
type ToolHandlerFunc = func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)

// initTracer sets up the OTel trace provider with OTLP gRPC export.
// Returns a shutdown function that must be called on exit.
func initTracer(ctx context.Context, cfg TelemetryConfig, serverName string) (func(context.Context) error, error) {
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(), // use WithTLSCredentials in production
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP exporter: %w", err)
	}

	sampler := sdktrace.AlwaysSample()
	if cfg.SampleRatio < 1.0 {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRatio)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serverName),
		)),
	)

	otel.SetTracerProvider(tp)
	log.Printf("OpenTelemetry tracing enabled → %s (sample_ratio=%.2f)", cfg.Endpoint, cfg.SampleRatio)
	return tp.Shutdown, nil
}

// withTracing wraps a tool handler with an OTel span.
// Adds tool name, arguments, and error status as span attributes/events.
func withTracing(toolName string, handler ToolHandlerFunc) ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tracer := otel.Tracer(tracerName)
		ctx, span := tracer.Start(ctx, "tool."+toolName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("mcp.tool", toolName),
			),
		)
		defer span.End()

		args := request.GetArguments()
		if s, ok := args["service"].(string); ok {
			span.SetAttributes(attribute.String("mcp.service", s))
		}
		if op, ok := args["operationId"].(string); ok {
			span.SetAttributes(attribute.String("mcp.operation_id", op))
		}
		if confirmed, ok := args["confirmed"].(bool); ok {
			span.SetAttributes(attribute.Bool("mcp.confirmed", confirmed))
		}

		result, err := handler(ctx, request)

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else if result != nil && result.IsError {
			span.SetStatus(codes.Error, "tool returned error")
			span.AddEvent("tool.error")
		} else {
			span.SetStatus(codes.Ok, "")
		}

		return result, err
	}
}
