package harness

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type Config struct {
	Rmdata         bool
	FirstPort      int
	NumExporters   int
	PrometheusPath string
	ScrapeInterval time.Duration
	TestDuration   time.Duration
	ExtraArgs      []string
}

func SetupDataDir(dir string, rm bool) {
	_, err := os.Open(dir)
	if os.IsNotExist(err) {
	} else if err != nil {
		log.Fatalf("error opening data dir: %v", err)
	} else if rm {
		rmcmd := exec.Command("rm", "-rf", dir)
		if err := rmcmd.Run(); err != nil {
			log.Fatalf("error deleting data dir: %v", err)
		}
	} else {
		log.Fatalf("error: data dir exists but --rmdata not given")
	}
}

func SetupPrometheusConfig(sdCfgDir string, scrapeInterval time.Duration) {
	cfgstr := fmt.Sprintf(`global:
  scrape_interval: '%s'

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']

  - job_name: 'prombench'
    static_configs:
      - targets: ['localhost:9999']

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

func StartPrometheus(ctx context.Context, prompath string, promargs []string) context.CancelFunc {
	myctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(myctx, prompath, promargs...)
	done := make(chan struct{})
	promlog := "prometheus.log"
	logfile, err := os.Create(promlog)
	if err != nil {
		log.Fatalf("unable to open log file '%s' for writing: %v", promlog, err)
	}
	cmd.Stdout = logfile
	cmd.Stderr = logfile
	go func() {
		log.Printf("running Prometheus: %s %v", prompath, promargs)
		if err := cmd.Run(); err != nil {
			log.Printf("Prometheus returned %v", err)
		}
		done <- struct{}{}
	}()
	return func() {
		cmd.Process.Signal(syscall.SIGTERM)
		timer := time.NewTimer(30 * time.Second)
		select {
		case <-timer.C:
			cancel()
			<-done
		case <-done:
		}
	}
}
