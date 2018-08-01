package main

import (
	"bytes"
	"container/list"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/json-iterator/go"
	"github.com/qiangxue/fasthttp-routing"
	"github.com/valyala/fasthttp"
	"github.com/xtrafrancyz/bwp/job"
	"github.com/xtrafrancyz/bwp/worker"
)

type WebServer struct {
	pool      *worker.Pool
	server    *fasthttp.Server
	listeners *list.List
}

type jobResponse struct {
	Success bool `json:"success"`
}

var (
	jobResponseSuccess = jobResponse{true}
	POST               = []byte("POST")
)

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

	router := routing.New()
	router.Post("/post/http", ws.handlePostHttp)
	router.Get("/status", ws.handleStatus)

	ws.server = &fasthttp.Server{
		Name:              "bwp",
		Handler:           router.HandleRequest,
		ReduceMemoryUsage: true,
		WriteTimeout:      10 * time.Second,
		ReadTimeout:       10 * time.Second,
	}

	return ws
}

func (ws *WebServer) Listen(host string) error {
	var err error
	var ln net.Listener
	if host[0] == '/' {
		log.Printf("Listening on http://unix:%s", host)
		if err = os.Remove(host); err != nil && !os.IsNotExist(err) {
			err = fmt.Errorf("unexpected error when trying to remove unix socket file %q: %s", host, err)
		}
		ln, err = net.Listen("unix", host)
		if err = os.Chmod(host, 0777); err != nil {
			err = fmt.Errorf("cannot chmod %#o for %q: %s", 0777, host, err)
		}
	} else {
		log.Printf("Listening on http://%s", host)
		ln, err = net.Listen("tcp4", host)
	}
	if err != nil {
		return err
	}
	ws.listeners.PushBack(ln)
	ws.server.Serve(ln)
	return nil
}

func (ws *WebServer) Finish() {
	for e := ws.listeners.Front(); e != nil; e = e.Next() {
		e.Value.(net.Listener).Close()
	}
}

func (ws *WebServer) handleStatus(c *routing.Context) error {
	c.SetStatusCode(200)
	c.SetContentType("application/json")
	body, _ := json.Marshal(statusResponse{
		QueueLimit:    ws.pool.QueueSize,
		Workers:       ws.pool.Size,
		ActiveWorkers: ws.pool.GetActiveWorkers(),
		JobsInQueue:   ws.pool.GetQueueLength(),
	})
	c.SetBody(body)
	return nil
}

func (ws *WebServer) handlePostHttp(c *routing.Context) error {
	if bytes.Equal(c.Method(), POST) {
		body := c.PostBody()
		if body == nil || len(body) < 2 {
			return simpleResponse(c, 400, "Invalid post body")
		}
		fc := body[0]
		if fc != '[' && fc != '{' {
			return simpleResponse(c, 400, "Invalid json data")
		}

		iter := json.BorrowIterator(body)
		if fc == '[' {
			jobs := acquireList()
			for iter.ReadArray() {
				jobData, err := unmarshalHttpJobData(iter)
				if err != nil {
					json.ReturnIterator(iter)
					return simpleResponse(c, 400, err.Error())
				}
				jobs.PushBack(jobData)
			}
			for e := jobs.Front(); e != nil; e = e.Next() {
				ws.pool.AddJob("http", e.Value.(*job.HttpData))
			}
			releaseList(jobs)
		} else {
			jobData, err := unmarshalHttpJobData(iter)
			if err != nil {
				json.ReturnIterator(iter)
				return simpleResponse(c, 400, err.Error())
			}
			ws.pool.AddJob("http", jobData)
		}
		json.ReturnIterator(iter)

		response, _ := json.Marshal(jobResponseSuccess)
		c.SetStatusCode(200)
		c.SetContentType("application/json")
		c.SetBody(response)
	}
	return nil
}

func unmarshalHttpJobData(iter *jsoniter.Iterator) (*job.HttpData, error) {
	jobData := job.AcquireHttpData()
	for field := iter.ReadObject(); field != ""; field = iter.ReadObject() {
		switch field {
		case "url":
			jobData.Url = iter.ReadString()
		case "method":
			jobData.Method = iter.ReadString()
		case "body":
			rawBody, err := base64.StdEncoding.DecodeString(iter.ReadString())
			if err != nil {
				job.ReleaseHttpData(jobData)
				return nil, errors.New("invalid request, body must be base64 encoded")
			}
			jobData.RawBody = rawBody
		case "parameters":
			jobData.Parameters = make(map[string]string)
			for name := iter.ReadObject(); name != ""; name = iter.ReadObject() {
				jobData.Parameters[name] = iter.ReadString()
			}
		case "headers":
			jobData.Headers = make(map[string]string)
			for name := iter.ReadObject(); name != ""; name = iter.ReadObject() {
				jobData.Headers[name] = iter.ReadString()
			}
		}
	}
	if jobData.Url == "" {
		job.ReleaseHttpData(jobData)
		return nil, errors.New("invalid request, url is not set")
	}
	if jobData.Method == "" {
		jobData.Method = "GET"
	}
	return jobData, nil
}

func simpleResponse(c *routing.Context, status int, body string) error {
	c.SetStatusCode(status)
	c.SetBodyString(body)
	return nil
}

var listPool sync.Pool

func acquireList() *list.List {
	v := listPool.Get()
	if v == nil {
		return list.New()
	}
	return v.(*list.List)
}

func releaseList(v *list.List) {
	v.Init()
	listPool.Put(v)
}
