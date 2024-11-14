package cacheotel

import (
	"context"
	"time"

	"github.com/redis/rueidis"
	"github.com/redis/rueidis/rueidishook"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	cache "github.com/jkratz55/redis-cache/v2"
)

func InstrumentClient(c rueidis.Client, opts ...Option) (rueidis.Client, error) {
	baseOpts := make([]baseOption, len(opts))
	for i, opt := range opts {
		baseOpts[i] = opt
	}
	conf := newConfig(baseOpts...)

	if conf.meter == nil {
		conf.meter = conf.meterProvider.Meter(
			name,
			metric.WithInstrumentationVersion("semver"+cache.Version()))
	}

	hook, err := newInstrumentingHook(conf)
	if err != nil {
		return nil, err
	}
	return rueidishook.WithHook(c, hook), nil
}

type instrumentingHook struct {
	attrs           []attribute.KeyValue
	cmdDuration     metric.Float64Histogram
	cmdErrors       metric.Int64Counter
	clientCacheHits metric.Int64Counter
}

func newInstrumentingHook(conf *config) (*instrumentingHook, error) {
	cmdDuration, err := conf.meter.Float64Histogram("rueidis.command.duration_seconds",
		metric.WithDescription("Duration of time in seconds to execute a command"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(conf.buckets...))
	if err != nil {
		return nil, err
	}

	cmdErrors, err := conf.meter.Int64Counter("rueidis.command.errors_total",
		metric.WithDescription("Count of errors during command execution"),
		metric.WithUnit("count"))
	if err != nil {
		return nil, err
	}

	clientCacheHits, err := conf.meter.Int64Counter("rueidis.command.client_cache_hits",
		metric.WithDescription("Count of commands that had a cache hit on client cache"),
		metric.WithUnit("count"))
	if err != nil {
		return nil, err
	}

	return &instrumentingHook{
		attrs:           conf.attrs,
		cmdDuration:     cmdDuration,
		cmdErrors:       cmdErrors,
		clientCacheHits: clientCacheHits,
	}, nil
}

func (i *instrumentingHook) Do(client rueidis.Client, ctx context.Context, cmd rueidis.Completed) (resp rueidis.RedisResult) {
	cmdName := cmd.Commands()[0]

	start := time.Now()
	resp = client.Do(ctx, cmd)
	dur := time.Since(start)

	attrs := make([]attribute.KeyValue, len(i.attrs)+1)
	copy(attrs, i.attrs)
	attrs[len(attrs)-1] = attribute.String("command", cmdName)
	i.cmdDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(attrs...))

	if resp.Error() != nil {
		i.cmdErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	return resp
}

func (i *instrumentingHook) DoMulti(client rueidis.Client, ctx context.Context, multi ...rueidis.Completed) (resps []rueidis.RedisResult) {
	cmds := make([]string, len(multi))
	for i := 0; i < len(multi); i++ {
		cmds[i] = multi[i].Commands()[0]
	}

	start := time.Now()
	resps = client.DoMulti(ctx, multi...)
	dur := time.Since(start)

	attrs := make([]attribute.KeyValue, len(i.attrs)+1)
	copy(attrs, attrs)
	attrs[len(attrs)-1] = attribute.String("command", "pipeline")
	i.cmdDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(attrs...))

	return resps
}

func (i *instrumentingHook) DoCache(client rueidis.Client, ctx context.Context, cmd rueidis.Cacheable, ttl time.Duration) (resp rueidis.RedisResult) {
	cmdName := cmd.Commands()[0]

	start := time.Now()
	resp = client.DoCache(ctx, cmd, ttl)
	dur := time.Since(start)

	attrs := make([]attribute.KeyValue, len(i.attrs)+1)
	copy(attrs, i.attrs)
	attrs[len(attrs)-1] = attribute.String("command", cmdName)
	i.cmdDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(attrs...))

	if resp.Error() != nil {
		i.cmdErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if resp.IsCacheHit() {
		i.clientCacheHits.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	return resp
}

func (i *instrumentingHook) DoMultiCache(client rueidis.Client, ctx context.Context, multi ...rueidis.CacheableTTL) (resps []rueidis.RedisResult) {
	cmds := make([]string, len(multi))
	for i := 0; i < len(multi); i++ {
		cmds[i] = multi[i].Cmd.Commands()[0]
	}

	start := time.Now()
	resps = client.DoMultiCache(ctx, multi...)
	dur := time.Since(start)

	attrs := make([]attribute.KeyValue, len(i.attrs)+1)
	copy(attrs, i.attrs)
	attrs[len(attrs)-1] = attribute.String("command", "pipeline")
	i.cmdDuration.Record(ctx, dur.Seconds(), metric.WithAttributes(attrs...))

	return resps
}

func (i *instrumentingHook) Receive(client rueidis.Client, ctx context.Context, subscribe rueidis.Completed, fn func(msg rueidis.PubSubMessage)) (err error) {
	return client.Receive(ctx, subscribe, fn)
}

func (i *instrumentingHook) DoStream(client rueidis.Client, ctx context.Context, cmd rueidis.Completed) rueidis.RedisResultStream {
	return client.DoStream(ctx, cmd)
}

func (i *instrumentingHook) DoMultiStream(client rueidis.Client, ctx context.Context, multi ...rueidis.Completed) rueidis.MultiRedisResultStream {
	return client.DoMultiStream(ctx, multi...)
}
