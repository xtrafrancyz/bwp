package main

import (
	"container/list"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/facebookarchive/grace/gracenet"
	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
	httpJob "github.com/xtrafrancyz/bwp/job/http"
	"github.com/xtrafrancyz/bwp/worker"
)

type WebServer struct {
	pool      *worker.Pool
	server    *fasthttp.Server
	listeners *list.List
}

type statusResponse struct {
	QueueLimit    int `json:"queueLimit"`
	Workers       int `json:"workers"`
	ActiveWorkers int `json:"activeWorkers"`
	JobsInQueue   int `json:"jobsInQueue"`
}

func NewWebServer(pool *worker.Pool) *WebServer {
	ws := &WebServer{
		pool:      pool,
		listeners: list.New(),
	}

	r := router.New()
	r.PanicHandler = func(ctx *fasthttp.RequestCtx, val interface{}) {
		log.Println("panic:", val)
		ctx.Error("Internal Server Error", 500)
	}
	r.POST("/post/http", httpJob.WebHandler(pool))
	r.GET("/status", ws.handleStatus)
	r.HandleMethodNotAllowed = true

	ws.server = &fasthttp.Server{
		Name:              "bwp",
		Handler:           r.Handler,
		ReduceMemoryUsage: true,
		WriteTimeout:      10 * time.Second,
		ReadTimeout:       10 * time.Second,
	}

	return ws
}

func (ws *WebServer) Listen(gnet *gracenet.Net, host string) error {
	var err error
	var ln net.Listener
	if host[0] == '/' {
		log.Printf("Listening on http://unix:%s", host)
		if err = os.Remove(host); err != nil && !os.IsNotExist(err) {
			err = fmt.Errorf("unexpected error when trying to remove unix socket file %q: %s", host, err)
		}
		ln, err = gnet.Listen("unix", host)
		if err = os.Chmod(host, 0777); err != nil {
			err = fmt.Errorf("cannot chmod %#o for %q: %s", 0777, host, err)
		}
	} else {
		log.Printf("Listening on http://%s", host)
		ln, err = gnet.Listen("tcp4", host)
	}
	if err != nil {
		return err
	}
	ws.listeners.PushBack(ln)
	return ws.server.Serve(ln)
}

func (ws *WebServer) Finish() {
	for e := ws.listeners.Front(); e != nil; e = e.Next() {
		_ = e.Value.(net.Listener).Close()
	}
}

func (ws *WebServer) handleStatus(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(200)
	ctx.SetContentType("application/json")
	body, _ := json.Marshal(statusResponse{
		QueueLimit:    ws.pool.QueueSize,
		Workers:       ws.pool.Size,
		ActiveWorkers: ws.pool.GetActiveWorkers(),
		JobsInQueue:   ws.pool.GetQueueLength(),
	})
	ctx.SetBody(body)
}
