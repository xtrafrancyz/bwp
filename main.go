package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/json-iterator/go"
	"github.com/vharitonsky/iniflags"
	"github.com/xtrafrancyz/bwp/iprouter"
	"github.com/xtrafrancyz/bwp/job"
	"github.com/xtrafrancyz/bwp/worker"
)

var json = jsoniter.ConfigFastest

func main() {
	listen := flag.String("listen", "127.0.0.1:7012", "address to bind web server")
	poolSize := flag.Int("pool-size", 50, "number of workers")
	poolQueueSize := flag.Int("pool-queue-size", 10000, "max number of queued jobs")
	ipRoutes := flag.String("ip-routes", "", "custom ip routing (example: 172.16.0.0/12 -> 172.16.1.1, 0.0.0.0/0 -> auto)")

	iniflags.Parse()

	ipRouter, err := iprouter.New(*ipRoutes)
	if err != nil {
		println(err.Error())
		return
	}
	if ipRouter != iprouter.Default {
		log.Println("Using routes:", ipRouter)
	}

	pool := &worker.Pool{
		Size:      *poolSize,
		QueueSize: *poolQueueSize,
	}
	pool.Init()
	pool.RegisterAction("http", job.NewHttpJobHandler(ipRouter))
	pool.RegisterAction("sleep", job.HandleSleep)
	pool.Start()

	ws := NewWebServer(pool)

	for _, host := range strings.Split(*listen, ",") {
		go func(host string) {
			var err error
			if host[0] == '/' {
				log.Printf("Listening on http://unix:%s", host)
				err = ws.server.ListenAndServeUNIX(host, 0777)
			} else {
				log.Printf("Listening on http://%s", host)
				err = ws.server.ListenAndServe(host)
			}
			if err != nil {
				log.Printf("Failed to bind listener on %s with %s", host, err.Error())
			}
		}(strings.TrimSpace(host))
	}

	waitForCtrlC()

	ws.ShuttingDown = true
	log.Printf("Finishing all jobs... Press Ctrl+C again to forcibly exit")
	pool.Finish()

	log.Printf("Bye!")
}

func waitForCtrlC() {
	var endWaiter sync.WaitGroup
	endWaiter.Add(1)
	var signalChan chan os.Signal
	signalChan = make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan
		endWaiter.Done()
	}()
	endWaiter.Wait()
	signal.Stop(signalChan)
}
