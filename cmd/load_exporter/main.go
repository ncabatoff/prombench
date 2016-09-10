package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
)

type (
	testCollector struct {
		descs      []*prometheus.Desc
		labelCount int
		cycle      int
	}
)

func main() {
	var (
		listenAddress = flag.String("web.listen-address", ":10000",
			"Address on which to expose metrics.")
		metricsPath = flag.String("web.telemetry-path", "/metrics",
			"Path under which to expose metrics.")
		metricCount = flag.Int("metric-count", 100,
			"how many metrics to expose per exporter")
		labelCount = flag.Int("label-count", 100,
			"how many labels to create per metric")
	)
	flag.Parse()

	descs := make([]*prometheus.Desc, *metricCount)
	for i := 0; i < *metricCount; i++ {
		metname := fmt.Sprintf("test%d", i)
		descs[i] = prometheus.NewDesc(metname, metname, []string{"lab"}, nil)
	}
	tc := &testCollector{descs: descs, labelCount: *labelCount}
	prometheus.MustRegister(tc)
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGTERM)
	go func() {
		<-sigchan
		fmt.Printf("%d", (*metricCount)*(*labelCount)*tc.cycle*(tc.cycle+1)/2)
		os.Exit(0)
	}()

	http.Handle(*metricsPath, prometheus.Handler())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>test exporter</title></head>
			<body>
			<h1>test exporter</h1>
			<p><a href="` + *metricsPath + `">Metrics</a></p>
			</body>
			</html>`))
	})
	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		log.Fatalf("Unable to setup HTTP server: %v", err)
	}
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
