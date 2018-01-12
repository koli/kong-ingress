package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/util/workqueue"
)

var (
	apisTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "kong_ingress",
		Subsystem: "controller",
		Name:      "total_kong_apis",
		Help:      "Total number of Kong apis",
	})

	apisFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "kong_ingress",
		Subsystem: "controller",
		Name:      "apis_failed",
		Help:      "Total number of requests that failed on creating a new api on Kong",
	})
)

func init() {
	prometheus.MustRegister(apisTotal)
	prometheus.MustRegister(apisFailed)
}

type prometheusMetricsProvider struct{}

func (_ prometheusMetricsProvider) NewDepthMetric(name string) workqueue.GaugeMetric {
	depth := prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem: name,
		Name:      "depth",
		Help:      "Current depth of workqueue: " + name,
	})
	prometheus.Register(depth)
	return depth
}

func (_ prometheusMetricsProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	adds := prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: name,
		Name:      "adds",
		Help:      "Total number of adds handled by workqueue: " + name,
	})
	prometheus.Register(adds)
	return adds
}

func (_ prometheusMetricsProvider) NewLatencyMetric(name string) workqueue.SummaryMetric {
	latency := prometheus.NewSummary(prometheus.SummaryOpts{
		Subsystem: name,
		Name:      "queue_latency",
		Help:      "How long an item stays in workqueue" + name + " before being requested.",
	})
	prometheus.Register(latency)
	return latency
}

func (_ prometheusMetricsProvider) NewWorkDurationMetric(name string) workqueue.SummaryMetric {
	workDuration := prometheus.NewSummary(prometheus.SummaryOpts{
		Subsystem: name,
		Name:      "work_duration",
		Help:      "How long processing an item from workqueue" + name + " takes.",
	})
	prometheus.Register(workDuration)
	return workDuration
}

func (_ prometheusMetricsProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	retries := prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: name,
		Name:      "retries",
		Help:      "Total number of retries handled by workqueue: " + name,
	})
	prometheus.Register(retries)
	return retries
}
