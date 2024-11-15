# Rueidis Cache

Rueidis Cache is a library for caching/persisting arbitrary data in Redis. Rueidis Cache is built on [Rueidis](https://github.com/redis/rueidis) and is an abstraction layer over that library that provides serialization, compression, along with tracing and metrics via OpenTelemetry. Rueidis Cache is focused on the developer experience making it easy to store and retrieve any arbitrary data structure. Under the hood Rueidis Cache stores everything as the [string data type](https://redis.io/docs/latest/develop/data-types/#strings) in Redis by serializing to represent that data as bytes. By default, msgpack is used to serialize and deserialize data without compression but that behavior can be customized by providing `Option` when initializing.

Rueidis Cache is very similar to another library I maintain [redis-cache](https://github.com/jkratz55/redis-cache). That library uses [go-redis](https://github.com/redis/go-redis) under the hood but provides near identical features. The main motivating factor for creating Rueidis Cache was Rueidis slight performance edge, and it's built in support for service-assisted client-side caching.

## Features

* Cache/Persist any data structure that can be represented as bytes
* Built-in serialization with msgpack but can be swapped out for JSON, Protobuf, etc. or even a custom representation.
* Built-in support for compression with built-in support for lz4, gzip, and brotli
* Instrumentation/Metrics via OpenTelemetry
* Support for read-through and write-through cache via helper functions like `Cacheable`

## Requirements

* Go 1.22
* Redis 7+
* Only compatible with Rueidis Redis client

_This library may work on earlier versions of Redis, but I did my testing with Redis 7.4_

## Getting Redis Cache

```shell
go get github.com/jkratz55/rueidis-cache
```

## Usage

Since Rueidis Cache is a wrapper and abstraction of Rueidis it requires a valid reference to a `rueidis.Client` prior to initializing and using the `Cache` type.

```go
func main() {

    // Initialize rueidis redis client
    client, err := rueidis.NewClient(rueidis.ClientOption{
        InitAddress: []string{"localhost:6379"},
    })
    if err != nil {
        panic(err)
    }
    defer client.Close()
    
    // Ping Redis to ensure its reachable
    func () {
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer cancel()
    
        err := client.Do(ctx, client.B().Ping().Build()).Error()
        if err != nil {
            panic(err)
        }
    }()

    // Initialize the Cache. This will use the default configuration but optionally
    // the serialization and compression can be swapped out and features like NearCaching
    // can be enabled.
    rdb := cache.New(client)
	
    // todo: remaining code goes here ...
}
```

Note that the `rueidis.ClientOption` type provides many configuration options. The above example is using the defaults but the client can be tuned and configured for specific workloads and requirements. Please refer to the rueidis documentation for further details configuring the client.

Expanding on the example above here is a very simple use case of setting, getting, and deleting entries.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/rueidis"

	cache "github.com/jkratz55/rueidis-cache"
)

type Person struct {
	FirstName string
	LastName  string
	Age       int
}

func main() {

	// Initialize a logger. You can use any logger you wish but in this example
	// we are sticking to the standard library using slog.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
	}))

	// Initialize rueidis redis client
	client, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{"localhost:6379"},
	})
	if err != nil {
		logger.Error("error creating redis client", slog.String("err", err.Error()))
		panic(err)
	}
	defer client.Close()

	// Ping Redis to ensure its reachable
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := client.Do(ctx, client.B().Ping().Build()).Error()
		if err != nil {
			logger.Error("error pinging redis", slog.String("err", err.Error()))
			panic(err)
		}
	}()

	// Initialize the Cache. This will use the default configuration but optionally
	// the serialization and compression can be swapped out and features like NearCaching
	// can be enabled.
	rdb := cache.New(client)

	if err := rdb.Set(context.Background(), "person", Person{
		FirstName: "Biily",
		LastName:  "Bob",
		Age:       45,
	}, 0); err != nil {
		panic("ohhhhh snap!")
	}

	var p Person
	if err := rdb.Get(context.Background(), "person", &p); err != nil {
		panic("ohhhhh snap")
	}
	fmt.Printf("%v\n", p)

	if err := rdb.Delete(context.Background(), "person"); err != nil {
		panic("ohhh snap!")
	}

	if err := rdb.Get(context.Background(), "person", &p); !errors.Is(err, cache.ErrKeyNotFound) {
		panic("ohhhhh snap, this key should be gone!")
	}
}
```

This library also supports MGET but because of limitations in GO's implementation of generics MGet is a function instead of a method on the Cache type. The MGet function accepts the Cache type as an argument to leverage the same marshaller and unmarshaller. In some rare use cases where thousands of keys are being fetched you may need to enable batching to break up into multiple MGET calls to redis to prevent latency issues with Redis due to Redis single threaded nature and MGET being O(n) time complexity.

```go
rdb := cache.New(client, BatchMultiGets(500)) // Break keys up into multiple MGET commands each with no more than 500 keys
```

This library also supports atomic updates of existing keys by using the Upsert function. If the key was modified while the upsert is in progress it will return RetryableError signaling the operation can be retried and the UpsertCallback can decide how to handle merging the changes.

As mentioned previously there are several options that can be passed into the `New` function when initializing the `Cache`. These options can change the serialization, enable compression with specific `Codec` or enable server assisted client side caching.

### Serialization

By default, msgpack is used to serialize and deserialize data stored in Redis. However, you can serialize the data with encoding you choose as long as it adheres to the `Marshaller` and `Unmarshaller` types.

```go
// Marshaller is a function type that marshals the value of a cache entry for
// storage.
type Marshaller func(v any) ([]byte, error)

