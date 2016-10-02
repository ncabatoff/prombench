package main

import (
	"flag"
	"github.com/ncabatoff/prombench"
	"github.com/prometheus/client_golang/prometheus"
	"net/http"
	_ "net/http/pprof"
	"time"
)

func main() {
	var (
		firstPort = flag.Int("first-port", 10000,
			"First port to assign to load exporters.")
		exporters = &prombench.ExporterSpecList{prombench.ExporterSpec{prombench.ExporterInc, 3}}
		rmdata    = flag.Bool("rmdata", false,
			"delete the data dir before starting Prometheus")
		prometheusPath = flag.String("prometheus-path", "prometheus",
			"path to prometheus executable")
		scrapeInterval = flag.Duration("scrape-interval", time.Second,
			"scrape interval")
		testDuration = flag.Duration("test-duration", time.Minute,
			"test duration")
		testRetention = flag.Duration("test-retention", 5*time.Minute,
			"retention period: will be passed to Prometheus as storage.local.retention")
	)
	flag.Var(exporters, "exporters", "Comma-separated list of exporter:count, where exporter is one of: inc, static, randcyclic, oscillate")
	flag.Parse()
	http.Handle("/metrics", prometheus.Handler())
	go http.ListenAndServe(":9999", nil)
	prombench.Run(prombench.Config{
		FirstPort:      *firstPort,
		Exporters:      *exporters,
		Rmdata:         *rmdata,
		PrometheusPath: *prometheusPath,
		ScrapeInterval: *scrapeInterval,
		TestDuration:   *testDuration,
		TestRetention:  *testRetention,
		ExtraArgs:      flag.Args(),
	})
}
