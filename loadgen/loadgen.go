package loadgen

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func StartExporter(sdCfgDir string, ctx context.Context, port int, sum chan<- int) context.CancelFunc {
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
