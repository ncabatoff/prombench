package prombench

import (
	"context"
	"fmt"
	"github.com/ncabatoff/prombench/harness"
	"github.com/ncabatoff/prombench/loadgen"
	api "github.com/prometheus/client_golang/api/prometheus"
	"github.com/prometheus/client_golang/prometheus"
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
		TestRetention  time.Duration
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

type extraPrometheusArgsCollector struct {
	descs   []*prometheus.Desc
	metrics []prometheus.Metric
}

func newExtraPrometheusArgsCollector(args []string, retention time.Duration) *extraPrometheusArgsCollector {
	epac := extraPrometheusArgsCollector{}
	for i := 0; i < len(args)-1; i += 2 {
		val, err := strconv.Atoi(args[i+1])
		if err == nil {
			nodashes := strings.TrimLeft(args[i], "-")
			name := "prometheus_arg_" + strings.Replace(strings.Replace(nodashes, "-", "_", -1), ".", "_", -1)
			help := fmt.Sprintf("value of prometheus -%s option", nodashes)
			desc := prometheus.NewDesc(name, help, nil, nil)
			epac.descs = append(epac.descs, desc)
			epac.metrics = append(epac.metrics, prometheus.MustNewConstMetric(desc,
				prometheus.GaugeValue, float64(val)))
		}
	}
	if retention > 0 {
		nodashes := "storage.local.retention"
		name := "prometheus_arg_" + strings.Replace(strings.Replace(nodashes, "-", "_", -1), ".", "_", -1) + "_seconds"
		help := fmt.Sprintf("value of prometheus -%s option in seconds", nodashes)
		desc := prometheus.NewDesc(name, help, nil, nil)
		epac.descs = append(epac.descs, desc)
		epac.metrics = append(epac.metrics, prometheus.MustNewConstMetric(desc,
			prometheus.GaugeValue, retention.Seconds()))

	}
	return &epac
}

// Describe implements prometheus.Collector.
func (epac *extraPrometheusArgsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range epac.descs {
		ch <- desc
	}
}

// Collect implements prometheus.Collector.
func (epac *extraPrometheusArgsCollector) Collect(ch chan<- prometheus.Metric) {
	for _, metric := range epac.metrics {
		ch <- metric
	}
}

func Run(cfg Config) {
	mainctx := context.Background()
	harness.SetupDataDir("data", cfg.Rmdata)
	harness.SetupPrometheusConfig(SdCfgDir, cfg.ScrapeInterval)
	extraArgs := cfg.ExtraArgs
	if cfg.TestRetention > 0 {
		extraArgs = append(extraArgs, "-storage.local.retention",
			fmt.Sprintf("%ds", int(cfg.TestRetention.Seconds())))
	}
	if len(extraArgs) > 0 {
		prometheus.MustRegister(newExtraPrometheusArgsCollector(extraArgs, cfg.TestRetention))
	}
	stopPrometheus := harness.StartPrometheus(mainctx, cfg.PrometheusPath, extraArgs)
	defer stopPrometheus()

	le := loadgen.NewLoadExporterInternal(mainctx, SdCfgDir)
	startExporters(le, cfg.Exporters, cfg.FirstPort)

	startTime := time.Now()
	time.Sleep(cfg.TestDuration)
	expectedSums, err := le.Stop()
	log.Printf("sums=%v, err=%v", expectedSums, err)
	var totalDelta int
	for _, instsum := range expectedSums {
		expectedSum, instance := instsum.Sum, instsum.Instance
		var delta int
		// ttime is used to work out what our expected sum should be, assuming on average each scrape
		// yields about the same sum, which isn't true for many non-cyclic/constant exporters, e.g. inc.
		// To make this approach work for them we'll want to allow for an option to use sum(rate) rather
		// than sum(sum_over_time).
		ttime := time.Since(startTime)
		if ttime > cfg.TestRetention {
			timeRatio := float64(cfg.TestRetention) / float64(ttime)
			expectedSum = int(timeRatio * float64(expectedSum))
		}
		for i := 0; i < 2; i++ {
			// qtime is how long the query range should be, i.e. it covers from test start to now
			qtime := time.Since(startTime)
			ttimestr := fmt.Sprintf("%ds", int(1+qtime.Seconds()))
			query := fmt.Sprintf(`sum(sum_over_time({__name__=~"test.+", instance="%s"}[%s]))`, instance, ttimestr)
			vect := queryPrometheusVector("http://localhost:9090", query)
			actualSum := -1
			if len(vect) > 0 {
				actualSum = int(vect[0].Value)
			}
			delta = expectedSum - actualSum
			deltaPct := int(100 * float64(delta) / float64(expectedSum))
			log.Printf("Expected %d, got %d (delta=%d or %d%%)", expectedSum, actualSum, delta, deltaPct)
			absratio := deltaPct
			if absratio < 0 {
				absratio = -absratio
			}
			if absratio <= 15 {
				break
			}
			time.Sleep(5 * time.Second)
		}
		if delta < 0 {
			delta = -delta
		}
		totalDelta += delta
	}
	log.Printf("total delta=%d", totalDelta)
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
	cfg := api.Config{Address: url, Transport: api.DefaultTransport}
	client, err := api.New(cfg)
	if err != nil {
		log.Fatalf("error building client: %v", err)
	}
	qapi := api.NewQueryAPI(client)
	log.Printf("issueing query: %s", query)
	result, err := qapi.Query(context.TODO(), query, time.Now())
	if err != nil {
		log.Printf("error performing query: %v", err)
		return nil
	}
	log.Printf("prometheus query result: %v", result)
	return result.(model.Vector)
}
