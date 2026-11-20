// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package tlsutil

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/acme"
)

func TestACMEValidation(t *testing.T) {
	for _, d := range []string{"nerdui.example.com", "A.B.example.org"} {
		if err := ValidACMEDomain(d); err != nil {
			t.Errorf("%s: %v", d, err)
		}
	}
	for _, d := range []string{"localhost", "10.0.0.1", "*.example.com", "-x.example.com", "example"} {
		if ValidACMEDomain(d) == nil {
			t.Errorf("%s accepted", d)
		}
	}
	for ca, want := range map[string]string{"": LetsEncryptProduction, "staging": LetsEncryptStaging, "https://ca.lan/dir": "https://ca.lan/dir"} {
		if got, err := ACMEDirectory(ca); err != nil || got != want {
			t.Errorf("%q: %s %v", ca, got, err)
		}
	}
	if _, err := ACMEDirectory("http://ca.lan/dir"); err == nil {
		t.Error("plain HTTP CA accepted")
	}
}

// Other names, and failures to obtain a certificate, get the self-signed one.
func TestACMEFallback(t *testing.T) {
	fallback, err := SelfSigned(t.TempDir(), []string{"localhost"})
	if err != nil {
		t.Fatal(err)
	}
	ca := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"type":"urn:ietf:params:acme:error:serverInternal","detail":"down"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(ca.Close)
	a := NewACME(fallback, ACMEOptions{
		Domains: []string{"Nerdui.Example.com"}, CacheDir: t.TempDir(),
		Client: &acme.Client{DirectoryURL: ca.URL, HTTPClient: ca.Client()},
	})
	cfg := a.TLSConfig()
	self, _ := fallback.current()

	got, err := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: "localhost"})
	if err != nil || got != self {
		t.Errorf("other name: %v", err)
	}
	got, err = cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: "nerdui.example.com."})
	if err != nil || got != self {
		t.Errorf("failed issuance: %v", err)
	}
	st := a.Status()
	if st.Ready || st.Error == "" || st.ErrorAt == nil || len(st.Domains) != 1 || st.Domains[0] != "nerdui.example.com" {
		t.Errorf("status %+v", st)
	}
	// A TLS-ALPN-01 challenge is never answered with the fallback.
	if _, err := cfg.GetCertificate(&tls.ClientHelloInfo{ServerName: "nerdui.example.com", SupportedProtos: []string{acme.ALPNProto}}); err == nil {
		t.Error("challenge without a pending authorization succeeded")
	}
	if !strings.Contains(strings.Join(cfg.NextProtos, ","), acme.ALPNProto) {
		t.Errorf("next protos %v", cfg.NextProtos)
	}
}

func TestACMEHTTPHandler(t *testing.T) {
	fallback, err := SelfSigned(t.TempDir(), []string{"localhost"})
	if err != nil {
		t.Fatal(err)
	}
	a := NewACME(fallback, ACMEOptions{Domains: []string{"nerdui.example.com"}, CacheDir: t.TempDir()})
	h := a.HTTPHandler("9443")
	for host, want := range map[string]string{
		"nerdui.example.com":    "https://nerdui.example.com:9443/x?y=1",
		"nerdui.example.com:80": "https://nerdui.example.com:9443/x?y=1",
		"evil.example.com":      "",
	} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "http://"+host+"/x?y=1", nil)
		h.ServeHTTP(rec, r)
		if got := rec.Header().Get("Location"); got != want {
			t.Errorf("%s: location %q, want %q (%d)", host, got, want, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	a.HTTPHandler("443").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://nerdui.example.com/", nil))
	if rec.Header().Get("Location") != "https://nerdui.example.com/" {
		t.Errorf("port 443: %q", rec.Header().Get("Location"))
	}
}
