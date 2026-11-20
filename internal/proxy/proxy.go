// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package proxy applies X-Forwarded-For, X-Forwarded-Proto and X-Forwarded-Host from trusted
// reverse proxies (#112, ADR-0035). Headers from any other address are ignored: a client could
// otherwise pick its own IP for rate limiting and logs.
package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParsePrefixes parses CIDRs and single addresses ("10.0.0.0/8", "127.0.0.1", "::1").
func ParsePrefixes(list []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(list))
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not an IP address or CIDR", s)
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}

type ctxKey struct{}

// Secure reports whether the client reached nerdui over HTTPS: directly, or through a trusted
// proxy that says so with X-Forwarded-Proto.
func Secure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	v, _ := r.Context().Value(ctxKey{}).(bool)
	return v
}

func addrOf(hostport string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

func trustedAddr(trusted []netip.Prefix, a netip.Addr) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// lastValue returns the last comma-separated value of all instances of a header: the one the
// nearest proxy added.
func lastValue(h http.Header, name string) string {
	vals := h.Values(name)
	if len(vals) == 0 {
		return ""
	}
	parts := strings.Split(vals[len(vals)-1], ",")
	return strings.TrimSpace(parts[len(parts)-1])
}

// Middleware rewrites RemoteAddr, Host and the scheme of requests that come from a trusted
// proxy. The client is the rightmost X-Forwarded-For address that is not itself a trusted
// proxy, so a chain of proxies works and a forged leftmost entry is ignored. With no trusted
// proxies the middleware does nothing.
func Middleware(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(trusted) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer, ok := addrOf(r.RemoteAddr)
			if !ok || !trustedAddr(trusted, peer) {
				next.ServeHTTP(w, r)
				return
			}
			r = r.Clone(r.Context())
			var hops []string
			for _, v := range r.Header.Values("X-Forwarded-For") {
				hops = append(hops, strings.Split(v, ",")...)
			}
			for i := len(hops) - 1; i >= 0; i-- {
				a, ok := addrOf(strings.TrimSpace(hops[i]))
				if !ok {
					break // garbage: keep the last valid hop
				}
				r.RemoteAddr = net.JoinHostPort(a.String(), "0")
				if !trustedAddr(trusted, a) {
					break
				}
			}
			if h := lastValue(r.Header, "X-Forwarded-Host"); h != "" && !strings.ContainsAny(h, " /\\@") {
				r.Host = h
			}
			if strings.EqualFold(lastValue(r.Header, "X-Forwarded-Proto"), "https") {
				r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, true))
			}
			next.ServeHTTP(w, r)
		})
	}
}
