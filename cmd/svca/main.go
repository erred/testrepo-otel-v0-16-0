package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	_ "net/http/pprof"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpgrpc"
	"go.opentelemetry.io/otel/label"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv"
	"go.opentelemetry.io/otel/trace"

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
			semconv.ServiceNameKey.String("service-a"),
			label.String("app", "svca"),
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

	tracer := otel.Tracer("svca")

	client := &http.Client{
		Transport: otelhttp.NewTransport(
			http.DefaultTransport,
		),
	}

	go http.ListenAndServe(":8080", nil)

	for range time.NewTicker(time.Second).C {
		func() {
			ctx := context.Background()
			ctx, span := tracer.Start(ctx, "ping", trace.WithAttributes(label.String("an", "apple"), label.Int("step", 1)))
			defer span.End()

			u := "http://svcb.default.svc"
			req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
			if err != nil {
				log.Printf("traceid=%s err=%q", span.SpanContext().TraceID, err.Error())
				return
			}
			res, err := client.Do(req)
			if err != nil {
				log.Printf("traceid=%s err=%q", span.SpanContext().TraceID, err.Error())
				return
			} else if res.StatusCode != 200 {
				log.Printf("traceid=%s status=%q", span.SpanContext().TraceID, res.Status)
				return
			}
			defer res.Body.Close()
			b, err := io.ReadAll(res.Body)
			if err != nil {
				log.Printf("traceid=%s err=%q", span.SpanContext().TraceID, err.Error())
			}
			log.Printf("traceid=%s msg=%s", span.SpanContext().TraceID, string(b))
		}()
	}
}
