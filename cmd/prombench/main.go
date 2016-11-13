package main

import (
	"flag"
	"fmt"
	"github.com/ncabatoff/prombench"
	"github.com/prometheus/client_golang/prometheus"
	"io"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
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
		adaptiveInterval = flag.Duration("adaptive-interval", 0,
			"if nonzero, interval at which to try adding new exporters if not yet too loaded")
		testDirectory = flag.String("test-directory", "prombench-data",
			"directory in which all writes will take place")
		testDuration = flag.Duration("test-duration", time.Minute,
			"test duration")
		testRetention = flag.Duration("test-retention", 5*time.Minute,
			"retention period: will be passed to Prometheus as storage.local.retention")
		maxDeltaRatio = flag.Float64("max-delta-ratio", 0.15,
			"absolute deviation from expected value tolerated without query retry [0-1]")
		maxQueryRetries = flag.Int("max-query-retries", 0,
			"how many query retries to do until maxDeltaRatio is satisfied")
		benchListenAddress = flag.String("web.listen-address", ":9999",
			"Address on which to expose prombench metrics.")
		promListenAddress = flag.String("prometheus.listen-address", ":8989",
			"Address on which the Prometheus being tested exposes metrics and serves queries.")
		runIntervals = &prombench.RunIntervalSpecList{}
	)
	flag.Var(exporters, "exporters", "Comma-separated list of exporter:count, where exporter is one of: inc, static, randcyclic, oscillate")
	flag.Var(runIntervals, "run-every", "Comma-separated list of interval:command, invoke command every interval duration")
	flag.Parse()

	extraArgs := flag.Args()
	promPath := "prometheus"
	if len(extraArgs) > 0 {
		promPath = extraArgs[0]
		extraArgs = extraArgs[1:]
	}

	http.Handle("/metrics", prometheus.Handler())
	go http.ListenAndServe(*benchListenAddress, nil)
	prombench.Run(prombench.Config{
		FirstPort:               *firstPort,
		Exporters:               *exporters,
		TestDirectory:           *testDirectory,
		RmTestDirectory:         *rmtestdir,
		PrometheusPath:          promPath,
		ScrapeInterval:          *scrapeInterval,
		TestDuration:            *testDuration,
		TestRetention:           *testRetention,
		MaxDeltaRatio:           *maxDeltaRatio,
		MaxQueryRetries:         *maxQueryRetries,
		ExtraArgs:               extraArgs,
		RunIntervals:            *runIntervals,
		AdaptiveInterval:        *adaptiveInterval,
		PrombenchListenAddress:  *benchListenAddress,
		PrometheusListenAddress: *promListenAddress,
	})

	writeMetrics(*benchListenAddress, *testDirectory)
	time.Sleep(5 * time.Second)
}

func writeMetrics(listenAddr, testdir string) {
	resp, err := http.Get("http://" + listenAddr + "/metrics")
	if err != nil {
		log.Fatalf("error querying my own metrics: %v", err)
	}
	fn := filepath.Join(testdir, "metrics.txt")
	metricsFile, err := os.Create(fn)
	if err != nil {
		log.Fatalf("error writing metrics to %q: %v", fn, err)
	}

	_, err = io.Copy(metricsFile, resp.Body)
	if err != nil {
		log.Fatalf("error writing metrics: %v", err)
	}
	resp.Body.Close()
	err = metricsFile.Close()
	if err != nil {
		log.Fatalf("error writing metrics: %v", err)
	}
}
