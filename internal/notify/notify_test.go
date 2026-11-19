// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package notify

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/store/db"
)

type request struct {
	path   string
	header http.Header
	body   string
}

func recorder(t *testing.T, status int) (*httptest.Server, func() []request) {
	t.Helper()
	var mu sync.Mutex
	var got []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, request{r.URL.Path, r.Header.Clone(), string(b)})
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []request {
		mu.Lock()
		defer mu.Unlock()
		return append([]request(nil), got...)
	}
}

var ev = Event{
	Kind: KindContainerDied, Severity: SeverityError, Title: "Container web exited with code 1",
	Message: "Exit code 1", Host: "vps", Environment: "local", Namespace: "default", Object: "web",
	Time: time.Date(2027, 3, 1, 10, 0, 0, 0, time.UTC),
}

func TestSenders(t *testing.T) {
	ctx := context.Background()
	srv, got := recorder(t, http.StatusOK)
	c := srv.Client()

	if err := sendWebhook(ctx, c, Config{URL: srv.URL + "/hook", Secret: "s3"}, ev); err != nil {
		t.Fatal(err)
	}
	r := got()[0]
	m := hmac.New(sha256.New, []byte("s3"))
	m.Write([]byte(r.body))
	var payload Event
	if json.Unmarshal([]byte(r.body), &payload) != nil || payload.Kind != KindContainerDied ||
		r.header.Get("X-Nerdui-Signature") != "sha256="+hex.EncodeToString(m.Sum(nil)) ||
		r.header.Get("X-Nerdui-Event") != KindContainerDied {
		t.Errorf("webhook %+v", r)
	}

	if err := sendSlack(ctx, c, Config{URL: srv.URL + "/slack"}, ev); err != nil {
		t.Fatal(err)
	}
	if b := got()[1].body; !strings.Contains(b, `"text":"[vps] Container web exited with code 1\nExit code 1\n(environment local, namespace default)"`) {
		t.Errorf("slack %s", b)
	}
	if err := sendDiscord(ctx, c, Config{URL: srv.URL + "/discord"}, ev); err != nil {
		t.Fatal(err)
	}
	if b := got()[2].body; !strings.Contains(b, `"content":"[vps]`) || !strings.Contains(b, `"allowed_mentions"`) {
		t.Errorf("discord %s", b)
	}
	if err := sendNtfy(ctx, c, Config{URL: srv.URL + "/alerts", Token: "tk"}, ev); err != nil {
		t.Fatal(err)
	}
	if r := got()[3]; r.body != "Exit code 1" || r.header.Get("Priority") != "high" ||
		r.header.Get("Authorization") != "Bearer tk" || r.header.Get("Title") != "[vps] Container web exited with code 1" {
		t.Errorf("ntfy %+v", r)
	}
	if err := sendGotify(ctx, c, Config{URL: srv.URL + "/", Token: "gt"}, ev); err != nil {
		t.Fatal(err)
	}
	if r := got()[4]; r.path != "/message" || r.header.Get("X-Gotify-Key") != "gt" || !strings.Contains(r.body, `"priority":8`) {
		t.Errorf("gotify %+v", r)
	}

	old := telegramAPI
	telegramAPI = srv.URL
	t.Cleanup(func() { telegramAPI = old })
	if err := sendTelegram(ctx, c, Config{Token: "123:abc", ChatID: "-100"}, ev); err != nil {
		t.Fatal(err)
	}
	if r := got()[5]; r.path != "/bot123:abc/sendMessage" || !strings.Contains(r.body, `"chat_id":"-100"`) {
		t.Errorf("telegram %+v", r)
	}
}

func TestSendErrorsHideSecrets(t *testing.T) {
	srv, _ := recorder(t, http.StatusBadRequest)
	old := telegramAPI
	telegramAPI = srv.URL
	t.Cleanup(func() { telegramAPI = old })
	err := sendTelegram(context.Background(), srv.Client(), Config{Token: "123:secret", ChatID: "1"}, ev)
	if err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("telegram error %v", err)
	}
	// An unreachable Slack URL: the error names the host, not the URL with its credentials.
	err = sendSlack(context.Background(), http.DefaultClient, Config{URL: "http://127.0.0.1:1/services/T0/B0/xyz"}, ev)
	if err == nil || strings.Contains(err.Error(), "xyz") {
		t.Errorf("slack error %v", err)
	}
}

