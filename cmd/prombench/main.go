package main

import (
	"flag"
	"github.com/ncabatoff/prombench"
	"github.com/ncabatoff/prombench/harness"
	"github.com/prometheus/client_golang/prometheus"
	"net/http"
	"time"
)

func main() {
	var (
		firstPort = flag.Int("first-port", 10000,
			"First port to assign to load exporters.")
		numExporters = flag.Int("num-exporters", 3,
			"Number of exporters to run.")
		rmdata = flag.Bool("rmdata", false,
			"delete the data dir before starting Prometheus")
		prometheusPath = flag.String("prometheus-path", "prometheus",
			"path to prometheus executable")
		scrapeInterval = flag.Duration("scrape-interval", time.Second,
			"scrape interval")
		testDuration = flag.Duration("test-duration", time.Minute,
			"test duration")
	)
	flag.Parse()
	http.Handle("/metrics", prometheus.Handler())
	go http.ListenAndServe("localhost:9999", nil)
	prombench.Run(harness.Config{
		FirstPort:      *firstPort,
		NumExporters:   *numExporters,
		Rmdata:         *rmdata,
		PrometheusPath: *prometheusPath,
		ScrapeInterval: *scrapeInterval,
		TestDuration:   *testDuration,
	})
}
