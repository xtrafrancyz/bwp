package http

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/valyala/fasthttp"
)

const dialTimeout = 3 * time.Second
const defaultDNSCacheDuration = time.Minute

var (
	tcpAddrsLock sync.Mutex
	tcpAddrsMap  = make(map[string]*tcpAddrEntry)
)

type tcpAddrEntry struct {
	addrs    []net.TCPAddr
	addrsIdx uint32

	resolveTime time.Time
	pending     bool
}

// Dial function is copied from fasthttp/tcpdialer
func (h *jobHandler) dialTcp(addr string) (net.Conn, error) {
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
		if err == fasthttp.ErrDialTimeout {
			return nil, err
		}
		idx++
		n--
	}
	return nil, err
}

func (h *jobHandler) tryDial(network string, addr *net.TCPAddr, deadline time.Time) (net.Conn, error) {
	if -time.Since(deadline) <= 0 {
		return nil, fasthttp.ErrDialTimeout
	}

	dialer := net.Dialer{LocalAddr: h.router.GetRoute(addr)}
	ctx, cancelCtx := context.WithDeadline(context.Background(), deadline)
	defer cancelCtx()
	conn, err := dialer.DialContext(ctx, network, addr.String())
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return nil, fasthttp.ErrDialTimeout
	}

	return conn, err
}

func (h *jobHandler) getTCPAddrs(addr string) ([]net.TCPAddr, uint32, error) {
	tcpAddrsLock.Lock()
	e := tcpAddrsMap[addr]
	if e != nil && !e.pending && time.Since(e.resolveTime) > defaultDNSCacheDuration {
		e.pending = true
		e = nil
	}
	tcpAddrsLock.Unlock()

	if e == nil {
		addrs, err := h.resolveTCPAddrs(addr, false)
		if err != nil {
			tcpAddrsLock.Lock()
			e = tcpAddrsMap[addr]
			if e != nil && e.pending {
				e.pending = false
			}
			tcpAddrsLock.Unlock()
			return nil, 0, err
		}

		e = &tcpAddrEntry{
			addrs:       addrs,
			resolveTime: time.Now(),
		}

		tcpAddrsLock.Lock()
		tcpAddrsMap[addr] = e
		tcpAddrsLock.Unlock()
	}

	idx := atomic.AddUint32(&e.addrsIdx, 1)
	return e.addrs, idx, nil
}

func (h *jobHandler) resolveTCPAddrs(addr string, dualStack bool) ([]net.TCPAddr, error) {
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
