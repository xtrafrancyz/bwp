package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/VictoriaMetrics/metrics"
	"github.com/facebookarchive/grace/gracenet"
	"github.com/json-iterator/go"
	"github.com/vharitonsky/iniflags"
	"github.com/xtrafrancyz/bwp/iprouter"
	"github.com/xtrafrancyz/bwp/job"
	httpJob "github.com/xtrafrancyz/bwp/job/http"
	"github.com/xtrafrancyz/bwp/worker"
)

//noinspection GoUnusedGlobalVariable
var (
	json = jsoniter.ConfigFastest
	// Is the program started from the facebookgo/grace
	didInherit = os.Getenv("LISTEN_FDS") != ""

	pidfile = flag.String("pidfile", "", "path to pid file")
)

func main() {
	listen := flag.String("listen", "127.0.0.1:7012", "address to bind web server")
	poolSize := flag.Int("pool-size", 50, "number of workers")
	poolQueueSize := flag.Int("pool-queue-size", 10000, "max number of queued jobs")
	ipRoutes := flag.String("ip-routes", "", "custom ip routing (example: 172.16.0.0/12 -> 172.16.1.1, 0.0.0.0/0 -> auto)")
	log4xxResponses := flag.Bool("log4xxResponses", false, "log http responses with status code >= 400")

	iniflags.Parse()

	ipRouter, err := iprouter.New(*ipRoutes)
	if err != nil {
		println(err.Error())
		return
	}
	if ipRouter != iprouter.Default {
		log.Println("Using routes:", ipRouter)
	}

	if *pidfile != "" {
		err = writePidFile(*pidfile)
		if err != nil {
			log.Printf("Failed to write pidfile %s: %s", *pidfile, err)
		} else {
			log.Printf("Pidfile: %s", *pidfile)
		}
	}

	pool := &worker.Pool{
		Size:      *poolSize,
		QueueSize: *poolQueueSize,
	}
	pool.Init()
	pool.RegisterAction("http", httpJob.NewJobHandler(ipRouter, *log4xxResponses))
	pool.RegisterAction("sleep", job.HandleSleep)
	pool.Start()

	metrics.NewGauge(`queue_size`, func() float64 {
		return float64(pool.GetQueueLength())
	})
	metrics.NewGauge(`busy_workers`, func() float64 {
		return float64(pool.GetActiveWorkers())
	})

	ws := NewWebServer(pool)
	gnet := &gracenet.Net{}

	for _, host := range strings.Split(*listen, ",") {
		go func(host string) {
			err := ws.Listen(gnet, host)
			if err != nil {
				log.Printf("Failed to bind listener on %s with %s", host, err.Error())
			}
		}(strings.TrimSpace(host))
	}

	waitForSignals(ws, pool, gnet)
}

func waitForSignals(ws *WebServer, pool *worker.Pool, gnet *gracenet.Net) {
	stopChan := make(chan os.Signal, 2)
	reloadChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
	if runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "386") {
		signal.Notify(reloadChan, syscall.Signal(12)) // SIGUSR2
	}
	shutdown := false
	for {
		select {
		case <-stopChan:
			signal.Stop(reloadChan)
			if shutdown {
				return
			}
			shutdown = true
			go func() {
				ws.Finish()
				pool.Finish()
				log.Println("Bye!")
				if *pidfile != "" {
					_ = os.Remove(*pidfile)
				}
				os.Exit(0)
			}()
		case <-reloadChan:
			log.Println("Graceful restarting process")
			_, err := gnet.StartProcess()
			if err != nil {
				log.Printf("Could not start new process: %s", err.Error())
				continue
			}
			signal.Stop(stopChan)
			signal.Stop(reloadChan)
			pool.Finish()
			log.Println("Done! Old process is slowly dying...")
			return
		}
	}
}

func writePidFile(pidfile string) error {
	if err := os.MkdirAll(filepath.Dir(pidfile), os.FileMode(0755)); err != nil {
		return err
	}
	return ioutil.WriteFile(pidfile, []byte(fmt.Sprintf("%d", os.Getpid())), 0664)
}
