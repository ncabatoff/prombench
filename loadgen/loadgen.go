package loadgen

import (
	"context"
	"fmt"
	"github.com/facebookgo/httpdown"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"
)

type (
	LoadExporter interface {
		AddTarget(port int) error
		Stop() (int, error)
	}

	MetricsGenerator interface {
		prometheus.Collector
		Sum() int
	}

	LoadExporterInternal struct {
		ctx        context.Context
		sdcfgdir   string
		cancel     func()
		sumchan    chan int
		totalchan  chan int
		err        error
		genbuilder func() MetricsGenerator
		wg         sync.WaitGroup
	}
)

func getSdFileContents(targetAddr string) string {
	return fmt.Sprintf(`[
  {
    "targets": [ "%s" ],
    "labels": {
    }
  }
]`, targetAddr)
}

func writeSdConfigFile(targetAddr, filename string) error {
	sdcontents := getSdFileContents(targetAddr)
	err := ioutil.WriteFile(filename, []byte(sdcontents), 0600)
	if err != nil {
		return fmt.Errorf("unable to write sd_config file '%s': %v", filename, err)
	}
	return nil
}

func NewLoadExporterInternal(ctx context.Context, sdcfgdir string, genbuilder func() MetricsGenerator) *LoadExporterInternal {
	lctx, cancel := context.WithCancel(ctx)
	lei := &LoadExporterInternal{
		ctx:        lctx,
		sdcfgdir:   sdcfgdir,
		cancel:     cancel,
		genbuilder: genbuilder,
		sumchan:    make(chan int),
		totalchan:  make(chan int),
	}
	go func() {
		var sum int
		for s := range lei.sumchan {
			sum += s
		}
		lei.totalchan <- sum
	}()
	return lei
}

func (lei *LoadExporterInternal) Stop() (int, error) {
	lei.cancel()
	lei.wg.Wait()
	close(lei.sumchan)
	return <-lei.totalchan, nil
}

func (lei *LoadExporterInternal) AddTarget(port int) error {
	targetAddr := fmt.Sprintf("localhost:%d", port)
	cfgfilename := filepath.Join(lei.sdcfgdir, fmt.Sprintf("load-%d.json", port))
	if err := writeSdConfigFile(targetAddr, cfgfilename); err != nil {

		return fmt.Errorf("unable to add target: %v", err)
	}

	go lei.start(targetAddr)

	return nil
}

func (lei *LoadExporterInternal) start(addr string) error {
	reg := prometheus.NewRegistry()
	gen := lei.genbuilder()
	reg.MustRegister(gen)
	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	server := &http.Server{Addr: addr, Handler: handler}
	hd := &httpdown.HTTP{
		StopTimeout: 10 * time.Second,
		KillTimeout: 1 * time.Second,
	}
	dserver, err := hd.ListenAndServe(server)
	if err != nil {
		return fmt.Errorf("unable to setup HTTP server: %v", err)
	}

	lei.wg.Add(1)

	go func() {
		done := lei.ctx.Done()
		<-done
		err := dserver.Stop()
		if err != nil {
			log.Printf("error stopping HTTP server: %v", err)
		}
		sum := gen.Sum()
		lei.sumchan <- sum
		lei.wg.Done()
	}()

	return nil
}
