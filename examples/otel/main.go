package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/rueidis"
	"github.com/redis/rueidis/rueidisotel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	cache "github.com/jkratz55/redis-cache/v2"
	"github.com/jkratz55/redis-cache/v2/cacheotel"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
)

func main() {

	var (
		enableNearCache = flag.Bool("near-cache", false, "Enables Rueidis Near Cache/Client Cache feature")
	)
	flag.Parse()

	// Initialize a logger. You can use any logger you wish but in this example
	// we are sticking to the standard library using slog.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
	}))

	// Setup OpenTelemetry trace exporter
	traceExporter, err := otlptracehttp.New(context.Background())
	if err != nil {
		logger.Error("error creating trace exporter", slog.String("err", err.Error()))
		panic(err)
	}

	// Configure OpenTelemetry resource and TracerProvider
	otelResource := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String("cacheotel-example"),
		semconv.ServiceVersionKey.String("1.0.0"))
	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(otelResource))
	defer func() {
		err := traceProvider.Shutdown(context.Background())
		if err != nil {
			logger.Error("error shutting down trace provider", slog.String("err", err.Error()))
		}
	}()

	// Set the TraceProvider and TextMapPropagator globally
	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Setup OpenTelemetry metric exporter
	exporter, err := prometheus.New()
	if err != nil {
		logger.Error("error creating prometheus exporter", slog.String("err", err.Error()))
		panic(err)
	}
	provider := metric.NewMeterProvider(metric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	// Initialize the Rueidis Redis client with OpenTelemetry tracing
	// Note you can customize the client parameters here as needed
	redisClient, err := rueidisotel.NewClient(rueidis.ClientOption{
		InitAddress:       []string{"localhost:6379"},
		ForceSingleClient: true,
	},
		rueidisotel.WithMeterProvider(provider),
		rueidisotel.WithTracerProvider(traceProvider),
		rueidisotel.WithDBStatement(func(cmdTokens []string) string {
			// This is an example displaying logging the command sent to Redis.
			// In some cases this may not be optimal because the values are
			// compressed or in an unreadable format to humans, or perhaps its
			// just too much data.
			var builder strings.Builder
			for _, token := range cmdTokens {
				if utf8.ValidString(token) {
					builder.WriteString(token + " ")
				} else {
					// Because this example is using lz4 compression the resulting
					// command contains invalid utf8 strings which makes OTEL unhappy
					// so we need to do some formatting to ensure all the strings are
					// valid
					builder.WriteString(fmt.Sprintf("0x%x ", token))
				}
			}
			return builder.String()
		}),
	)
	if err != nil {
		logger.Error("error creating redis client", slog.String("err", err.Error()))
		panic(err)
	}
	defer redisClient.Close()

	// Ping Redis to ensure its reachable
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := redisClient.Do(ctx, redisClient.B().Ping().Build()).Error()
		if err != nil {
			logger.Error("error pinging redis", slog.String("err", err.Error()))
			panic(err)
		}
	}()

	// Enable instrumentation for the Redis client
	redisClient, err = cacheotel.InstrumentClient(redisClient,
		cacheotel.WithMeterProvider(provider),
		cacheotel.WithExplicitBucketBoundaries(cacheotel.ExponentialBuckets(0.001, 2, 6)))

	// Configure the Cache options to use compression and JSON serialization.
	// Typically, it is recommended to use the default encoding which is msgpack
	// but for this example, we will use JSON as it makes viewing the data in
	// Redis easier.
	opts := []cache.Option{
		cache.LZ4(),
		cache.JSON(),
	}
	if *enableNearCache {
		opts = append(opts, cache.NearCache(time.Minute*10))
	}

	// Initialize the Cache and enable instrumentation of the cache
	rdb := cache.New(redisClient, opts...)
	err = cacheotel.InstrumentMetrics(rdb, cacheotel.WithMeterProvider(provider),
		cacheotel.WithExplicitBucketBoundaries(cacheotel.ExponentialBuckets(0.001, 2, 6)))

	// Example HTTP handlers to demonstrate using the cache
	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/get", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")

		var res string
		err := rdb.Get(r.Context(), key, &res)
		if errors.Is(err, cache.ErrKeyNotFound) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(res))
	}))
	http.Handle("/set", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		value := r.URL.Query().Get("value")

		err := rdb.Set(r.Context(), key, value, time.Minute*10)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))

	_ = http.ListenAndServe(":8080", nil)
}
