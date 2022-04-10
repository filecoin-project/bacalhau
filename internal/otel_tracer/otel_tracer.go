package otel_tracer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/rs/zerolog/log"

	_ "github.com/filecoin-project/bacalhau/internal/logger"

	"github.com/spf13/viper"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"google.golang.org/grpc/credentials"

	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
)

func InitializeOtel(ctxWithId context.Context) (*sdktrace.TracerProvider, func()) {
	// This is the default, pass along os.Stdout as the writer
	return InitializeOtelWithWriter(ctxWithId, os.Stdout)
}

func InitializeOtelWithWriter(ctxWithId context.Context, w io.Writer) (*sdktrace.TracerProvider, func()) {

	// Read in config from $HOME/.bacalhau
	// which contains the HONEYCOMB key
	viper.AddConfigPath("$HOME/.bacalhau")
	viper.SetConfigFile(".env")
	viper.ReadInConfig()

	var tp *sdktrace.TracerProvider
	var cleanupFunc func()

	if (os.Getenv("HONEYCOMB_API_KEY") == "") || (os.Getenv("OTEL_STDOUT") != "") {
		tp, cleanupFunc = initStdOutTracer(ctxWithId, w)
	} else {
		tp, cleanupFunc = initHCTracer(ctxWithId)

	}
	// Create a new tracer provider with a batch span processor and the otlp exporter.

	// Start the Tracer Provider and the W3C Trace Context propagator as globals
	otel.SetTracerProvider(tp)

	// Register the trace context and baggage propagators so data is propagated across services/processes.
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return tp, cleanupFunc
}

func newExporter(ctx context.Context) (*otlptrace.Exporter, error) {
	// Configuration to export data to Honeycomb:
	//
	// 1. The Honeycomb endpoint
	// 2. Your API key, set as the x-honeycomb-team header
	hcKey := viper.GetString("HONEYCOMB_API_KEY")
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint("api.honeycomb.io:443"),
		otlptracegrpc.WithHeaders(map[string]string{
			"x-honeycomb-team":    hcKey,
			"x-honeycomb-dataset": fmt.Sprint(ctx.Value("Bacalhau-Tracing")),
		}),
		otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")),
	}

	client := otlptracegrpc.NewClient(opts...)
	return otlptrace.New(ctx, client)
}

func newTraceProvider(exp *otlptrace.Exporter) *sdktrace.TracerProvider {
	// The service.name attribute is required.
	//
	//
	// Your service name will be used as the Service Dataset in honeycomb, which is where data is stored.
	resource :=
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("Bacalhau-Execution"),
		)

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource),
	)
}

// Implement an HTTP Handler func to be instrumented
func httpHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, World")
}

// Wrap the HTTP handler func with OTel HTTP instrumentation
func wrapHandler() {
	handler := http.HandlerFunc(httpHandler)
	wrappedHandler := otelhttp.NewHandler(handler, "hello")
	http.Handle("/hello", wrappedHandler)
}

func initStdOutTracer(ctx context.Context, w io.Writer) (*sdktrace.TracerProvider, func()) {
	var err error
	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint(), stdouttrace.WithWriter(w)) // for debugging
	if err != nil {
		log.Panic().Msgf("failed to initialize stdouttrace exporter %v\n", err)
		return nil, nil
	}

	ssp := sdktrace.NewSimpleSpanProcessor(exp)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(ssp),
	)
	otel.SetTracerProvider(tp)

	return tp, func() {
		if err := tp.Shutdown(ctx); err != nil {
			log.Fatal().Msgf("stopping tracer provider: %v", err)
		}
	}
}

func initHCTracer(ctx context.Context) (*sdktrace.TracerProvider, func()) {

	// Configure a new exporter using environment variables for sending data to Honeycomb over gRPC.
	exp, err := newExporter(ctx)
	if err != nil {
		log.Fatal().Msgf("failed to initialize exporter: %v", err)
	}
	tp := newTraceProvider(exp)

	return tp, func() {
		if err := tp.Shutdown(ctx); err != nil {
			log.Fatal().Msgf("stopping tracer provider: %v", err)
		}
	}

}
