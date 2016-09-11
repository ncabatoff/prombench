package prombench

import (
	"context"
	"github.com/ncabatoff/prombench/harness"
	"github.com/ncabatoff/prombench/loadgen"
	"github.com/prometheus/client_golang/api/prometheus"
	"github.com/prometheus/common/model"
	"log"
	"time"
)

const (
	SdCfgDir = "sd_configs"
)

func Run(cfg harness.Config) {
	harness.SetupDataDir("data", cfg.Rmdata)
	harness.SetupPrometheusConfig(SdCfgDir, cfg.ScrapeInterval)
	mainctx := context.Background()
	stopPrometheus := harness.StartPrometheus(mainctx, cfg.PrometheusPath)
	sums := make(chan int)
	stopExporter := loadgen.StartExporter(SdCfgDir, mainctx, cfg.FirstPort, sums)
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
