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
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

//go:generate stringer -type=LoadExporterKind
type LoadExporterKind int

const (
	ExporterInc LoadExporterKind = iota
	ExporterStatic
	ExporterRandCyclic
	ExporterOscillate
)

var (
	QueryTime *prometheus.HistogramVec = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "prombench",
			Subsystem: "query",
			Name:      "latency_seconds",
			Help:      "time to execute query",
			ConstLabels: prometheus.Labels{
				// Only benchmark supported for now so constant
				"benchmark": "insert-then-sum",
			},
		},
		[]string{"run_name", "query"},
	)
)

func init() {
	prometheus.MustRegister(QueryTime)
}

type (
	ExporterSpec struct {
		Exporter LoadExporterKind
		Count    int
	}
	ExporterSpecList []ExporterSpec
	RunIntervalSpec  struct {
		Command  string
		Interval time.Duration
	}
	RunIntervalSpecList []RunIntervalSpec

	Config struct {
		TestDirectory           string
		RmTestDirectory         bool
		FirstPort               int
		PrometheusPath          string
		ScrapeInterval          time.Duration
		TestDuration            time.Duration
		TestRetention           time.Duration
		ExtraArgs               []string
		Exporters               ExporterSpecList
		RunIntervals            RunIntervalSpecList
		MaxDeltaRatio           float64
		MaxQueryRetries         int
		AdaptiveInterval        time.Duration
		PrombenchListenAddress  string
		PrometheusListenAddress string
	}
)

// TODO check for errors when the Config is created
func (c Config) PrometheusInstance() (string, error) {
	host, port, err := net.SplitHostPort(c.PrometheusListenAddress)
	if err != nil {
		return "", err
	}
	if len(host) == 0 {
		host = "localhost"
	}
	return net.JoinHostPort(host, port), nil
}

func (r *RunIntervalSpec) String() string {
	return fmt.Sprintf("%s:%s", r.Interval, r.Command)
}

func (r *RunIntervalSpec) Get() interface{} {
	return *r
}

func (r *RunIntervalSpec) Set(v string) error {
	pieces := strings.SplitN(v, ":", 2)
	if len(pieces) != 2 {
		return fmt.Errorf("bad runinterval spec '%s': must be of the form 'interval:command'", v)
	}
	dur, err := time.ParseDuration(pieces[0])
	if err != nil {
		return fmt.Errorf("invalid duration in runinterval '%s': %v", v, err)
	}
	r.Interval = dur
	r.Command = pieces[1]
	return nil
}

func (rsl *RunIntervalSpecList) String() string {
	ss := make([]string, len(*rsl))
	for i, rs := range *rsl {
		ss[i] = rs.String()
	}
	return strings.Join(ss, ",")
}

func (rsl *RunIntervalSpecList) Get() interface{} {
	return *rsl
}

func (rsl *RunIntervalSpecList) Set(v string) error {
	ss := strings.Split(v, ",")
	*rsl = make([]RunIntervalSpec, len(ss))
	for i, s := range ss {
		if err := (*rsl)[i].Set(s); err != nil {
			return fmt.Errorf("error parsing run interval spec list '%s', spec '%s' has error: %v", v, s, err)
		}
	}
	return nil
}

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

func startExportersAdaptive(ctx context.Context, le loadgen.LoadExporter, firstPort int, cfg Config) context.CancelFunc {
	instance, err := cfg.PrometheusInstance()
	if err != nil {
		log.Fatalf("can't construct query URL: %v", err)
	}
	queryUrl := "http://" + instance

	myctx, cancel := context.WithCancel(ctx)
	go func() {
		query := fmt.Sprintf(`prometheus_target_interval_length_seconds{quantile="0.99", interval="%s"}`,
			cfg.ScrapeInterval)
		ticker := time.NewTicker(cfg.AdaptiveInterval)
		done := myctx.Done()
		for {
			select {
			case <-done:
				ticker.Stop()
				cancel()
				break
			case <-ticker.C:
				vect := queryPrometheusVector(myctx, queryUrl, query)
				if len(vect) != 1 {
					log.Printf("error querying scrape interval: %d results returned", len(vect))
					continue
				}
				secs := time.Duration(float64(time.Second) * float64(vect[0].Value))
				deltaSecs := secs - cfg.ScrapeInterval
				if deltaSecs < cfg.ScrapeInterval/20 {
					log.Printf("99th percentile of scrape interval %s within 5%% (delta %s), adding targets", cfg.ScrapeInterval, deltaSecs)
					firstPort += startExporters(le, cfg.Exporters, firstPort)
				}
			}
		}
	}()
	return cancel
}

func getExtraArgs(cfg Config) []string {
	extraArgs := append([]string{}, cfg.ExtraArgs...)
	if cfg.TestRetention > 0 {
		extraArgs = append(extraArgs, "-storage.local.retention",
			fmt.Sprintf("%ds", int(cfg.TestRetention.Seconds())))
	}
	if len(extraArgs) > 0 {
		prometheus.MustRegister(newExtraPrometheusArgsCollector(extraArgs, cfg.TestRetention))
	}
	return append(extraArgs, "-web.listen-address", cfg.PrometheusListenAddress)
}

