package loadgen

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"strconv"
)

type (
	testCollector struct {
		descs      []*prometheus.Desc
		labelCount int
		cycle      int
	}
)

func NewTestCollector(nmetrics, nlabels int) *testCollector {
	descs := make([]*prometheus.Desc, nmetrics)
	for i := 0; i < nmetrics; i++ {
		metname := fmt.Sprintf("test%d", i)
		descs[i] = prometheus.NewDesc(metname, metname, []string{"lab"}, nil)
	}
	return &testCollector{descs: descs, labelCount: nlabels}
}

// Describe implements prometheus.Collector.
func (t *testCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range t.descs {
		ch <- desc
	}
}

// Collect implements prometheus.Collector.
func (t *testCollector) Collect(ch chan<- prometheus.Metric) {
	t.cycle++
	for _, desc := range t.descs {
		for j := 0; j < t.labelCount; j++ {
			ch <- prometheus.MustNewConstMetric(desc,
				prometheus.GaugeValue, float64(t.cycle), strconv.Itoa(j))
		}
	}
}

func (t *testCollector) Sum() int {
	return len(t.descs) * (t.labelCount) * t.cycle * (t.cycle + 1) / 2
}
