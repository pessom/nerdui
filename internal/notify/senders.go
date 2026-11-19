// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type sender struct {
	validate func(Config) error
	send     func(ctx context.Context, c *http.Client, cfg Config, ev Event) error
}

var senders = map[string]sender{
	TypeWebhook:  {validateWebhook, sendWebhook},
	TypeSlack:    {validateURLOnly, sendSlack},
	TypeDiscord:  {validateURLOnly, sendDiscord},
	TypeTelegram: {validateTelegram, sendTelegram},
	TypeNtfy:     {validateURLOnly, sendNtfy},
	TypeGotify:   {validateGotify, sendGotify},
	TypeEmail:    {validateEmail, sendEmail},
}

// subject is the one-line summary of an event, with the host it comes from.
func subject(ev Event) string {
	s := oneLine(ev.Title)
	if ev.Host != "" {
		s = "[" + ev.Host + "] " + s
	}
	return s
}

// text is the plain-text form of an event.
func text(ev Event) string {
	var b strings.Builder
	b.WriteString(subject(ev))
	if ev.Message != "" {
		b.WriteString("\n" + ev.Message)
	}
	var where []string
	if ev.Environment != "" {
		where = append(where, "environment "+ev.Environment)
	}
	if ev.Namespace != "" {
		where = append(where, "namespace "+ev.Namespace)
	}
	if len(where) > 0 {
		b.WriteString("\n(" + strings.Join(where, ", ") + ")")
	}
	return b.String()
}

func checkURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("url must be an absolute http or https URL")
	}
	return u, nil
}

func validateURLOnly(c Config) error {
	_, err := checkURL(c.URL)
	return err
}

func validateWebhook(c Config) error { return validateURLOnly(c) }

func post(ctx context.Context, c *http.Client, target, contentType string, body []byte, header http.Header) error {
	u, err := checkURL(target)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "nerdui")
	resp, err := c.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			// Do not repeat the URL: Slack and Discord URLs hold their credentials.
			return fmt.Errorf("%s request failed: %w", u.Host, uerr.Err)
		}
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return errStatus(resp, b)
	}
	return nil
}

func postJSON(ctx context.Context, c *http.Client, target string, v any, header http.Header) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return post(ctx, c, target, "application/json", b, header)
}

// sendWebhook posts the event as JSON. With a secret, X-Nerdui-Signature is
// "sha256=" + hex(HMAC-SHA256(secret, body)).
func sendWebhook(ctx context.Context, c *http.Client, cfg Config, ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	h := http.Header{"X-Nerdui-Event": {ev.Kind}}
	if cfg.Secret != "" {
		m := hmac.New(sha256.New, []byte(cfg.Secret))
		m.Write(body)
		h.Set("X-Nerdui-Signature", "sha256="+hex.EncodeToString(m.Sum(nil)))
	}
	return post(ctx, c, cfg.URL, "application/json", body, h)
}

func sendSlack(ctx context.Context, c *http.Client, cfg Config, ev Event) error {
	return postJSON(ctx, c, cfg.URL, map[string]string{"text": text(ev)}, nil)
}

func sendDiscord(ctx context.Context, c *http.Client, cfg Config, ev Event) error {
	t := text(ev)
	if len(t) > 2000 {
		t = t[:1999] + "…"
	}
	return postJSON(ctx, c, cfg.URL, map[string]any{
		"content": t, "allowed_mentions": map[string]any{"parse": []string{}},
	}, nil)
}

var telegramToken = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)

// telegramAPI is the Bot API base URL; tests replace it.
var telegramAPI = "https://api.telegram.org"

func validateTelegram(c Config) error {
	if !telegramToken.MatchString(c.Token) {
		return errors.New("token must be a Telegram bot token (123456:ABC-...)")
	}
	if strings.TrimSpace(c.ChatID) == "" {
		return errors.New("chat ID is required")
	}
	return nil
}

func sendTelegram(ctx context.Context, c *http.Client, cfg Config, ev Event) error {
	if err := validateTelegram(cfg); err != nil {
		return err
	}
	err := postJSON(ctx, c, telegramAPI+"/bot"+cfg.Token+"/sendMessage", map[string]any{
		"chat_id": strings.TrimSpace(cfg.ChatID), "text": text(ev), "disable_web_page_preview": true,
	}, nil)
	if err != nil {
		// Never show the token, which is part of the URL.
		return errors.New(strings.ReplaceAll(err.Error(), cfg.Token, "<token>"))
	}
	return nil
}

