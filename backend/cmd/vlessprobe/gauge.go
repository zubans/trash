package main

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// gaugeValue and gaugeScalar read a gauge back out of the registry.
//
// Reading a metric you just wrote is unusual, but the alternative here is
// threading the probe results back through two layers of call sites purely to
// count them, and the gauge already holds exactly the answer.
func gaugeValue(vec *prometheus.GaugeVec, labels []string) float64 {
	g, err := vec.GetMetricWithLabelValues(labels...)
	if err != nil {
		return 0
	}
	return read(g)
}

func gaugeScalar(g prometheus.Gauge) float64 { return read(g) }

func read(m prometheus.Metric) float64 {
	var out dto.Metric
	if err := m.Write(&out); err != nil || out.Gauge == nil {
		return 0
	}
	return out.Gauge.GetValue()
}
