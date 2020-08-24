package http

import (
	"encoding/base64"
	"errors"

	"github.com/json-iterator/go"
	"github.com/valyala/fasthttp"
	"github.com/xtrafrancyz/bwp/worker"
)

type webHandler struct {
	pool *worker.Pool
}

func WebHandler(pool *worker.Pool) fasthttp.RequestHandler {
	return (&webHandler{pool: pool}).handlePostHttp
}

var (
	json = jsoniter.ConfigFastest
)

func (h *webHandler) handlePostHttp(ctx *fasthttp.RequestCtx) {
	body := ctx.Request.Body()
	if value := ctx.Request.Header.Peek(fasthttp.HeaderContentEncoding); value != nil {
		enc := string(value)
		var err error
		if enc == "gzip" {
			if body, err = ctx.Request.BodyGunzip(); err != nil {
				ctx.Error(err.Error(), 400)
				return
			}
		} else if enc == "deflate" {
			if body, err = ctx.Request.BodyInflate(); err != nil {
				ctx.Error(err.Error(), 400)
				return
			}
		}
	}

	if body == nil || len(body) < 2 {
		ctx.Error("Invalid post body", 400)
		return
	}
	fc := body[0]
	if fc != '[' && fc != '{' {
		ctx.Error("Invalid json data", 400)
		return
	}

	iter := json.BorrowIterator(body)
	defer json.ReturnIterator(iter)
	if fc == '[' {
		jobs := make([]*requestData, 0, 4)
		for iter.ReadArray() {
			jobData, err := unmarshalRequestData(iter, true)
			if err != nil {
				ctx.Error(err.Error(), 400)
				return
			}
			jobs = append(jobs, jobData)
		}
		for _, data := range jobs {
			if err := h.submitJob(data); err != nil {
				ctx.Error(err.Error(), 503)
				return
			}
		}
	} else {
		jobData, err := unmarshalRequestData(iter, true)
		if err != nil {
			ctx.Error(err.Error(), 400)
			return
		}
		if err = h.submitJob(jobData); err != nil {
			ctx.Error(err.Error(), 503)
			return
		}
	}

	ctx.SetStatusCode(200)
	ctx.SetContentType("application/json")
	ctx.SetBodyString(`{"success":true}`)
}

func (h *webHandler) submitJob(data *requestData) error {
	if len(data.clones) > 0 {
		defer releaseRequestData(data)
		for _, c := range data.clones {
			// Copy parameters
			for k, v := range data.parameters {
				if _, ok := c.parameters[k]; !ok {
					c.parameters[k] = v
				}
			}

			// Copy headers
			for k, v := range data.headers {
				if _, ok := c.headers[k]; !ok {
					c.headers[k] = v
				}
			}

			if len(c.body) == 0 {
				c.body = data.body
			}

			if c.url == "" {
				c.url = data.url
			}

			if c.method == "" {
				c.method = data.method
			}

			if data.hostMetrics {
				c.hostMetrics = true
			}

			if err := h.pool.AddJob("http", c); err != nil {
				return err
			}
		}
		return nil
	}
	return h.pool.AddJob("http", data)
}

func unmarshalRequestData(iter *jsoniter.Iterator, root bool) (*requestData, error) {
	data := acquireRequestData()
	for field := iter.ReadObject(); field != ""; field = iter.ReadObject() {
		switch field {
		case "url":
			data.url = iter.ReadString()
		case "method":
			data.method = iter.ReadString()
		case "body":
			rawBody, err := base64.StdEncoding.DecodeString(iter.ReadString())
			if err != nil {
				return nil, errors.New("invalid request, body must be base64 encoded")
			}
			data.body = rawBody
		case "parameters":
			data.parameters = make(map[string]string)
			for name := iter.ReadObject(); name != ""; name = iter.ReadObject() {
				data.parameters[name] = iter.ReadString()
			}
		case "headers":
			data.headers = make(map[string]string)
			for name := iter.ReadObject(); name != ""; name = iter.ReadObject() {
				data.headers[name] = iter.ReadString()
			}
		case "hostMetrics":
			data.hostMetrics = iter.ReadBool()
		case "clones":
			if !root {
				return nil, errors.New("invalid request, clones can exists only on root request")
			}
			data.clones = make([]*requestData, 0, 4)
			for iter.ReadArray() {
				c, err := unmarshalRequestData(iter, false)
				if err != nil {
					return nil, err
				}
				data.clones = append(data.clones, c)
			}
		}
	}
	if data.url == "" && len(data.clones) == 0 {
		return nil, errors.New("invalid request, url is not set")
	}
	if data.method == "" && root {
		data.method = "GET"
	}
	return data, nil
}
