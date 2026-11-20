// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package tlsutil

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// Certificates from an ACME CA such as Let's Encrypt (#113, ADR-0038).

// ACME directory URLs of Let's Encrypt.
const (
	LetsEncryptProduction = acme.LetsEncryptURL
	LetsEncryptStaging    = "https://acme-staging-v02.api.letsencrypt.org/directory"
)

// ACMEDirectory resolves --tls-acme-ca: "production", "staging" or an https URL.
func ACMEDirectory(ca string) (string, error) {
	switch ca {
	case "", "production":
		return LetsEncryptProduction, nil
	case "staging":
		return LetsEncryptStaging, nil
	}
	if strings.HasPrefix(ca, "https://") && len(ca) > len("https://") {
		return ca, nil
	}
	return "", errors.New(`must be "production", "staging" or an https:// directory URL`)
}

var hostname = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// ValidACMEDomain reports whether d is a public DNS name a certificate can be issued for
// (no IP addresses, no wildcards: those need DNS-01).
func ValidACMEDomain(d string) error {
	d = strings.ToLower(strings.TrimSpace(d))
	switch {
	case net.ParseIP(d) != nil:
		return fmt.Errorf("%q: IP addresses cannot get ACME certificates", d)
	case strings.Contains(d, "*"):
		return fmt.Errorf("%q: wildcard certificates need DNS-01, which is not supported", d)
	case !hostname.MatchString(d):
		return fmt.Errorf("%q is not a fully qualified domain name", d)
	}
	return nil
}

// ACMEOptions configure ACME certificates.
type ACMEOptions struct {
	Domains      []string
	Email        string
	DirectoryURL string
	// CacheDir keeps the account key and certificates (<data-dir>/tls/acme).
	CacheDir string
	Logf     func(format string, args ...any)
	// Client overrides the ACME client (tests).
	Client *acme.Client
}

// ACMEStatus describes the ACME certificate for the UI. Keep in sync with api/openapi.yaml.
type ACMEStatus struct {
	Domains []string `json:"domains"`
	// Ready: a certificate was obtained and is served.
	Ready    bool       `json:"ready"`
	NotAfter *time.Time `json:"notAfter,omitempty"`
	// Error of the newest failed attempt while no certificate is ready; the self-signed
	// certificate is served meanwhile.
	Error   string     `json:"error,omitempty"`
	ErrorAt *time.Time `json:"errorAt,omitempty"`
}

// ACME serves certificates from an ACME CA for its domains and the fallback certificate for
// any other name (localhost, IP addresses, the health check) and while issuance fails.
type ACME struct {
	m        *autocert.Manager
	fallback *Manager
	domains  []string
	logf     func(format string, args ...any)

	mu      sync.Mutex
	status  ACMEStatus
	lastLog time.Time
}

// NewACME returns the ACME certificate source.
func NewACME(fallback *Manager, o ACMEOptions) *ACME {
	domains := make([]string, 0, len(o.Domains))
	for _, d := range o.Domains {
		domains = append(domains, strings.ToLower(strings.TrimSpace(d)))
	}
	client := o.Client
	if client == nil {
		client = &acme.Client{DirectoryURL: o.DirectoryURL}
	}
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &ACME{
		m: &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(o.CacheDir),
			HostPolicy: autocert.HostWhitelist(domains...),
			Email:      o.Email,
			Client:     client,
		},
		fallback: fallback,
		domains:  domains,
		logf:     logf,
		status:   ACMEStatus{Domains: domains},
	}
}

// TLSConfig serves ACME certificates and answers TLS-ALPN-01 challenges.
func (a *ACME) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		NextProtos:     []string{"http/1.1", acme.ALPNProto},
		GetCertificate: a.getCertificate,
	}
}

func isChallenge(hello *tls.ClientHelloInfo) bool {
	return slices.Contains(hello.SupportedProtos, acme.ALPNProto)
}

func (a *ACME) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name := strings.TrimSuffix(strings.ToLower(hello.ServerName), ".")
	if isChallenge(hello) {
		return a.m.GetCertificate(hello)
	}
	if !slices.Contains(a.domains, name) {
		return a.fallback.current()
	}
	cert, err := a.m.GetCertificate(hello)
	if err != nil {
		a.failed(err)
		return a.fallback.current()
	}
	a.obtained(cert)
	return cert, nil
}

func (a *ACME) obtained(cert *tls.Certificate) {
	leaf := cert.Leaf
	if leaf == nil && len(cert.Certificate) > 0 {
		leaf, _ = x509.ParseCertificate(cert.Certificate[0])
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	first := !a.status.Ready
	a.status.Ready, a.status.Error, a.status.ErrorAt = true, "", nil
	if leaf != nil {
		t := leaf.NotAfter
		a.status.NotAfter = &t
	}
	if first && leaf != nil {
		a.logf("TLS: ACME certificate for %s, expires %s", strings.Join(a.domains, ", "), leaf.NotAfter.Format(time.DateOnly))
	}
}

func (a *ACME) failed(err error) {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status.Ready {
		return // autocert keeps serving the valid certificate while renewal retries
	}
	a.status.Error, a.status.ErrorAt = err.Error(), &now
	if now.Sub(a.lastLog) >= 10*time.Minute {
		a.lastLog = now
		a.logf("TLS: obtaining an ACME certificate for %s failed, serving the self-signed one: %v",
			strings.Join(a.domains, ", "), err)
	}
}

// Status reports the ACME certificate.
func (a *ACME) Status() ACMEStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.status
	s.Domains = slices.Clone(a.domains)
	return s
}

// Warm obtains the certificates at start, so the first visitor does not wait and the UI knows
// the result, retrying every interval until ctx ends or all domains succeeded.
func (a *ACME) Warm(ctx context.Context, interval time.Duration) {
	pending := slices.Clone(a.domains)
	for len(pending) > 0 {
		var left []string
		for _, d := range pending {
			// autocert bounds each attempt itself (5 minutes).
			hello := &tls.ClientHelloInfo{ServerName: d, SupportedProtos: []string{"http/1.1"}}
			cert, err := a.m.GetCertificate(hello)
			if err != nil {
				a.failed(err)
				left = append(left, d)
				continue
			}
			a.obtained(cert)
		}
		pending = left
		if len(pending) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// HTTPHandler answers HTTP-01 challenges and redirects other requests to HTTPS on httpsPort.
func (a *ACME) HTTPHandler(httpsPort string) http.Handler {
	return a.m.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		i := slices.Index(a.domains, strings.ToLower(host))
		if i < 0 {
			http.Error(w, "use HTTPS", http.StatusBadRequest)
			return
		}
		// The target host is the configured domain, never the request's Host header.
		target := "https://" + a.domains[i]
		if httpsPort != "443" {
			target += ":" + httpsPort
		}
		path := "/" + strings.TrimLeft(r.URL.Path, "/")
		if r.URL.RawQuery != "" {
			path += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target+path, http.StatusMovedPermanently) //nolint:gosec // G710: host is a configured domain, path is local
	}))
}
