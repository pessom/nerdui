// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestMiddleware(t *testing.T) {
	trusted, err := ParsePrefixes([]string{"10.0.0.0/8", "127.0.0.1", " "})
	if err != nil || len(trusted) != 2 {
		t.Fatalf("%v %v", trusted, err)
	}
	if _, err := ParsePrefixes([]string{"example.com"}); err == nil {
		t.Error("hostname accepted")
	}
	type seen struct {
		remote, host string
		secure       bool
	}
	run := func(remote string, hdr map[string]string, trusted []netip.Prefix) seen {
		var s seen
		h := Middleware(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			s = seen{r.RemoteAddr, r.Host, Secure(r)}
		}))
		r := httptest.NewRequest(http.MethodGet, "http://nerdui.internal:9443/", nil)
		r.RemoteAddr = remote
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		h.ServeHTTP(httptest.NewRecorder(), r)
		return s
	}
	fwd := map[string]string{
		"X-Forwarded-For":   "6.6.6.6, 203.0.113.7, 10.1.1.1",
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "nerdui.example.com",
	}
	cases := []struct {
		name, remote string
		hdr          map[string]string
		trusted      []netip.Prefix
		want         seen
	}{
		{"trusted chain", "10.0.0.2:5000", fwd, trusted, seen{"203.0.113.7:0", "nerdui.example.com", true}},
		{"untrusted peer", "198.51.100.1:5000", fwd, trusted, seen{"198.51.100.1:5000", "nerdui.internal:9443", false}},
		{"no trusted proxies", "10.0.0.2:5000", fwd, nil, seen{"10.0.0.2:5000", "nerdui.internal:9443", false}},
		{"no headers", "127.0.0.1:5000", nil, trusted, seen{"127.0.0.1:5000", "nerdui.internal:9443", false}},
		{"bad host ignored", "127.0.0.1:5000", map[string]string{"X-Forwarded-Host": "evil.com/x", "X-Forwarded-Proto": "http"}, trusted, seen{"127.0.0.1:5000", "nerdui.internal:9443", false}},
		{"ipv4-mapped peer", "[::ffff:10.0.0.2]:5000", map[string]string{"X-Forwarded-For": "2001:db8::1"}, trusted, seen{"[2001:db8::1]:0", "nerdui.internal:9443", false}},
	}
	for _, c := range cases {
		if got := run(c.remote, c.hdr, c.trusted); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
