package http

import (
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
	"github.com/xtrafrancyz/bwp/iprouter"
	"github.com/xtrafrancyz/bwp/worker"
)

type requestData struct {
	url        string
	method     string
	body       []byte
	parameters map[string]string
	headers    map[string]string
	clones     []*requestData
}

type jobHandler struct {
	router          *iprouter.IpRouter
	client          *fasthttp.Client
	log4xxResponses bool
}

func NewJobHandler(router *iprouter.IpRouter, log4xxResponses bool) worker.JobHandler {
	h := &jobHandler{
		router:          router,
		log4xxResponses: log4xxResponses,
	}
	h.client = &fasthttp.Client{
		Name:                "bwp (github.com/xtrafrancyz/bwp)",
		Dial:                h.dialTcp,
		ReadTimeout:         10 * time.Second,
		WriteTimeout:        3 * time.Second,
		MaxResponseBodySize: 256 * 1024, // 256kb
	}
	return h.handle
}

func (h *jobHandler) handle(input interface{}) error {
	data := input.(*requestData)
	start := time.Now()

	// Put parameters directly to the url on GET or HEAD requests
	if (data.method == "GET" || data.method == "HEAD") && data.parameters != nil && len(data.parameters) != 0 {
		parsedUrl, err := url.Parse(data.url)
		if err != nil {
			releaseRequestData(data)
			return err
		}
		values := parsedUrl.Query()
		for name, value := range data.parameters {
			values.Set(name, value)
		}
		parsedUrl.RawQuery = values.Encode()
		data.parameters = nil
		data.url = parsedUrl.String()
	}

	req := fasthttp.AcquireRequest()
	res := fasthttp.AcquireResponse()
	req.Header.SetMethod(data.method)
	req.SetRequestURI(data.url)
	if data.headers != nil {
		for name, value := range data.headers {
			req.Header.Set(name, value)
		}
	}
	if data.body != nil {
		req.SetBody(data.body)
	} else if data.parameters != nil {
		query := ""
		for name, value := range data.parameters {
			if query != "" {
				query += "&"
			}
			query += name + "=" + url.QueryEscape(value)
		}
		req.SetBodyString(query)
	}
	if data.method == "HEAD" {
		res.SkipBody = true
	}

	err := h.client.DoTimeout(req, res, 10*time.Second)
	elapsed := time.Since(start).Round(100 * time.Microsecond)

	if err != nil {
		if err == fasthttp.ErrTimeout {
			log.Printf("http: %v %v %v timeout", elapsed, data.method, data.url)
		} else if err == fasthttp.ErrDialTimeout {
			log.Printf("http: %v %v %v dial timeout", elapsed, data.method, data.url)
		} else {
			log.Printf("http: %v %v %v error: %s", elapsed, data.method, data.url, err.Error())
		}
	} else if h.log4xxResponses && res.StatusCode() >= 400 && !res.SkipBody {
		logLength := len(res.Body())
		if logLength > 3000 {
			logLength = 3000
		}
		log.Printf("http: %v %v %v %v %v, Response:\n%s", elapsed, data.method, data.url, res.StatusCode(), len(res.Body()), res.Body()[0:logLength])
	} else {
		log.Printf("http: %v %v %v %v %v", elapsed, data.method, data.url, res.StatusCode(), len(res.Body()))
	}

	fasthttp.ReleaseRequest(req)
	fasthttp.ReleaseResponse(res)
	releaseRequestData(data)
	return nil
}

var requestDataPool sync.Pool

func acquireRequestData() *requestData {
	v := requestDataPool.Get()
	if v == nil {
		v = &requestData{}
	}
	return v.(*requestData)
}

func releaseRequestData(v *requestData) {
	v.url = ""
	v.headers = nil
	v.method = ""
	v.parameters = nil
	v.body = nil
	v.clones = nil
	requestDataPool.Put(v)
}
