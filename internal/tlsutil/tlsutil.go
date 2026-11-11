// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package tlsutil provides the TLS certificate for nerdui's HTTPS server (ADR-0005):
// either a generated self-signed certificate kept in <data-dir>/tls, or a user-provided
// certificate/key pair that is reloaded when the files change.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// SelfSignedValidity is the lifetime of generated certificates (Apple platforms reject
	// longer-lived TLS server certificates).
	SelfSignedValidity = 825 * 24 * time.Hour
	// RenewBefore is how long before expiry a self-signed certificate is regenerated.
	RenewBefore = 30 * 24 * time.Hour
	// reloadInterval limits how often user-provided files are checked for changes.
	reloadInterval = time.Minute

	certFileName = "cert.pem"
	keyFileName  = "key.pem"
	dirMode      = fs.FileMode(0o700)
	keyMode      = fs.FileMode(0o600)
	certMode     = fs.FileMode(0o644)
)

// Info describes the certificate currently served.
type Info struct {
	SelfSigned  bool
	CertFile    string
	NotAfter    time.Time
	DNSNames    []string
	IPAddresses []net.IP
	// SHA256 is the colon-separated SHA-256 fingerprint as shown by browsers.
	SHA256 string
}

// Manager serves a certificate via tls.Config.GetCertificate and renews/reloads it.
type Manager struct {
	certFile, keyFile string
	selfSigned        bool
	hosts             []string
	now               func() time.Time
	logf              func(format string, args ...any)

	mu          sync.Mutex
	cert        *tls.Certificate
	leaf        *x509.Certificate
	modTime     time.Time
	lastChecked time.Time
}

// Option configures a Manager.
type Option func(*Manager)

// WithClock overrides the time source (tests).
func WithClock(now func() time.Time) Option { return func(m *Manager) { m.now = now } }

// WithLogf sets a logger for renewals and reloads.
func WithLogf(logf func(format string, args ...any)) Option {
	return func(m *Manager) { m.logf = logf }
}

// SelfSigned returns a manager for a self-signed certificate stored in dir (created 0700).
// The certificate is generated on first use and regenerated when it is invalid or expires
// within RenewBefore. hosts become the SANs (DNS names or IP addresses).
func SelfSigned(dir string, hosts []string, opts ...Option) (*Manager, error) {
	m := newManager(filepath.Join(dir, certFileName), filepath.Join(dir, keyFileName), true, opts)
	m.hosts = hosts
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("create TLS dir: %w", err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return nil, fmt.Errorf("chmod TLS dir: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil || m.expiring() {
		if err := m.generate(); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// FromFiles returns a manager for a user-provided certificate and key (PEM). The files are
// re-read when their modification time changes, e.g. after certbot renews them.
func FromFiles(certFile, keyFile string, opts ...Option) (*Manager, error) {
	m := newManager(certFile, keyFile, false, opts)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func newManager(certFile, keyFile string, selfSigned bool, opts []Option) *Manager {
	m := &Manager{
		certFile: certFile, keyFile: keyFile, selfSigned: selfSigned,
		now: time.Now, logf: func(string, ...any) {},
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// TLSConfig returns a server configuration that always serves the current certificate.
func (m *Manager) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return m.current() },
	}
}

// Info returns details about the current certificate.
func (m *Manager) Info() Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Info{
		SelfSigned:  m.selfSigned,
		CertFile:    m.certFile,
		NotAfter:    m.leaf.NotAfter,
		DNSNames:    m.leaf.DNSNames,
		IPAddresses: m.leaf.IPAddresses,
		SHA256:      Fingerprint(m.leaf),
	}
}

func (m *Manager) current() (*tls.Certificate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	if now.Sub(m.lastChecked) >= reloadInterval {
		m.lastChecked = now
		switch {
		case m.selfSigned && m.expiring():
			if err := m.generate(); err != nil {
				m.logf("TLS: renewing self-signed certificate failed, serving the old one: %v", err)
			}
		case !m.selfSigned:
			if info, err := os.Stat(m.certFile); err == nil && !info.ModTime().Equal(m.modTime) {
				if err := m.load(); err != nil {
					m.logf("TLS: reloading %s failed, serving the old certificate: %v", m.certFile, err)
				} else {
					m.logf("TLS: reloaded certificate %s (expires %s)", m.certFile, m.leaf.NotAfter.Format(time.DateOnly))
				}
			}
		}
	}
	return m.cert, nil
}

func (m *Manager) expiring() bool {
	return m.leaf == nil || m.now().Add(RenewBefore).After(m.leaf.NotAfter)
}

// load reads and validates the certificate/key pair. Caller holds m.mu.
func (m *Manager) load() error {
	info, err := os.Stat(m.certFile)
	if err != nil {
		return fmt.Errorf("TLS certificate: %w", err)
	}
	cert, err := tls.LoadX509KeyPair(m.certFile, m.keyFile)
	if err != nil {
		return fmt.Errorf("TLS certificate %s / key %s: %w", m.certFile, m.keyFile, err)
	}
	leaf := cert.Leaf
	if leaf == nil {
		if leaf, err = x509.ParseCertificate(cert.Certificate[0]); err != nil {
			return fmt.Errorf("parse TLS certificate: %w", err)
		}
	}
	m.cert, m.leaf, m.modTime = &cert, leaf, info.ModTime()
	return nil
}

// generate creates and stores a new self-signed certificate. Caller holds m.mu.
func (m *Manager) generate() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate serial: %w", err)
	}
	now := m.now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "nerdui", Organization: []string{"nerdui self-signed"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(SelfSignedValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range m.hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else if h != "" {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal key: %w", err)
	}
	if err := writeAtomic(m.keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), keyMode); err != nil {
		return err
	}
	if err := writeAtomic(m.certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), certMode); err != nil {
		return err
	}
	if err := m.load(); err != nil {
		return err
	}
	m.logf("TLS: generated self-signed certificate %s (expires %s, SHA-256 %s)",
		m.certFile, m.leaf.NotAfter.Format(time.DateOnly), Fingerprint(m.leaf))
	return nil
}

func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-"+filepath.Base(path))
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Fingerprint returns the colon-separated upper-case SHA-256 fingerprint of a certificate.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	h := strings.ToUpper(hex.EncodeToString(sum[:]))
	parts := make([]string, 0, len(sum))
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":")
}

// LocalHosts returns SANs for a self-signed certificate: "localhost", the hostname and all
// non-link-local unicast addresses of the machine.
func LocalHosts() []string {
	hosts := []string{"localhost"}
	if name, err := os.Hostname(); err == nil && name != "" && name != "localhost" {
		hosts = append(hosts, name)
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return append(hosts, "127.0.0.1", "::1")
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet.IP.IsLinkLocalUnicast() || ipNet.IP.IsMulticast() || ipNet.IP.IsUnspecified() {
			continue
		}
		hosts = append(hosts, ipNet.IP.String())
	}
	return hosts
}
