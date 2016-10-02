package main

import (
	"flag"
	"fmt"
	"github.com/ncabatoff/prombench"
	"github.com/prometheus/client_golang/prometheus"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [-- path-to-prometheus prometheus-options]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nIf no -- is present, will execute 'prometheus' based on $PATH.\n")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
	}
	var (
		firstPort = flag.Int("first-port", 10000,
			"First port to assign to load exporters.")
		exporters = &prombench.ExporterSpecList{prombench.ExporterSpec{prombench.ExporterInc, 3}}
		rmtestdir = flag.Bool("rmtestdir", false,
			"delete the test dir if present")
		scrapeInterval = flag.Duration("scrape-interval", time.Second,
			"scrape interval")
		testDirectory = flag.String("test-directory", "prombench",
			"directory in which all writes will take place")
		testDuration = flag.Duration("test-duration", time.Minute,
			"test duration")
		testRetention = flag.Duration("test-retention", 5*time.Minute,
			"retention period: will be passed to Prometheus as storage.local.retention")
	)
	flag.Var(exporters, "exporters", "Comma-separated list of exporter:count, where exporter is one of: inc, static, randcyclic, oscillate")
	flag.Parse()

	extraArgs := flag.Args()
	promPath := "prometheus"
	if len(extraArgs) > 0 {
		promPath = extraArgs[0]
		extraArgs = extraArgs[1:]
	}

	http.Handle("/metrics", prometheus.Handler())
	go http.ListenAndServe(":9999", nil)
	prombench.Run(prombench.Config{
		FirstPort:       *firstPort,
		Exporters:       *exporters,
		TestDirectory:   *testDirectory,
		RmTestDirectory: *rmtestdir,
		PrometheusPath:  promPath,
		ScrapeInterval:  *scrapeInterval,
		TestDuration:    *testDuration,
		TestRetention:   *testRetention,
		ExtraArgs:       extraArgs,
	})
}
