package loadgen

import (
	"bytes"
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
	InstanceSum struct {
		Instance string
		Sum      int
	}

	LoadExporter interface {
		AddTarget(port int, job string, exporter Exporter) error
		Stop() ([]InstanceSum, error)
	}

	MetricsGenerator interface {
		prometheus.Collector
		Sum() (int, error)
	}

	Exporter interface {
		Sum() (int, error)
	}

	HttpExporter interface {
		http.Handler
		Exporter
	}

	httpExporter struct {
		http.Handler
		MetricsGenerator
	}

	LoadExporterInternal struct {
		ctx       context.Context
		sdcfgdir  string
		cancel    func()
		sumchan   chan InstanceSum
		totalchan chan []InstanceSum
		err       error
		wg        sync.WaitGroup
	}
)

func getSdFileContents(targetAddr, job string) string {
	return fmt.Sprintf(`[
  {
    "targets": [ "%s" ],
    "labels": {
	    "job": "%s"
    }
  }
]`, targetAddr, job)
}

func writeSdConfigFile(targetAddr, job, filename string) error {
	sdcontents := getSdFileContents(targetAddr, job)
	err := ioutil.WriteFile(filename, []byte(sdcontents), 0600)
	if err != nil {
		return fmt.Errorf("unable to write sd_config file '%s': %v", filename, err)
	}
	return nil
}

func NewHttpExporter(mg MetricsGenerator) HttpExporter {
	reg := prometheus.NewRegistry()
	reg.MustRegister(mg)
	return httpExporter{promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), mg}
}

func NewLoadExporterInternal(ctx context.Context, sdcfgdir string) *LoadExporterInternal {
	lctx, cancel := context.WithCancel(ctx)
	lei := &LoadExporterInternal{
		ctx:       lctx,
		sdcfgdir:  sdcfgdir,
		cancel:    cancel,
		sumchan:   make(chan InstanceSum),
		totalchan: make(chan []InstanceSum),
	}
	go func() {
		var sums []InstanceSum
		for s := range lei.sumchan {
			sums = append(sums, s)
		}
		lei.totalchan <- sums
	}()
	return lei
}

func (lei *LoadExporterInternal) Stop() ([]InstanceSum, error) {
	lei.cancel()
	lei.wg.Wait()
	close(lei.sumchan)
	return <-lei.totalchan, nil
}

func (lei *LoadExporterInternal) AddTarget(port int, job string, exporter Exporter) error {
	var hexporter HttpExporter
	if he, ok := exporter.(HttpExporter); ok {
		hexporter = he
	} else {
		return fmt.Errorf("LoadExporterInternal requires an HttpExporter, got %v", exporter)
	}
	targetAddr := fmt.Sprintf("localhost:%d", port)
	cfgfilename := filepath.Join(lei.sdcfgdir, fmt.Sprintf("load-%d.json", port))
	if err := writeSdConfigFile(targetAddr, job, cfgfilename); err != nil {

		return fmt.Errorf("unable to add target: %v", err)
	}

	go lei.start(targetAddr, hexporter)

	return nil
}

type (
	dummyResponseWriter struct {
		bytes.Buffer
		header http.Header
		code   int
		sum    int
	}
)

func (d *dummyResponseWriter) Header() http.Header {
	return d.header
}

func (d *dummyResponseWriter) WriteHeader(code int) {
	d.code = code
}

func newDummyResponseWriter() *dummyResponseWriter {
	return &dummyResponseWriter{header: make(http.Header)}
}

type replayHandler struct {
	dwrs     [2]*dummyResponseWriter
	mtx      sync.Mutex
	replays  int
	sum      int
	exporter HttpExporter
}

func NewReplayHandler(e HttpExporter) *replayHandler {
	return &replayHandler{exporter: e}
}

// ServeHTTP implements http.Handler.
func (rh *replayHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	rh.mtx.Lock()
	idx := rh.replays % 2
	rh.replays++
	if rh.dwrs[idx] == nil {
		rh.dwrs[idx] = newDummyResponseWriter()
		rh.exporter.ServeHTTP(rh.dwrs[idx], req)
		sum, err := rh.exporter.Sum()
		if err != nil {
			log.Fatalf("Error fetching exporter sum: %v", err)
		} else {
			rh.dwrs[idx].sum += sum
		}
		if idx > 0 {
			rh.dwrs[idx].sum -= rh.dwrs[idx-1].sum
		}
	}
	rh.mtx.Unlock()

	header := w.Header()
	for k, v := range rh.dwrs[idx].header {
		header[k] = v
	}

	// w.WriteHeader(dwr.code)
	w.Write(rh.dwrs[idx].Bytes())
	rh.mtx.Lock()
	rh.sum += rh.dwrs[idx].sum
	rh.mtx.Unlock()
}

func (rh *replayHandler) Sum() (int, error) {
	return rh.sum, nil
}

func (lei *LoadExporterInternal) start(addr string, exporter HttpExporter) error {
	server := &http.Server{Addr: addr, Handler: exporter}
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
		sum, err := exporter.Sum()
		if err != nil {
			log.Printf("error fetching exporter sum: %v", err)
		} else {
			lei.sumchan <- InstanceSum{addr, sum}
		}
		lei.wg.Done()
	}()

	return nil
}