func waitForPrometheus(ctx context.Context, instance string) bool {
	queryUrl := "http://" + instance
	// TODO make timeout configurable
	endTime := time.Now().Add(time.Second * 10)
	for {
		timeLeft := endTime.Sub(time.Now())
		if timeLeft < 0 {
			log.Printf("Timed out waiting for Prometheus to respond.")
			return false
		}

		myctx, cancel := context.WithTimeout(ctx, timeLeft)
		query := fmt.Sprintf(`up{job="prometheus", instance="%s"}`, instance)
		vect := queryPrometheusVector(myctx, queryUrl, query)
		cancel()

		if len(vect) > 0 {
			if vect[0].Value > 0 {
				log.Printf("prometheus is up")
				return true
			}
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func Run(cfg Config) {
	instance, err := cfg.PrometheusInstance()
	if err != nil {
		log.Fatalf("can't construct query URL: %v", err)
	}
	queryUrl := "http://" + instance

	mainctx := context.Background()
	h := harness.NewHarness(cfg.TestDirectory, cfg.RmTestDirectory, cfg.ScrapeInterval, cfg.PrombenchListenAddress, instance)

	stopPrometheus := h.StartPrometheus(mainctx, cfg.PrometheusPath, getExtraArgs(cfg))
	defer stopPrometheus()

	if !waitForPrometheus(mainctx, instance) {
		return
	}

	le := loadgen.NewLoadExporterInternal(mainctx, h.GetSdCfgDir())
	exporterCount := startExporters(le, cfg.Exporters, cfg.FirstPort)
	cancelAdaptive := func() {}
	if cfg.AdaptiveInterval > 0 {
		cancelAdaptive = startExportersAdaptive(mainctx, le, cfg.FirstPort+exporterCount, cfg)
	}

	cancelRunIntervals := startRunIntervals(mainctx, cfg.RunIntervals)
	defer cancelRunIntervals()

	startTime := time.Now()
	time.Sleep(cfg.TestDuration)
	cancelAdaptive()
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
		for i := 0; i <= cfg.MaxQueryRetries; i++ {
			log.Printf("query %s %d (maxretries=%d)", instance, i+1, cfg.MaxQueryRetries)
			// qtime is how long the query range should be, i.e. it covers from test start to now
			qtime := time.Since(startTime)
			ttimestr := fmt.Sprintf("%ds", int(1+qtime.Seconds()))
			query := fmt.Sprintf(`sum(sum_over_time({__name__=~"test.+", instance="%s"}[%s]))`, instance, ttimestr)
			queryStart := time.Now()
			vect := queryPrometheusVector(mainctx, queryUrl, query)
			QueryTime.WithLabelValues("run1", query).Observe(time.Since(queryStart).Seconds())

			actualSum := -1
			if len(vect) > 0 {
				actualSum = int(vect[0].Value)
			}
			delta = expectedSum - actualSum
			deltaRatio := float64(delta) / float64(expectedSum)
			log.Printf("Expected %d, got %d (delta=%d or %.0f%%)", expectedSum, actualSum, delta, 100*deltaRatio)
			absRatio := deltaRatio
			if absRatio < 0 {
				absRatio = -absRatio
			}
			if absRatio <= cfg.MaxDeltaRatio {
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

func startRunIntervals(ctx context.Context, ris RunIntervalSpecList) func() {
	if len(ris) == 0 {
		return func() {}
	}
	myctx, cancel := context.WithCancel(ctx)
	for _, ri := range ris {
		startRunInterval(myctx, ri)
	}
	return cancel
}

func startRunInterval(ctx context.Context, ri RunIntervalSpec) func() {
	myctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(ri.Interval)
		done := myctx.Done()
		for {
			select {
			case <-done:
				ticker.Stop()
				cancel()
				break
			case <-ticker.C:
				log.Printf("running %s", ri.Command)
				cmd := exec.CommandContext(myctx, "sh", "-c", ri.Command)
				out, err := cmd.CombinedOutput()
				if err != nil {
					log.Printf("error running background command '%s': %v; output follows:\n%s", ri.Command, err, string(out))
				}
				log.Printf("ran %s", ri.Command)
			}
		}
	}()
	return cancel
}

func startExporters(le loadgen.LoadExporter, esl ExporterSpecList, firstPort int) int {
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
	return exporterCount
}

func queryPrometheusVector(ctx context.Context, url, query string) model.Vector {
	cfg := api.Config{Address: url, Transport: api.DefaultTransport}
	client, err := api.New(cfg)
	if err != nil {
		log.Fatalf("error building client: %v", err)
	}
	qapi := api.NewQueryAPI(client)
	// log.Printf("issueing query: %s to %s", query, url)
	result, err := qapi.Query(ctx, query, time.Now())
	if err != nil {
		log.Printf("error performing query: %v", err)
		return nil
	}
	// log.Printf("prometheus query result: %v", result)
	return result.(model.Vector)
}
