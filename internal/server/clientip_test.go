package server

import (
	"net"
	"net/http"
	"strings"
	"testing"
)

func srvWithProxies(cidrs ...string) *Server {
	s := &Server{}
	for _, c := range cidrs {
		if !strings.Contains(c, "/") {
			c += "/32"
		}
		if _, n, err := net.ParseCIDR(c); err == nil {
			s.trustedProxies = append(s.trustedProxies, n)
		}
	}
	return s
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name    string
		proxies []string
		remote  string
		xff     string
		want    string
	}{
		{"no proxies: ignore XFF, use remote", nil, "203.0.113.5:5000", "1.2.3.4", "203.0.113.5"},
		{"untrusted peer: ignore forged XFF", []string{"10.0.10.248"}, "203.0.113.5:5000", "1.2.3.4", "203.0.113.5"},
		{"trusted proxy: use rightmost non-proxy hop", []string{"10.0.10.248"}, "10.0.10.248:5000", "9.9.9.9, 8.8.8.8", "8.8.8.8"},
		{"trusted proxy chain: skip trailing proxies", []string{"10.0.10.248", "10.0.10.1"}, "10.0.10.248:5000", "7.7.7.7, 10.0.10.1", "7.7.7.7"},
		{"trusted proxy, no XFF: use peer", []string{"10.0.10.248"}, "10.0.10.248:5000", "", "10.0.10.248"},
		{"forged XFF cannot escape throttle bucket", []string{"10.0.10.248"}, "203.0.113.9:5000", "10.0.10.248", "203.0.113.9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := srvWithProxies(c.proxies...)
			r := &http.Request{RemoteAddr: c.remote, Header: http.Header{}}
			if c.xff != "" {
				r.Header.Set("X-Forwarded-For", c.xff)
			}
			if got := s.clientIP(r); got != c.want {
				t.Fatalf("clientIP=%q want %q", got, c.want)
			}
		})
	}
}
