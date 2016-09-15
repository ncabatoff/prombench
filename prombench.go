package prombench

import (
	"context"
	"fmt"
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
	mainctx := context.Background()
	harness.SetupDataDir("data", cfg.Rmdata)
	harness.SetupPrometheusConfig(SdCfgDir, cfg.ScrapeInterval)
	stopPrometheus := harness.StartPrometheus(mainctx, cfg.PrometheusPath, cfg.ExtraArgs)
	defer stopPrometheus()

	exporterProvider := func() loadgen.HttpExporter {
		switch cfg.Exporter {
		case "inc":
			return loadgen.NewHttpExporter(loadgen.NewIncCollector(100, 100))
		case "static":
			return loadgen.NewHttpExporter(loadgen.NewStaticCollector(100, 100))
		case "randcyclic":
			return loadgen.NewHttpExporter(loadgen.NewRandCyclicCollector(100, 100, 100000))
		case "oscillate":
			return loadgen.NewReplayHandler(loadgen.NewHttpExporter(loadgen.NewIncCollector(100, 100)))
		default:
			panic(fmt.Sprintf("invalid exporter name '%s'", cfg.Exporter))
		}
	}
	le := loadgen.NewLoadExporterInternal(mainctx, SdCfgDir, exporterProvider)
	for i := 0; i < cfg.NumExporters; i++ {
		if err := le.AddTarget(cfg.FirstPort + i); err != nil {
			log.Fatalf("Error starting exporter: %v", err)
		}
	}

	startTime := time.Now()
	time.Sleep(cfg.TestDuration)
	expectedSum, err := le.Stop()
	log.Printf("sum=%d, err=%v", expectedSum, err)
	for {
		ttime := time.Since(startTime)
		ttimestr := fmt.Sprintf("%ds", int(1+ttime.Seconds()))
		query := fmt.Sprintf(`sum(sum_over_time({__name__=~"test.+"}[%s]))`, ttimestr)
		vect := queryPrometheusVector("http://localhost:9090", query)
		actualSum := -1
		if len(vect) > 0 {
			actualSum = int(vect[0].Value)
		}
		if expectedSum == actualSum {
			break
		}
		log.Printf("Expected %d, got %d", expectedSum, actualSum)
		time.Sleep(5 * time.Second)
	}
}

func queryPrometheusVector(url, query string) model.Vector {
	cfg := prometheus.Config{Address: url, Transport: prometheus.DefaultTransport}
	client, err := prometheus.New(cfg)
	if err != nil {
		log.Fatalf("error building client: %v", err)
	}
	qapi := prometheus.NewQueryAPI(client)
	log.Printf("issueing query: %s", query)
	result, err := qapi.Query(context.TODO(), query, time.Now())
	if err != nil {
		log.Printf("error performing query: %v", err)
		return nil
	}
	log.Printf("prometheus query result: %v", result)
	return result.(model.Vector)
}
