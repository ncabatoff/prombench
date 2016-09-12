package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/ncabatoff/prombench/loadgen"
	"github.com/prometheus/client_golang/prometheus"
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

	tc := loadgen.NewIncCollector(*metricCount, *labelCount)
	prometheus.MustRegister(tc)
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGTERM)
	go func() {
		<-sigchan
		fmt.Printf("%d", tc.Sum())
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
