package main

import (
	"context"
	"flag"
	"github.com/prometheus/client_golang/api/prometheus"
	"github.com/prometheus/common/model"
	"log"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func main() {
	var (
		firstPort = flag.Int("first-port", 10000,
			"First port to assign to load exporters.")
		rmdata = flag.Bool("rmdata", false,
			"delete the data dir before starting Prometheus")
	)
	flag.Parse()
	log.Printf("will run exporters starting from port %d", *firstPort)

	promdir := "../../../../prometheus/prometheus/"
	// TODO data dir should be arg?
	setupDataDir("data", *rmdata)
	// setupPrometheusConfig(dir)
	mainctx := context.Background()
	stopPrometheus := startPrometheus(mainctx, promdir)
	sums := make(chan int)
	stopExporters := startExporters(mainctx, *firstPort, sums)
	time.Sleep(10 * time.Second)
	stopExporters()
	sum := <-sums
	log.Printf("sum=%d", sum)
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

func setupPrometheusConfig(promdir string) {
}

func startPrometheus(ctx context.Context, promdir string) context.CancelFunc {
	log.Print("starting prometheus")
	myctx, cancel := context.WithCancel(ctx)
	promcmd := exec.CommandContext(myctx, promdir+"prometheus")
	done := make(chan struct{})
	go func() {
		output, err := promcmd.CombinedOutput()
		log.Printf("Prometheus returned %v; output:\n%s", err, output)
		done <- struct{}{}
	}()
	return func() {
		log.Print("stopping prometheus")
		cancel()
		<-done
	}
}

func startExporters(ctx context.Context, firstport int, sum chan<- int) context.CancelFunc {
	log.Print("starting exporters")
	myctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(myctx, "../load_exporter/load_exporter")
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
	log.Printf("%v", result)
	vect := result.(model.Vector)
	return int(vect[0].Value)
}
