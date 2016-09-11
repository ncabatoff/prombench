package prombench

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/api/prometheus"
	"github.com/prometheus/common/model"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	sdCfgDir = "sd_configs"
)

type Config struct {
	Rmdata         bool
	FirstPort      int
	PrometheusPath string
	ScrapeInterval time.Duration
}

func Run(cfg Config) {
	setupDataDir("data", cfg.Rmdata)
	setupPrometheusConfig(cfg.ScrapeInterval)
	mainctx := context.Background()
	stopPrometheus := startPrometheus(mainctx, cfg.PrometheusPath)
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

func setupDataDir(dir string, rm bool) {
	_, err := os.Open(dir)
	if os.IsNotExist(err) {
		log.Print("data dir already absent")
	} else if err != nil {
		log.Fatalf("error opening data dir: %v", err)
	} else if rm {
		log.Print("removing data dir")
		rmcmd := exec.Command("rm", "-rf", dir)
		if err := rmcmd.Run(); err != nil {
			log.Fatalf("error deleting data dir: %v", err)
		}
	} else {
		log.Fatalf("error: data dir exists but --rmdata not given")
	}
}

func setupPrometheusConfig(scrapeInterval time.Duration) {
	cfgstr := fmt.Sprintf(`global:
  scrape_interval: '%s'

scrape_configs:
  - job_name: 'test'
    file_sd_configs:
      - files:
        - '%s/*.json'`, scrapeInterval, sdCfgDir)

	cfgfilename := "prometheus.yml"
	if err := ioutil.WriteFile(cfgfilename, []byte(cfgstr), 0600); err != nil {
		log.Fatalf("unable to write config file '%s': %v", cfgfilename, err)
	}
	if err := os.Mkdir(sdCfgDir, 0700); err != nil && !os.IsExist(err) {
		log.Fatalf("unable to create sd_config dir '%s': %v", sdCfgDir, err)
	}
	// TODO clean out sd_config dir
}

func startPrometheus(ctx context.Context, prompath string) context.CancelFunc {
	log.Print("starting prometheus")
	myctx, cancel := context.WithCancel(ctx)
	promcmd := exec.CommandContext(myctx, prompath)
	done := make(chan struct{})
	promlog := "prometheus.log"
	logfile, err := os.Create(promlog)
	if err != nil {
		log.Fatalf("unable to open log file '%s' for writing: %v", promlog, err)
	}
	promcmd.Stdout = logfile
	promcmd.Stderr = logfile
	go func() {
		err := promcmd.Run()
		log.Printf("Prometheus returned %v", err)
		done <- struct{}{}
	}()
	return func() {
		log.Print("stopping prometheus")
		cancel()
		<-done
	}
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
	cfgfilename := filepath.Join(sdCfgDir, "load.json")
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
