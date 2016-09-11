package main

import (
	"flag"
	"github.com/ncabatoff/prombench"
	"github.com/ncabatoff/prombench/harness"
	"time"
)

func main() {
	var (
		firstPort = flag.Int("first-port", 10000,
			"First port to assign to load exporters.")
		rmdata = flag.Bool("rmdata", false,
			"delete the data dir before starting Prometheus")
		prometheusPath = flag.String("prometheus-path", "prometheus",
			"path to prometheus executable")
		scrapeInterval = flag.Duration("scrape-interval", time.Second,
			"scrape interval")
	)
	flag.Parse()
	prombench.Run(harness.Config{
		FirstPort:      *firstPort,
		Rmdata:         *rmdata,
		PrometheusPath: *prometheusPath,
		ScrapeInterval: *scrapeInterval})
}