// Unmarshaller is a function type that unmarshalls the value retrieved from the
// cache into the target type.
type Unmarshaller func(b []byte, v any) error
```

This allows data to be stored using msgpack, JSON, Protobuf, or a custom binary representation. Since JSON is a popular choice the library provide a convenient `Option` already called `JSON`

```go
rdb := cache.New(client, cache.JSON()) // Configures the cache to use JSON for serialization
```

If you want to use a different encoding for serialization you can use the `Serialization` option. In the example below we are still using JSON, but you could use protobuf or whatever you wish.

```go
marshaller := func(v any) ([]byte, error) {
    return json.Marshal(v)
}
unmarshaller := func(data []byte, v any) error {
    return json.Unmarshal(data, v)
rdb := cache.New(client, cache.Serialization(marshaller, unmarshaller)) // Configures the cache to use our custom serialization
}
```

### Compression

In some cases compressing values stored in Redis can have tremendous benefits, particularly when storing large volumes of data, large values per key, or both. Compression reduces the size of the cache, significantly decreases bandwidth and latency but at the cost of additional CPU consumption on the application/client.

By default, compression is not enabled. However, compression can be enabled through the `Serialization` `Option` when initializing the `Cache`. `Serialization` accepts a `Codec` which enables bringing or implementing your own compression. For developer convenience there are several implementations available out of the box including gzip, flate, lz4 and brotli.

The following example uses lz4.

```go
rdb := cache.New(client, cache.JSON(), cache.LZ4()) // cache.JSON is here to demonstrate multiple Options call be passed
```

### Server Assisted Client Caching

Rueidis supports server assisted client side caching which utilizing a feature in Redis where it notifies the client if a key it's interesting in has be updated and invalidates the local cache. Rueidis cache supports this feature as well, but it is not enabled by default. To enable it, an `Option` needs to be passed to `New` when initializing the `Cache`.

```go
rdb := cache.New(client, cache.NearCache(time.Minute * 10)) // This will keep entry in client side cache for no longer than 10 minutes but it can be evicted sooner if Redis notifies the client a key has changed.
```

## Instrumentation & Tracing

Rueidis Cache and Rueidis supports metrics and tracing using OpenTelemetry. However, there are a couple to be aware of:

* Because the way `rueidis` handles `DoMulti` and `DoMultiCache` instrumenting errors and client side cache hits is not tracked for performance reasons. This is due to the API of the `Hook` from the `rueidishook` package. In order to capture that level of detail we'd need to iterate over the results in the hot path and check for errors and cache hits. Since performance is a major goal of this library we only capture the overall execution time.

The following example demonstrates how to set up OpenTelemetry for tracing and metrics, exposing those metrics via Prometheus. 

```go
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

	cache "github.com/jkratz55/rueidis-cache"
	"github.com/jkratz55/rueidis-cache/cacheotel"

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
		InitAddress: []string{"localhost:6379"},
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

```