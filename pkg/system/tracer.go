package system

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	_ "github.com/filecoin-project/bacalhau/pkg/logger"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials"
)

type cleanupFn func() error

// CleanupTracer should be called at the end of a node's execution to send all
// remaining traces to the exporter before the process ends.
var CleanupTracer cleanupFn

func init() { // nolint:gochecknoinits // use of init here is idomatic
	tp, cleanup, err := hcProvider()
	if err != nil {
		// don't error here because for CLI users they get a red message
		log.Debug().Msgf("error initializing http tracer: %v", err)
		log.Debug().Msg("failed to initialize http tracer, falling back to debug tracer")

		tp, cleanup, err = loggerProvider()
		if err != nil {
			log.Error().Msgf("error initializing debug tracer: %v", err)
			log.Warn().Msg("failed to initialize debug tracer, will proceed without trace instrumentation")
			return // not fatal
		}
	}

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	CleanupTracer = cleanup
}

// Span creates and starts a new span, and a context containing it.
// For more information see the otel.Tracer.Start(...) docs:
//   https://pkg.go.dev/go.opentelemetry.io/otel/trace#Tracer
func Span(ctx context.Context, svcName, spanName string,
	opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	svc := fmt.Sprintf("bacalhau.org/%s", svcName)
	spn := fmt.Sprintf("%s/%s", svcName, spanName)

	// Always include environment info in spans:
	opts = append(opts, trace.WithAttributes(
		attribute.String("environment", GetEnvironment().String()),
	))

	return tracer(svc).Start(ctx, spn, opts...)
}

func tracer(svcName string) trace.Tracer {
	return otel.GetTracerProvider().Tracer(svcName)
}

// loggerProvider provides traces that are exported to a trace logger as JSON.
func loggerProvider() (*sdktrace.TracerProvider, cleanupFn, error) {
	exp, err := stdouttrace.New(
		stdouttrace.WithPrettyPrint(),
		stdouttrace.WithWriter(jsonLogger()))
	if err != nil {
		return nil, nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp))

	return tp, cleanupFor("logger", tp, exp), nil
}

// httpProvider provides traces that are exported over GRPC to Honeycomb. It
// should be configured by setting the following environment variable:
//   export HONEYCOMB_KEY="<honeycomb api key>"
func hcProvider() (*sdktrace.TracerProvider, cleanupFn, error) {
	if os.Getenv("HONEYCOMB_KEY") == "" {
		return nil, nil, fmt.Errorf(
			"error creating honeycomb exporter: please ensure that \"HONEYCOMB_KEY\" has been set")
	}

	exp, err := hcExporter()
	if err != nil {
		return nil, nil, fmt.Errorf("error creating honeycomb exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp), // TODO: use WithBatcher in prod
		sdktrace.WithResource(
			resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String("bacalhau"),
			),
		),
	)

	return tp, cleanupFor("honeycomb", tp, exp), nil
}

// hcExporter returns a SpanExporter configured for Honeycomb.
func hcExporter() (*otlptrace.Exporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint("api.honeycomb.io:443"),
		otlptracegrpc.WithTLSCredentials(
			credentials.NewClientTLSFromCert(nil, "")),
		otlptracegrpc.WithHeaders(map[string]string{
			"x-honeycomb-team":    os.Getenv("HONEYCOMB_KEY"),
			"x-honeycomb-dataset": "bacalhau",
		}),
	}

	return otlptrace.New(context.Background(),
		otlptracegrpc.NewClient(opts...))
}

// jsonLogger returns a writer than trace logs all JSON objects thrown at it.
func jsonLogger() io.Writer {
	r, w := io.Pipe()
	go func(r io.Reader) {
		d := json.NewDecoder(r)

		for {
			var data map[string]interface{}
			if err := d.Decode(&data); err != nil {
				if err == io.EOF {
					return
				}

				log.Trace().Msgf("error parsing json span: %v", err)
				continue
			}

			bs, err := json.Marshal(data)
			if err != nil {
				log.Trace().Msgf("error marshaling json span: %v", err)
				continue
			}

			log.Trace().Msg(string(bs))
		}
	}(r)

	return w
}

// cleanupFor returns a cleanup function that flushes remaining spans in
// memory to the exporter and releases any tracing resources.
// TODO: #288 Use trace to close out span
// nolintunparam // will add tracing
func cleanupFor(name string, tp *sdktrace.TracerProvider, exp sdktrace.SpanExporter) cleanupFn {
	return func() error {
		if err := tp.Shutdown(context.Background()); err != nil {
			return fmt.Errorf(
				"error shutting down %s provider: %w", name, err)
		}

		return nil
	}
}
