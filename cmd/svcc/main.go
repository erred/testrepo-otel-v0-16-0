package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpgrpc"
	"go.opentelemetry.io/otel/label"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func installOtlpPipeline(ctx context.Context) (func(), error) {
	exporter, err := otlp.NewExporter(ctx, otlpgrpc.NewDriver(
		otlpgrpc.WithInsecure(),
		otlpgrpc.WithEndpoint("otel-collector.otel.svc.cluster.local:55680"),
	))
	if err != nil {
		return nil, fmt.Errorf("otlp setup: create exporter: %w", err)
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(
			// the service name used to display traces in backends
			semconv.ServiceNameKey.String("service-c"),
			label.String("app", "svcc"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otlp setup: create resource: %w", err)
	}

	traceProvider := sdktrace.NewTracerProvider(sdktrace.WithConfig(
		sdktrace.Config{
			DefaultSampler: sdktrace.AlwaysSample(),
		},
	), sdktrace.WithResource(
		res,
	), sdktrace.WithSpanProcessor(
		sdktrace.NewSimpleSpanProcessor(exporter),
	))
	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return func() {
		ctx := context.TODO()
		err := traceProvider.Shutdown(ctx)
		if err != nil {
			otel.Handle(err)
		}
		err = exporter.Shutdown(ctx)
		if err != nil {
			otel.Handle(err)
		}
	}, nil
}

func main() {
	ctx := context.Background()

	shutdown, err := installOtlpPipeline(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer shutdown()

	tracer := otel.Tracer("svcc")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, span := tracer.Start(r.Context(), "pong")
		defer span.End()

		n, err := w.Write([]byte("pog"))
		if err != nil {
			log.Printf("traceid=%s err=%q", span.SpanContext().TraceID, err.Error())
			return
		}
		log.Printf("traceid=%s bytes=%d", span.SpanContext().TraceID, n)
	})

	http.ListenAndServe(":8080", otelhttp.NewHandler(http.DefaultServeMux, "inject"))
}