func TestValidate(t *testing.T) {
	ok := []Channel{
		{Name: "hook", Type: TypeWebhook, Config: Config{URL: "https://example.com/h"}, Events: []string{KindJobFailed}},
		{Name: "tg", Type: TypeTelegram, Config: Config{Token: "1:a", ChatID: "@ops"}},
		{Name: "mail", Type: TypeEmail, Config: Config{Host: "smtp.example.com", From: "nerdui <n@example.com>", To: "a@example.com, b@example.com"}},
	}
	for _, ch := range ok {
		if err := ch.Validate(); err != nil {
			t.Errorf("%s: %v", ch.Name, err)
		}
	}
	bad := []Channel{
		{Name: "", Type: TypeWebhook, Config: Config{URL: "https://example.com"}},
		{Name: "x", Type: "sms"},
		{Name: "x", Type: TypeWebhook, Config: Config{URL: "ftp://example.com"}},
		{Name: "x", Type: TypeWebhook, Config: Config{URL: "https://example.com"}, Events: []string{"nope"}},
		{Name: "x", Type: TypeTelegram, Config: Config{Token: "../x", ChatID: "1"}},
		{Name: "x", Type: TypeGotify, Config: Config{URL: "https://g.example.com"}},
		{Name: "x", Type: TypeEmail, Config: Config{Host: "smtp.example.com:25", From: "a@example.com", To: "b@example.com"}},
		{Name: "x", Type: TypeEmail, Config: Config{Host: "smtp.example.com", From: "a@example.com", To: ""}},
	}
	for _, ch := range bad {
		if err := ch.Validate(); err == nil {
			t.Errorf("accepted %+v", ch)
		}
	}
}

func TestSecrets(t *testing.T) {
	c := Config{URL: "https://hooks.slack.com/services/x", ChatID: "1"}
	m, set := c.Masked(TypeSlack)
	if m.URL != "" || len(set) != 1 || set[0] != "url" {
		t.Errorf("masked %+v %v", m, set)
	}
	m.KeepSecrets(TypeSlack, c)
	if m.URL != c.URL {
		t.Errorf("kept %+v", m)
	}
}

type fakeStore struct {
	mu      sync.Mutex
	rows    []db.NotificationChannel
	results []db.SetNotificationResultParams
}

func (f *fakeStore) ListNotificationChannels(context.Context) ([]db.NotificationChannel, error) {
	return f.rows, nil
}

func (f *fakeStore) SetNotificationResult(_ context.Context, p db.SetNotificationResultParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results = append(f.results, p)
	return nil
}

func TestServiceDispatch(t *testing.T) {
	srv, got := recorder(t, http.StatusOK)
	row := func(id int64, name string, enabled int64, events ...string) db.NotificationChannel {
		cfg, _ := json.Marshal(Config{URL: srv.URL + "/" + name})
		ev, _ := json.Marshal(events)
		return db.NotificationChannel{ID: id, Name: name, Type: TypeWebhook, Config: string(cfg), Events: string(ev), Enabled: enabled}
	}
	st := &fakeStore{rows: []db.NotificationChannel{
		row(1, "all", 1, KindContainerDied, KindJobFailed),
		row(2, "jobs", 1, KindJobFailed),
		row(3, "off", 0, KindContainerDied),
	}}
	s := &Service{Store: st, Host: "vps", Client: srv.Client()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	s.Notify(ev)
	s.Notify(ev) // repeat within the dedupe window
	s.Notify(Event{Kind: KindJobFailed, Title: "Job failed", Object: "j1"})
	deadline := time.Now().Add(5 * time.Second)
	for len(got()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	paths := map[string]int{}
	for _, r := range got() {
		paths[r.path]++
	}
	if len(got()) != 3 || paths["/all"] != 2 || paths["/jobs"] != 1 {
		t.Errorf("deliveries %v", paths)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.results) != 3 || st.results[0].Error != "" {
		t.Errorf("results %+v", st.results)
	}
	var nilService *Service
	nilService.Notify(ev) // no-op
}

// fakeSMTP accepts one message without TLS and returns what it received.
func fakeSMTP(t *testing.T) (port int, got <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		rd, w := bufio.NewReader(conn), conn
		var all strings.Builder
		write := func(s string) { _, _ = io.WriteString(w, s+"\r\n") }
		write("220 fake")
		data := false
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			all.WriteString(line)
			switch {
			case data && line == ".\r\n":
				data = false
				write("250 queued")
			case data:
			case strings.HasPrefix(line, "EHLO"):
				write("250-fake")
				write("250 8BITMIME")
			case strings.HasPrefix(line, "DATA"):
				data = true
				write("354 go on")
			case strings.HasPrefix(line, "QUIT"):
				write("221 bye")
				ch <- all.String()
				return
			default:
				write("250 ok")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, ch
}

func TestSendEmail(t *testing.T) {
	port, got := fakeSMTP(t)
	cfg := Config{Host: "127.0.0.1", Port: port, TLS: "none", From: "nerdui <n@example.com>", To: "a@example.com"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e := ev
	e.Title = "Injected\r\nBcc: x@example.com"
	if err := sendEmail(ctx, nil, cfg, e); err != nil {
		t.Fatal(err)
	}
	msg := <-got
	if !strings.Contains(msg, "RCPT TO:<a@example.com>") || !strings.Contains(msg, "Subject: [vps] Injected  Bcc: x@example.com") ||
		strings.Contains(msg, "\r\nBcc:") || !strings.Contains(msg, "Exit code 1") {
		t.Errorf("message:\n%s", msg)
	}
	cfg.Username, cfg.Password = "u", "p"
	if err := sendEmail(ctx, nil, cfg, ev); err == nil || !strings.Contains(err.Error(), "without TLS") {
		t.Errorf("password without TLS: %v", err)
	}
}