func sendNtfy(ctx context.Context, c *http.Client, cfg Config, ev Event) error {
	h := http.Header{"Title": {mime.QEncoding.Encode("utf-8", subject(ev))}}
	switch ev.Severity {
	case SeverityError:
		h.Set("Priority", "high")
		h.Set("Tags", "rotating_light")
	case SeverityWarning:
		h.Set("Tags", "warning")
	}
	if cfg.Token != "" {
		h.Set("Authorization", "Bearer "+cfg.Token)
	}
	msg := ev.Message
	if msg == "" {
		msg = ev.Title
	}
	return post(ctx, c, cfg.URL, "text/plain; charset=utf-8", []byte(msg), h)
}

func validateGotify(c Config) error {
	if err := validateURLOnly(c); err != nil {
		return err
	}
	if c.Token == "" {
		return errors.New("application token is required")
	}
	return nil
}

func sendGotify(ctx context.Context, c *http.Client, cfg Config, ev Event) error {
	prio := 4
	switch ev.Severity {
	case SeverityError:
		prio = 8
	case SeverityWarning:
		prio = 6
	}
	target := strings.TrimRight(cfg.URL, "/") + "/message"
	return postJSON(ctx, c, target, map[string]any{
		"title": subject(ev), "message": text(ev), "priority": prio,
	}, http.Header{"X-Gotify-Key": {cfg.Token}})
}

func recipients(to string) ([]string, error) {
	var out []string
	for _, a := range strings.Split(to, ",") {
		if a = strings.TrimSpace(a); a == "" {
			continue
		}
		addr, err := mail.ParseAddress(a)
		if err != nil {
			return nil, fmt.Errorf("recipient %q: %w", a, err)
		}
		out = append(out, addr.Address)
	}
	if len(out) == 0 {
		return nil, errNoRecipients
	}
	return out, nil
}

func validateEmail(c Config) error {
	if strings.TrimSpace(c.Host) == "" || strings.ContainsAny(c.Host, "/: ") {
		return errors.New("SMTP host is required (without port)")
	}
	if c.Port < 0 || c.Port > 65535 {
		return errors.New("port must be 1 to 65535")
	}
	switch c.TLS {
	case "", "starttls", "tls", "none":
	default:
		return errors.New("tls must be starttls, tls or none")
	}
	if c.Username != "" && c.TLS == "none" {
		return errors.New("refusing to send the SMTP password without TLS")
	}
	if _, err := mail.ParseAddress(c.From); err != nil {
		return fmt.Errorf("from: %w", err)
	}
	_, err := recipients(c.To)
	return err
}

func smtpPort(c Config) int {
	switch {
	case c.Port != 0:
		return c.Port
	case c.TLS == "tls":
		return 465
	default:
		return 587
	}
}

// sendEmail sends a plain-text message over SMTP with STARTTLS (default), implicit TLS or, for
// local relays, without encryption. Credentials are only sent over TLS.
func sendEmail(ctx context.Context, _ *http.Client, cfg Config, ev Event) error {
	if err := validateEmail(cfg); err != nil {
		return err
	}
	to, _ := recipients(cfg.To)
	from, _ := mail.ParseAddress(cfg.From)
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(smtpPort(cfg)))
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}

	d := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if cfg.TLS == "tls" {
		conn, err = (&tls.Dialer{NetDialer: d, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	cl, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = cl.Close() }()
	if cfg.TLS == "" || cfg.TLS == "starttls" {
		if err := cl.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if cfg.Username != "" {
		if err := cl.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return err
		}
	}
	if err := cl.Mail(from.Address); err != nil {
		return err
	}
	for _, r := range to {
		if err := cl.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	var msg bytes.Buffer
	hdr := func(k, v string) { fmt.Fprintf(&msg, "%s: %s\r\n", k, v) }
	hdr("From", from.String())
	hdr("To", strings.Join(to, ", "))
	hdr("Subject", mime.QEncoding.Encode("utf-8", subject(ev)))
	hdr("Date", ev.Time.Format(time.RFC1123Z))
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", "text/plain; charset=utf-8")
	hdr("Content-Transfer-Encoding", "8bit")
	msg.WriteString("\r\n")
	msg.WriteString(strings.ReplaceAll(text(ev), "\n", "\r\n"))
	msg.WriteString("\r\n")
	if _, err := w.Write(msg.Bytes()); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

// oneLine removes line breaks, which would end an email header.
func oneLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}
