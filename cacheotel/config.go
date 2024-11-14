package cacheotel

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type config struct {
	dbSystem      string
	attrs         []attribute.KeyValue
	meterProvider metric.MeterProvider
	meter         metric.Meter
	poolName      string
	buckets       []float64
}

func newConfig(opts ...baseOption) *config {
	conf := &config{
		dbSystem:      "redis",
		attrs:         []attribute.KeyValue{},
		meterProvider: otel.GetMeterProvider(),
		buckets:       ExponentialBuckets(0.001, 2, 10), // 1ms, 2ms, 4ms, 8ms, 16ms, 32ms, 64ms, 128ms, 256ms, 512ms
	}

	for _, opt := range opts {
		opt.apply(conf)
	}

	conf.attrs = append(conf.attrs, attribute.String("db.system", conf.dbSystem))
	return conf
}

type baseOption interface {
	apply(cong *config)
}

type Option interface {
	baseOption
	metrics()
}

type option func(conf *config)

func (fn option) apply(conf *config) {
	fn(conf)
}

func (fn option) metrics() {}

func WithAtributes(atts ...attribute.KeyValue) Option {
	return option(func(conf *config) {
		conf.attrs = atts
	})
}

func WithDBSystem(system string) Option {
	return option(func(conf *config) {
		conf.dbSystem = system
	})
}

type MetricsOption interface {
	baseOption
	metrics()
}

type metricOption func(conf *config)

func (m metricOption) apply(conf *config) {
	m(conf)
}

func (m metricOption) metrics() {}

var _ MetricsOption = (*metricOption)(nil)

func WithMeterProvider(mp metric.MeterProvider) MetricsOption {
	return metricOption(func(conf *config) {
		conf.meterProvider = mp
	})
}

func WithExplicitBucketBoundaries(boundaries []float64) MetricsOption {
	return metricOption(func(conf *config) {
		conf.buckets = boundaries
	})
}
