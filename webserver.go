package main

import (
	"bytes"
	"encoding/base64"
	"time"

	"github.com/qiangxue/fasthttp-routing"
	"github.com/valyala/fasthttp"
	"github.com/xtrafrancyz/bwp/job"
	"github.com/xtrafrancyz/bwp/worker"
)

type WebServer struct {
	ShuttingDown bool
	pool         *worker.Pool
	server       *fasthttp.Server
}

type jobResponse struct {
	Success bool `json:"success"`
}

type statusResponse struct {
	QueueLimit    int `json:"queueLimit"`
	Workers       int `json:"workers"`
	ActiveWorkers int `json:"activeWorkers"`
	JobsInQueue   int `json:"jobsInQueue"`
}

func NewWebServer(pool *worker.Pool) *WebServer {
	ws := &WebServer{
		ShuttingDown: false,
		pool:         pool,
	}

	router := routing.New()
	router.Post("/post/http", ws.shutdownHandler, ws.handlePostHttp)
	router.Get("/status", ws.handleStatus)

	ws.server = &fasthttp.Server{
		Name:              "Background Worker Pool (https://github.com/xtrafrancyz/bwp)",
		Handler:           router.HandleRequest,
		ReduceMemoryUsage: true,
		WriteTimeout:      10 * time.Second,
		ReadTimeout:       10 * time.Second,
	}

	return ws
}

func (ws *WebServer) shutdownHandler(c *routing.Context) error {
	if ws.ShuttingDown {
		c.Abort()
		simpleResponse(c, 503, "Service is shutting down. Please try again later")
	}
	return nil
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
	if bytes.Equal(c.Method(), []byte("POST")) {
		var jobData job.HttpData
		iter := json.BorrowIterator(c.PostBody())
		for field := iter.ReadObject(); field != ""; field = iter.ReadObject() {
			switch field {
			case "url":
				jobData.Url = iter.ReadString()
			case "method":
				jobData.Method = iter.ReadString()
			case "body":
				rawBody, err := base64.StdEncoding.DecodeString(iter.ReadString())
				if err != nil {
					json.ReturnIterator(iter)
					return simpleResponse(c, 400, "Invalid request. Body must be base64 encoded")
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
		json.ReturnIterator(iter)

		if jobData.Method == "" {
			jobData.Method = "GET"
		}

		ws.pool.AddJob("http", jobData)

		body, _ := json.Marshal(jobResponse{
			Success: true,
		})
		c.SetStatusCode(200)
		c.SetContentType("application/json")
		c.SetBody(body)
	}
	return nil
}

func simpleResponse(c *routing.Context, status int, body string) error {
	c.SetStatusCode(status)
	c.SetBodyString(body)
	return nil
}
