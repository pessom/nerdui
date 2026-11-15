// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package prometheus reads container metrics history from a Prometheus server that scrapes
// containerd's built-in metrics (#176, ADR-0023). It is a small read-only client of the
// Prometheus HTTP API: queries are built from fixed templates, never taken from callers.
package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
)

// Authentication modes of the Prometheus server.
const (
	AuthNone   = "none"
	AuthBasic  = "basic"
	AuthBearer = "bearer"
)

// Config is the connection to a Prometheus server.
type Config struct {
	// URL is the base URL of the server, e.g. http://127.0.0.1:9090 or
	// https://example.org/prometheus (without /api/v1).
	URL string `json:"url"`
	// Auth is AuthNone, AuthBasic or AuthBearer.
	Auth     string `json:"auth"`
	Username string `json:"username,omitempty"`
	// Secret is the password (basic) or the token (bearer). It is never returned by the API.
	Secret string `json:"secret,omitempty"`
}

// Validate checks the configuration and normalizes the URL (no trailing slash).
func (c *Config) Validate() error {
	u, err := url.Parse(strings.TrimSpace(c.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: url must be an http(s) URL of the Prometheus server", errdefs.ErrInvalidArgument)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%w: url must not contain credentials, a query or a fragment", errdefs.ErrInvalidArgument)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	c.URL = u.String()
	switch c.Auth {
	case "", AuthNone:
		c.Auth, c.Username, c.Secret = AuthNone, "", ""
	case AuthBasic:
		if c.Username == "" {
			return fmt.Errorf("%w: username is required for basic authentication", errdefs.ErrInvalidArgument)
		}
	case AuthBearer:
		c.Username = ""
	default:
		return fmt.Errorf("%w: auth must be none, basic or bearer", errdefs.ErrInvalidArgument)
	}
	if c.Auth != AuthNone && c.Secret == "" {
		return fmt.Errorf("%w: a password or token is required for %s authentication", errdefs.ErrInvalidArgument, c.Auth)
	}
	return nil
}

const (
	// requestTimeout bounds one call to Prometheus.
	requestTimeout = 20 * time.Second
	// maxResponse caps a response body; history queries return a few hundred points.
	maxResponse = 8 << 20
)

// Client queries one Prometheus server.
type Client struct {
	cfg  Config
	http *http.Client
}

// New returns a client for cfg (validated by the caller). A nil hc uses a client with a
// request timeout.
func New(cfg Config, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: requestTimeout}
	}
	return &Client{cfg: cfg, http: hc}
}

// Sample is one point of a series: unix seconds and value.
type Sample struct {
	T float64
	V float64
}

// Series is one result series of a query.
type Series struct {
	Labels  map[string]string
	Samples []Sample
}

// QueryRange runs a range query (GET /api/v1/query_range) and returns a matrix.
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) ([]Series, error) {
	q := url.Values{
		"query": {query},
		"start": {strconv.FormatInt(start.Unix(), 10)},
		"end":   {strconv.FormatInt(end.Unix(), 10)},
		"step":  {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)},
	}
	var data struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
		} `json:"result"`
	}
	if err := c.get(ctx, "/api/v1/query_range", q, &data); err != nil {
		return nil, err
	}
	if data.ResultType != "matrix" {
		return nil, fmt.Errorf("%w: prometheus returned %q instead of a matrix", errdefs.ErrUnavailable, data.ResultType)
	}
	out := make([]Series, 0, len(data.Result))
	for _, r := range data.Result {
		s := Series{Labels: r.Metric, Samples: make([]Sample, 0, len(r.Values))}
		for _, v := range r.Values {
			p, ok := parseSample(v)
			if ok {
				s.Samples = append(s.Samples, p)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// Query runs an instant query (GET /api/v1/query) and returns the values of a vector.
func (c *Client) Query(ctx context.Context, query string) ([]float64, error) {
	var data struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value [2]any `json:"value"`
		} `json:"result"`
	}
	if err := c.get(ctx, "/api/v1/query", url.Values{"query": {query}}, &data); err != nil {
		return nil, err
	}
	if data.ResultType != "vector" {
		return nil, fmt.Errorf("%w: prometheus returned %q instead of a vector", errdefs.ErrUnavailable, data.ResultType)
	}
	out := make([]float64, 0, len(data.Result))
	for _, r := range data.Result {
		if p, ok := parseSample(r.Value); ok {
			out = append(out, p.V)
		}
	}
	return out, nil
}

// Version returns the server version from /api/v1/status/buildinfo ("" when unavailable,
// e.g. on Prometheus-compatible servers that do not implement it).
func (c *Client) Version(ctx context.Context) string {
	var data struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, "/api/v1/status/buildinfo", nil, &data); err != nil {
		return ""
	}
	return data.Version
}

func parseSample(v [2]any) (Sample, bool) {
	t, ok := v[0].(float64)
	if !ok {
		return Sample{}, false
	}
	s, ok := v[1].(string)
	if !ok {
		return Sample{}, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return Sample{}, false
	}
	return Sample{T: t, V: f}, true
}

// get calls an API endpoint and decodes the "data" field of a successful response.
func (c *Client) get(ctx context.Context, path string, q url.Values, data any) error {
	u := c.cfg.URL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", errdefs.ErrInvalidArgument, err)
	}
	req.Header.Set("Accept", "application/json")
	switch c.cfg.Auth {
	case AuthBasic:
		req.SetBasicAuth(c.cfg.Username, c.cfg.Secret)
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+c.cfg.Secret)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// The URL is the admin's own setting; the error names the host but no credentials.
		return fmt.Errorf("%w: cannot reach prometheus: %w", errdefs.ErrUnavailable, unwrapURLError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return fmt.Errorf("%w: read prometheus response: %w", errdefs.ErrUnavailable, err)
	}
	if len(body) > maxResponse {
		return fmt.Errorf("%w: prometheus response is larger than %d MiB", errdefs.ErrUnavailable, maxResponse>>20)
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: prometheus rejected the credentials (HTTP %d)", errdefs.ErrUnavailable, resp.StatusCode)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s not found on the server: is the URL the Prometheus base URL?", errdefs.ErrUnavailable, path)
	}
	var env struct {
		Status    string          `json:"status"`
		Data      json.RawMessage `json:"data"`
		ErrorType string          `json:"errorType"`
		Error     string          `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("%w: not a Prometheus API response (HTTP %d)", errdefs.ErrUnavailable, resp.StatusCode)
	}
	if env.Status != "success" {
		msg := env.Error
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("%w: prometheus: %s", errdefs.ErrUnavailable, msg)
	}
	if err := json.Unmarshal(env.Data, data); err != nil {
		return fmt.Errorf("%w: decode prometheus data: %w", errdefs.ErrUnavailable, err)
	}
	return nil
}

// unwrapURLError drops the *url.Error wrapper, which repeats the full request URL.
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
