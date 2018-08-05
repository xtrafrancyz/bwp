package job

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valyala/fasthttp"
	"github.com/xtrafrancyz/bwp/iprouter"
	"github.com/xtrafrancyz/bwp/worker"
)

var ErrDialTimeout = errors.New("dialing to the given TCP address timed out")

const dialTimeout = 3 * time.Second
const defaultDNSCacheDuration = time.Minute

type HttpData struct {
	Url        string
	Method     string
	RawBody    []byte
	Parameters map[string]string
	Headers    map[string]string
}

type HttpJobHandler struct {
	router *iprouter.IpRouter
	client *fasthttp.Client

	tcpAddrsLock sync.Mutex
	tcpAddrsMap  map[string]*tcpAddrEntry
}

type tcpAddrEntry struct {
	addrs    []net.TCPAddr
	addrsIdx uint32

	resolveTime time.Time
	pending     bool
}

type dialResult struct {
	conn net.Conn
	err  error
}

func NewHttpJobHandler(router *iprouter.IpRouter) worker.JobHandler {
	h := &HttpJobHandler{
		router:      router,
		tcpAddrsMap: make(map[string]*tcpAddrEntry),
	}
	h.client = &fasthttp.Client{
		Name:         "bwp (https://github.com/xtrafrancyz/bwp)",
		Dial:         h.dial,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
	return h.Handle
}

func (h *HttpJobHandler) Handle(input interface{}) error {
	data := input.(*HttpData)

	// Put parameters directly to the url on GET or HEAD requests
	if (data.Method == "GET" || data.Method == "HEAD") && data.Parameters != nil && len(data.Parameters) != 0 {
		parsedUrl, err := url.Parse(data.Url)
		if err != nil {
			ReleaseHttpData(data)
			return err
		}
		values := parsedUrl.Query()
		for name, value := range data.Parameters {
			values.Set(name, value)
		}
		parsedUrl.RawQuery = values.Encode()
		data.Parameters = nil
		data.Url = parsedUrl.String()
	}

	log.Printf("JOB-HTTP: %s %s", data.Method, data.Url)

	var req fasthttp.Request
	req.Header.SetMethod(data.Method)
	req.SetRequestURI(data.Url)
	if data.Headers != nil {
		for name, value := range data.Headers {
			req.Header.Set(name, value)
		}
	}
	if data.RawBody != nil {
		req.SetBody(data.RawBody)
	} else if data.Parameters != nil {
		query := ""
		for name, value := range data.Parameters {
			if query != "" {
				query += "&"
			}
			query += name + "=" + url.QueryEscape(value)
		}
		req.SetBodyString(query)
	}

	var res fasthttp.Response
	err := h.client.DoTimeout(&req, &res, 10*time.Second)
	if err != nil {
		ReleaseHttpData(data)
		return err
	}

	if res.StatusCode() >= 400 {
		err = errors.New(fmt.Sprintf("Invalid status code %d from %s %s. Response:\n%s", res.StatusCode(), data.Method, data.Url, string(res.Body())))
	}

	ReleaseHttpData(data)
	return nil
}

// Dial function is copied from fasthttp/tcpdialer
func (h *HttpJobHandler) dial(addr string) (net.Conn, error) {
	addrs, idx, err := h.getTCPAddrs(addr)
	if err != nil {
		return nil, err
	}
	var conn net.Conn
	n := uint32(len(addrs))
	deadline := time.Now().Add(dialTimeout)
	for n > 0 {
		conn, err = h.tryDial("tcp", &addrs[idx%n], deadline)
		if err == nil {
			return conn, nil
		}
		if err == ErrDialTimeout {
			return nil, err
		}
		idx++
		n--
	}
	return nil, err
}

func (h *HttpJobHandler) tryDial(network string, addr *net.TCPAddr, deadline time.Time) (net.Conn, error) {
	timeout := -time.Since(deadline)
	if timeout <= 0 {
		return nil, ErrDialTimeout
	}

	ch := make(chan dialResult, 1)
	go func() {
		var dr dialResult
		dr.conn, dr.err = net.DialTCP(network, h.router.GetRoute(addr), addr)
		ch <- dr
	}()

	var (
		conn net.Conn
		err  error
	)

	tc := time.NewTimer(timeout)
	select {
	case dr := <-ch:
		conn = dr.conn
		err = dr.err
	case <-tc.C:
		err = ErrDialTimeout
	}

	return conn, err
}

func (h *HttpJobHandler) getTCPAddrs(addr string) ([]net.TCPAddr, uint32, error) {
	h.tcpAddrsLock.Lock()
	e := h.tcpAddrsMap[addr]
	if e != nil && !e.pending && time.Since(e.resolveTime) > defaultDNSCacheDuration {
		e.pending = true
		e = nil
	}
	h.tcpAddrsLock.Unlock()

	if e == nil {
		addrs, err := h.resolveTCPAddrs(addr, false)
		if err != nil {
			h.tcpAddrsLock.Lock()
			e = h.tcpAddrsMap[addr]
			if e != nil && e.pending {
				e.pending = false
			}
			h.tcpAddrsLock.Unlock()
			return nil, 0, err
		}

		e = &tcpAddrEntry{
			addrs:       addrs,
			resolveTime: time.Now(),
		}

		h.tcpAddrsLock.Lock()
		h.tcpAddrsMap[addr] = e
		h.tcpAddrsLock.Unlock()
	}

	idx := atomic.AddUint32(&e.addrsIdx, 1)
	return e.addrs, idx, nil
}

func (h *HttpJobHandler) resolveTCPAddrs(addr string, dualStack bool) ([]net.TCPAddr, error) {
	host, portS, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portS)
	if err != nil {
		return nil, err
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}

	n := len(ips)
	addrs := make([]net.TCPAddr, 0, n)
	for i := 0; i < n; i++ {
		ip := ips[i]
		if !dualStack && ip.To4() == nil {
			continue
		}
		addrs = append(addrs, net.TCPAddr{
			IP:   ip,
			Port: port,
		})
	}
	if len(addrs) == 0 {
		return nil, errors.New("couldn't find DNS entries for the given domain. Try using DialDualStack")
	}
	return addrs, nil
}

var httpDataPool sync.Pool

func AcquireHttpData() *HttpData {
	v := httpDataPool.Get()
	if v == nil {
		v = &HttpData{}
	}
	return v.(*HttpData)
}

func ReleaseHttpData(v *HttpData) {
	v.Url = ""
	v.Headers = nil
	v.Method = ""
	v.Parameters = nil
	v.RawBody = nil
	httpDataPool.Put(v)
}
