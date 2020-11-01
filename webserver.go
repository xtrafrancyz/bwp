package main

import (
	"container/list"
	"fmt"
	"log"
	"net"
	"os"
	"runtime/debug"
	"time"

	"github.com/VictoriaMetrics/metrics"
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

var (
	requestsIn = metrics.NewCounter("requests_in")
)

func NewWebServer(pool *worker.Pool) *WebServer {
	ws := &WebServer{
		pool:      pool,
		listeners: list.New(),
	}

	r := router.New()
	r.PanicHandler = func(ctx *fasthttp.RequestCtx, val interface{}) {
		log.Println("panic:", val, "\n", string(debug.Stack()))
		ctx.Error("Internal Server Error", 500)
	}
	r.POST("/post/http", httpJob.WebHandler(pool))
	r.GET("/metrics", ws.handleMetrics)

	handler := func(ctx *fasthttp.RequestCtx) {
		requestsIn.Inc()
		r.Handler(ctx)
	}

	ws.server = &fasthttp.Server{
		Name:              "bwp",
		Handler:           handler,
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

func (ws *WebServer) handleMetrics(ctx *fasthttp.RequestCtx) {
	requestsIn.Dec()
	ctx.SetStatusCode(200)
	metrics.WritePrometheus(ctx, true)
}
