package loadgen

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"strconv"
)

type (
	incCollector struct {
		descs      []*prometheus.Desc
		labelCount int
		cycle      int
	}
)

func NewIncCollector(nmetrics, nlabels int) *incCollector {
	descs := make([]*prometheus.Desc, nmetrics)
	for i := 0; i < nmetrics; i++ {
		metname := fmt.Sprintf("test%d", i)
		descs[i] = prometheus.NewDesc(metname, metname, []string{"lab"}, nil)
	}
	return &incCollector{descs: descs, labelCount: nlabels}
}

// Describe implements prometheus.Collector.
func (t *incCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range t.descs {
		ch <- desc
	}
}

// Collect implements prometheus.Collector.
func (t *incCollector) Collect(ch chan<- prometheus.Metric) {
	t.cycle++
	for _, desc := range t.descs {
		for j := 0; j < t.labelCount; j++ {
			ch <- prometheus.MustNewConstMetric(desc,
				prometheus.GaugeValue, float64(t.cycle), strconv.Itoa(j))
		}
	}
}

func (t *incCollector) Sum() int {
	return len(t.descs) * (t.labelCount) * t.cycle * (t.cycle + 1) / 2
}

type (
	staticCollector struct {
		descs      []*prometheus.Desc
		metrics    []prometheus.Metric
		labelCount int
		cycle      int
	}
)

func NewStaticCollector(nmetrics, nlabels int) *staticCollector {
	descs := make([]*prometheus.Desc, nmetrics)
	metrics := make([]prometheus.Metric, 0, nlabels*nmetrics)
	for i := 0; i < nmetrics; i++ {
		metname := fmt.Sprintf("test%d", i)
		desc := prometheus.NewDesc(metname, metname, []string{"lab"}, nil)
		descs[i] = desc
		for j := 0; j < nlabels; j++ {
			metrics = append(metrics, prometheus.MustNewConstMetric(desc,
				prometheus.GaugeValue, float64(1), strconv.Itoa(j)))
		}
	}
	return &staticCollector{descs: descs, metrics: metrics, labelCount: nlabels}
}

// Describe implements prometheus.Collector.
func (t *staticCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range t.descs {
		ch <- desc
	}
}

// Collect implements prometheus.Collector.
func (t *staticCollector) Collect(ch chan<- prometheus.Metric) {
	t.cycle++
	for _, metric := range t.metrics {
		ch <- metric
	}
}

func (t *staticCollector) Sum() int {
	return len(t.descs) * (t.labelCount) * t.cycle
}
