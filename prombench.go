package prombench

import (
	"context"
	"fmt"
	"github.com/ncabatoff/prombench/harness"
	"github.com/prometheus/client_golang/api/prometheus"
	"github.com/prometheus/common/model"
	"io/ioutil"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func Run(cfg harness.Config) {
	harness.SetupDataDir("data", cfg.Rmdata)
	harness.SetupPrometheusConfig(cfg.ScrapeInterval)
	mainctx := context.Background()
	stopPrometheus := harness.StartPrometheus(mainctx, cfg.PrometheusPath)
	sums := make(chan int)
	stopExporter := startExporter(mainctx, cfg.FirstPort, sums)
	time.Sleep(10 * time.Second)
	stopExporter()
	sum := <-sums
	log.Printf("sum reported by exporters: %d", sum)
	time.Sleep(1 * time.Second)
	qresult := queryPrometheus()
	if sum != qresult {
		log.Printf("Expected %d, got %d", sum, qresult)
	}
	stopPrometheus()
}

func startExporter(ctx context.Context, port int, sum chan<- int) context.CancelFunc {
	log.Print("starting exporters")
	myctx, cancel := context.WithCancel(ctx)
	addr := fmt.Sprintf("localhost:%d", port)
	sdjson := fmt.Sprintf(`[
  {
    "targets": [ "%s" ],
    "labels": {
    }
  }
]`, addr)
	cfgfilename := filepath.Join(harness.SdCfgDir, "load.json")
	if err := ioutil.WriteFile(cfgfilename, []byte(sdjson), 0600); err != nil {
		log.Fatalf("unable to write sd_config file '%s': %v", cfgfilename, err)
	}
	cmd := exec.CommandContext(myctx, "../load_exporter/load_exporter", "-web.listen-address", addr)
	go func() {
		output, err := cmd.Output()
		if err != nil {
			log.Printf("load_exporter returned %v; output:\n%s", err, output)
		}
		thissum, err := strconv.Atoi(string(output))
		if err != nil {
			log.Printf("error parsing load_exporter output '%s', error: %v", string(output), err)
		}
		sum <- thissum
	}()
	return func() {
		log.Print("stopping exporters")
		cmd.Process.Signal(syscall.SIGTERM)
		time.Sleep(time.Millisecond * 100)
		cancel()
	}
}

func queryPrometheus() int {
	cfg := prometheus.Config{Address: "http://localhost:9090", Transport: prometheus.DefaultTransport}
	client, err := prometheus.New(cfg)
	if err != nil {
		log.Fatalf("error building client: %v", err)
	}
	qapi := prometheus.NewQueryAPI(client)
	query := `sum(sum_over_time({__name__=~"test.+"}[1m]))`
	result, err := qapi.Query(context.TODO(), query, time.Now())
	if err != nil {
		log.Fatalf("error performing query: %v", err)
	}
	log.Printf("prometheus query result: %v", result)
	vect := result.(model.Vector)
	return int(vect[0].Value)
}
