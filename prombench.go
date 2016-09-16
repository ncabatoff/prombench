package prombench

import (
	"context"
	"fmt"
	"github.com/ncabatoff/prombench/harness"
	"github.com/ncabatoff/prombench/loadgen"
	"github.com/prometheus/client_golang/api/prometheus"
	"github.com/prometheus/common/model"
	"log"
	"strconv"
	"strings"
	"time"
)

const (
	SdCfgDir = "sd_configs"
)

//go:generate stringer -type=LoadExporterKind
type LoadExporterKind int

const (
	ExporterInc LoadExporterKind = iota
	ExporterStatic
	ExporterRandCyclic
	ExporterOscillate
)

type (
	ExporterSpec struct {
		Exporter LoadExporterKind
		Count    int
	}
	ExporterSpecList []ExporterSpec

	Config struct {
		Rmdata         bool
		FirstPort      int
		PrometheusPath string
		ScrapeInterval time.Duration
		TestDuration   time.Duration
		ExtraArgs      []string
		Exporters      ExporterSpecList
	}
)

func (esl *ExporterSpecList) String() string {
	ss := make([]string, len(*esl))
	for i, es := range *esl {
		ss[i] = es.String()
	}
	return strings.Join(ss, ",")
}

func (esl *ExporterSpecList) Get() interface{} {
	return *esl
}

func (esl *ExporterSpecList) Set(v string) error {
	ss := strings.Split(v, ",")
	*esl = make([]ExporterSpec, len(ss))
	for i, s := range ss {
		if err := (*esl)[i].Set(s); err != nil {
			return fmt.Errorf("error parsing exporter spec list '%s', spec '%s' has error: %v", v, s, err)
		}
	}
	return nil
}

func (e *ExporterSpec) String() string {
	return fmt.Sprintf("%s:%d", e.Exporter, e.Count)
}

func (e *ExporterSpec) Get() interface{} {
	return *e
}

func (e *ExporterSpec) Set(v string) error {
	pieces := strings.SplitN(v, ":", 2)
	if len(pieces) != 2 {
		return fmt.Errorf("bad exporter spec '%s': must be of the form 'name:count'", v)
	}

	switch pieces[0] {
	case "inc":
		e.Exporter = ExporterInc
	case "static":
		e.Exporter = ExporterStatic
	case "randcyclic":
		e.Exporter = ExporterRandCyclic
	case "oscillate":
		e.Exporter = ExporterOscillate
	default:
		return fmt.Errorf("invalid exporter name '%s'", pieces[0])
	}
	if c, err := strconv.Atoi(pieces[1]); err != nil || c <= 0 {
		return fmt.Errorf("invalid exporter count '%s'", pieces[1])
	} else {
		e.Count = c
	}
	return nil
}

func Run(cfg Config) {
	mainctx := context.Background()
	harness.SetupDataDir("data", cfg.Rmdata)
	harness.SetupPrometheusConfig(SdCfgDir, cfg.ScrapeInterval)
	stopPrometheus := harness.StartPrometheus(mainctx, cfg.PrometheusPath, cfg.ExtraArgs)
	defer stopPrometheus()

	le := loadgen.NewLoadExporterInternal(mainctx, SdCfgDir)
	startExporters(le, cfg.Exporters, cfg.FirstPort)

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

func startExporters(le loadgen.LoadExporter, esl ExporterSpecList, firstPort int) {
	log.Printf("starting exporters: %s", esl.String())
	exporterCount := 0
	for _, exporterSpec := range esl {
		for i := 0; i < exporterSpec.Count; i++ {
			var exporter loadgen.HttpExporter
			switch exporterSpec.Exporter {
			case ExporterInc:
				exporter = loadgen.NewHttpExporter(loadgen.NewIncCollector(100, 100))
			case ExporterStatic:
				exporter = loadgen.NewHttpExporter(loadgen.NewStaticCollector(100, 100))
			case ExporterRandCyclic:
				exporter = loadgen.NewHttpExporter(loadgen.NewRandCyclicCollector(100, 100, 100000))
			case ExporterOscillate:
				exporter = loadgen.NewReplayHandler(loadgen.NewHttpExporter(loadgen.NewIncCollector(100, 100)))
			default:
				log.Fatalf("invalid exporter '%s'", exporterSpec.Exporter)
			}
			if err := le.AddTarget(firstPort+exporterCount, exporterSpec.Exporter.String(), exporter); err != nil {
				log.Fatalf("Error starting exporter: %v", err)
			} else {
				exporterCount++
			}
		}
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
