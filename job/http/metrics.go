package http

import (
	"time"

	"github.com/ReneKroon/ttlcache/v2"
	"github.com/VictoriaMetrics/metrics"
)

var (
	mRequestsOut = metrics.NewCounter("http_requests")
	m2xx         = metrics.NewCounter(`http_status{code="2xx"}`)
	m3xx         = metrics.NewCounter(`http_status{code="3xx"}`)
	m4xx         = metrics.NewCounter(`http_status{code="4xx"}`)
	m5xx         = metrics.NewCounter(`http_status{code="5xx"}`)
	mTimeouts    = metrics.NewCounter(`http_timeouts`)
	mErrors      = metrics.NewCounter(`http_error`)
)

type byHostMetric struct {
	name  string
	cache *ttlcache.Cache
}

func newByHostMetric(name string) *byHostMetric {
	m := &byHostMetric{
		name:  name,
		cache: ttlcache.NewCache(),
	}
	m.cache.SkipTTLExtensionOnHit(false)
	_ = m.cache.SetTTL(time.Hour * 12)
	m.cache.SetLoaderFunction(func(host string) (data interface{}, ttl time.Duration, err error) {
		return metrics.NewCounter(m.getMetricName(host)), 0, nil
	})
	m.cache.SetExpirationCallback(func(host string, counter interface{}) {
		metrics.UnregisterMetric(m.getMetricName(host))
	})
	return m
}

func (m *byHostMetric) getMetricName(host string) string {
	return m.name + `{host="` + host + `"}`
}

func (m *byHostMetric) inc(host string) {
	val, err := m.cache.Get(host)
	if err == ttlcache.ErrNotFound {
		return
	}
	counter := val.(*metrics.Counter)
	counter.Inc()
}
