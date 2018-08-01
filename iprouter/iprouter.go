package iprouter

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

type IpRouter struct {
	routes []route
}

type route struct {
	net    *net.IPNet
	target *net.TCPAddr
}

var Default = &IpRouter{routes: []route{}}

// Example config:
//  172.16.0.0/12 -> 172.16.0.1, 0.0.0.0/0 -> 192.168.0.2
func New(config string) (*IpRouter, error) {
	config = strings.TrimSpace(config)
	if config == "" {
		return Default, nil
	}

	router := &IpRouter{}
	split0 := strings.Split(config, ",")
	router.routes = make([]route, len(split0))
	for i, str := range split0 {
		split := strings.SplitN(str, "->", 2)

		route, err := parseRoute(strings.TrimSpace(split[0]), strings.TrimSpace(split[1]))
		if err != nil {
			return nil, err
		}

		router.routes[i] = route
	}
	return router, nil
}

func (r *IpRouter) GetRoute(remote *net.TCPAddr) *net.TCPAddr {
	if remote == nil {
		return nil
	}
	for idx := range r.routes {
		if r.routes[idx].net.Contains(remote.IP) {
			return r.routes[idx].target
		}
	}
	return nil
}

func (r *IpRouter) String() string {
	str := ""
	for _, route := range r.routes {
		if str != "" {
			str += ", "
		}
		str += route.String()
	}
	return str
}

func (r route) String() string {
	var target string
	if r.target != nil {
		target = r.target.IP.String()
	} else {
		target = "auto"
	}
	return fmt.Sprint(r.net, " -> ", target)
}

func parseRoute(ipnet, target string) (route, error) {
	_, ipnet0, err := net.ParseCIDR(ipnet)
	if err != nil {
		return route{}, err
	}
	var target0 *net.TCPAddr
	if target == "auto" {
		target0 = nil
	} else {
		targetIp := net.ParseIP(target)
		if targetIp == nil {
			return route{}, errors.New("invalid target ip " + target)
		}
		target0 = &net.TCPAddr{
			IP:   targetIp,
			Port: 0,
		}
	}
	return route{
		net:    ipnet0,
		target: target0,
	}, nil
}
