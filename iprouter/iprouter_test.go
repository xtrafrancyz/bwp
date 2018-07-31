package iprouter

import (
	"net"
	"testing"
)

func TestRoutes(t *testing.T) {
	r, err := New("172.16.0.0/12 -> 172.16.1.1, " +
		"127.0.0.1/32 -> 127.0.0.1, " +
		"0.0.0.0/0 -> auto")
	if err != nil {
		t.Error("Could not parse route config", err)
	}
	checkTarget(t, r, "172.16.50.1", "172.16.1.1")
	checkTarget(t, r, "172.16.0.0", "172.16.1.1")
	checkTarget(t, r, "172.31.255.255", "172.16.1.1")
	checkTarget(t, r, "127.0.0.1", "127.0.0.1")
	checkTarget(t, r, "127.0.0.2", "auto")
	checkTarget(t, Default, "127.0.0.1", "auto")
	checkTarget(t, Default, "172.16.0.0", "auto")
}

func checkTarget(t *testing.T, r *IpRouter, addr, target string) {
	result := r.GetRoute(getTcpAddr(t, addr))
	var sresult string
	if result == nil {
		sresult = "auto"
	} else {
		sresult = result.IP.String()
	}
	if sresult != target {
		t.Errorf("Wrong target for %s - %s. Must be %s", addr, sresult, target)
	}
}

func getTcpAddr(t *testing.T, addr string) *net.TCPAddr {
	parsed, err := net.ResolveTCPAddr("tcp", addr+":0")
	if err != nil {
		t.Error(err)
	}
	return parsed
}
