package harness

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const (
	sdCfgDir = "sd_configs"
)

type Harness struct {
	testDirectory string
}

func (h *Harness) GetSdCfgDir() string {
	return filepath.Join(h.testDirectory, sdCfgDir)
}

func NewHarness(testDirectory string, rmIfPresent bool, scrapeInterval time.Duration, benchListenAddr, promListenAddr string) *Harness {
	SetupTestDir(testDirectory, rmIfPresent)
	h := &Harness{testDirectory}
	h.setupPrometheusConfig(scrapeInterval, benchListenAddr, promListenAddr)
	return h
}

func SetupTestDir(dir string, rm bool) {
	_, err := os.Open(dir)
	if os.IsNotExist(err) {
	} else if err != nil {
		log.Fatalf("error opening test dir '%s': %v", dir, err)
	} else if rm {
		rmcmd := exec.Command("rm", "-rf", dir)
		if err := rmcmd.Run(); err != nil {
			log.Fatalf("error deleting test dir '%s': %v", dir, err)
		}
	} else {
		log.Fatalf("error: test dir '%s' exists but I wasn't asked to delete it", dir)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		log.Fatalf("error creating test dir '%s': %v", dir, err)
	}
}

func (h *Harness) setupPrometheusConfig(scrapeInterval time.Duration, benchListenAddr, promListenAddr string) {
	cfgstr := fmt.Sprintf(`global:
scrape_configs:
  - job_name: 'prometheus'
    scrape_interval: '1s'
    static_configs:
      - targets: [%q]

  - job_name: 'prombench'
    scrape_interval: '1s'
    static_configs:
      - targets: [%q]

  - job_name: 'test'
    scrape_interval: '%s'
    file_sd_configs:
      - files:
        - '%s/*.json'`, promListenAddr, benchListenAddr, scrapeInterval, sdCfgDir)

	cfgfilename := filepath.Join(h.testDirectory, "prometheus.yml")
	if err := ioutil.WriteFile(cfgfilename, []byte(cfgstr), 0600); err != nil {
		log.Fatalf("unable to write config file '%s': %v", cfgfilename, err)
	}
	if err := os.Mkdir(h.GetSdCfgDir(), 0700); err != nil && !os.IsExist(err) {
		log.Fatalf("unable to create sd_config dir '%s': %v", h.GetSdCfgDir(), err)
	}
	// TODO clean out sd_config dir
}

func (h *Harness) StartPrometheus(ctx context.Context, prompath string, promargs []string) context.CancelFunc {
	vercmd := exec.Command(prompath, "-version")
	output, err := vercmd.Output()
	if err != nil {
		log.Fatalf("Prometheus returned %v", err)
	}
	log.Printf("Prometheus -version output: %s", string(output))

	myctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(myctx, prompath, promargs...)
	cmd.Dir = h.testDirectory
	done := make(chan struct{})
	promlog := filepath.Join(h.testDirectory, "prometheus.log")
	logfile, err := os.Create(promlog)
	if err != nil {
		log.Fatalf("unable to open log file '%s' for writing: %v", promlog, err)
	}
	cmd.Stdout = logfile
	cmd.Stderr = logfile
	go func() {
		log.Printf("running Prometheus in dir %q: %s %v", cmd.Dir, prompath, promargs)
		if err := cmd.Run(); err != nil {
			log.Printf("Prometheus returned %v, see log %q", err, promlog)
		} else {
			log.Printf("Prometheus exited, see log %q", promlog)
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
