// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package notify sends events to notification channels: webhooks, chat services, push services
// and email (#86, ADR-0034). Channels are configured by administrators and stored in the
// database; each receives the event kinds it subscribed to.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/store/db"
)

// Event kinds. Keep in sync with api/openapi.yaml and the web UI.
const (
	KindImageUpdate        = "image.update"        // a tag in use points to a new image (#96)
	KindAutoUpdateDone     = "autoupdate.updated"  // an automatic update succeeded (#54)
	KindAutoUpdateFailed   = "autoupdate.failed"   // an automatic update failed and was rolled back
	KindAutoUpdatePending  = "autoupdate.pending"  // automatic updates wait for approval (#56)
	KindContainerDied      = "container.died"      // a container exited with an error code
	KindContainerOOM       = "container.oom"       // a container ran out of memory
	KindContainerUnhealthy = "container.unhealthy" // a container's healthcheck started failing (#22)
	KindContainerAutoHeal  = "container.autoheal"  // nerdui restarted an unhealthy container, or gave up (#22)
	KindJobFailed          = "job.failed"          // a background operation failed
	KindGitUpdate          = "git.update"          // a Git stack has a new commit or was updated from it (#38, #39)
	KindTest               = "test"                // sent by "Send test"; every channel receives it
)

// Kinds are the event kinds a channel can subscribe to.
var Kinds = []string{
	KindImageUpdate, KindAutoUpdateDone, KindAutoUpdateFailed, KindAutoUpdatePending,
	KindContainerDied, KindContainerOOM, KindContainerUnhealthy, KindContainerAutoHeal, KindJobFailed, KindGitUpdate,
}

// Severities.
const (
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Event is something to tell people about. It is the webhook payload: keep in sync with
// docs/notifications.md.
type Event struct {
	Kind        string `json:"kind"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Message     string `json:"message,omitempty"`
	Host        string `json:"host,omitempty"`
	Environment string `json:"environment,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	// Object names what the event is about (container, stack, image reference, job); events of
	// the same kind and object are sent at most once per DedupeWindow.
	Object string    `json:"object,omitempty"`
	Time   time.Time `json:"time"`
}

// Channel types.
const (
	TypeWebhook  = "webhook"
	TypeSlack    = "slack"
	TypeDiscord  = "discord"
	TypeTelegram = "telegram"
	TypeNtfy     = "ntfy"
	TypeGotify   = "gotify"
	TypeEmail    = "email"
)

// Types are the supported channel types.
var Types = []string{TypeWebhook, TypeSlack, TypeDiscord, TypeTelegram, TypeNtfy, TypeGotify, TypeEmail}

// Config is a channel's settings; which fields apply depends on the type. Keep in sync with
// api/openapi.yaml.
type Config struct {
	// URL: webhook, Slack and Discord webhook URL, ntfy topic URL, Gotify server URL.
	URL string `json:"url,omitempty"`
	// Secret: HMAC key of webhook signatures. Write-only.
	Secret string `json:"secret,omitempty"`
	// Token: Telegram bot token, ntfy access token, Gotify application token. Write-only.
	Token string `json:"token,omitempty"`
	// ChatID: Telegram chat.
	ChatID string `json:"chatId,omitempty"`
	// Email.
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	TLS      string `json:"tls,omitempty"` // starttls (default), tls or none
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"` // write-only
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"` // comma-separated
}

// secretFields lists the write-only fields of each type: the Slack and Discord webhook URLs carry
// their credentials.
var secretFields = map[string][]string{
	TypeWebhook: {"secret"}, TypeSlack: {"url"}, TypeDiscord: {"url"}, TypeTelegram: {"token"},
	TypeNtfy: {"token"}, TypeGotify: {"token"}, TypeEmail: {"password"},
}

func (c *Config) field(name string) *string {
	switch name {
	case "url":
		return &c.URL
	case "secret":
		return &c.Secret
	case "token":
		return &c.Token
	case "password":
		return &c.Password
	}
	return nil
}

// Masked returns the config without secrets, and which secrets are set.
func (c Config) Masked(typ string) (Config, []string) {
	set := []string{}
	for _, f := range secretFields[typ] {
		p := c.field(f)
		if *p != "" {
			set = append(set, f)
		}
		*p = ""
	}
	return c, set
}

// KeepSecrets fills secrets left empty in c from the stored config, when the type is unchanged.
func (c *Config) KeepSecrets(typ string, stored Config) {
	for _, f := range secretFields[typ] {
		if p := c.field(f); *p == "" {
			*p = *stored.field(f)
		}
	}
}

// Channel is a configured notification channel.
type Channel struct {
	ID      int64
	Name    string
	Type    string
	Config  Config
	Events  []string
	Enabled bool
	// LastSentAt and LastError describe the newest delivery attempt.
	LastSentAt time.Time
	LastError  string
}

// FromRow decodes a stored channel.
func FromRow(r db.NotificationChannel) (Channel, error) {
	ch := Channel{ID: r.ID, Name: r.Name, Type: r.Type, Enabled: r.Enabled != 0, LastError: r.LastError}
	if r.LastSentAt > 0 {
		ch.LastSentAt = time.Unix(r.LastSentAt, 0).UTC()
	}
	if err := json.Unmarshal([]byte(r.Config), &ch.Config); err != nil {
		return Channel{}, fmt.Errorf("channel %s: config: %w", r.Name, err)
	}
	if err := json.Unmarshal([]byte(r.Events), &ch.Events); err != nil {
		return Channel{}, fmt.Errorf("channel %s: events: %w", r.Name, err)
	}
	return ch, nil
}

// Validate checks a channel's name, type, events and the settings its type needs.
func (ch Channel) Validate() error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{errdefs.ErrInvalidArgument}, a...)...)
	}
	if strings.TrimSpace(ch.Name) == "" || len(ch.Name) > 100 {
		return bad("name must be 1 to 100 characters")
	}
	for _, k := range ch.Events {
		if !slices.Contains(Kinds, k) {
			return bad("unknown event %q", k)
		}
	}
	s, ok := senders[ch.Type]
	if !ok {
		return bad("unknown channel type %q", ch.Type)
	}
	if err := s.validate(ch.Config); err != nil {
		return bad("%s", err.Error())
	}
	return nil
}

// Store is the database of channels; *db.Queries of the write connection implements it.
type Store interface {
	ListNotificationChannels(ctx context.Context) ([]db.NotificationChannel, error)
	SetNotificationResult(ctx context.Context, arg db.SetNotificationResultParams) error
}

// DedupeWindow suppresses repeats of an event (same kind, environment, namespace and object).
const DedupeWindow = 15 * time.Minute

const (
	queueSize   = 256
	sendTimeout = 20 * time.Second
)

// Service queues events and sends them to the channels subscribed to their kind.
type Service struct {
	Store  Store
	Host   string // shown in messages: which server the event comes from
	Client *http.Client
	Logger *slog.Logger
	Now    func() time.Time

	once   sync.Once
	queue  chan Event
	mu     sync.Mutex
	recent map[string]time.Time
}

func (s *Service) init() {
	s.once.Do(func() {
		s.queue = make(chan Event, queueSize)
		s.recent = map[string]time.Time{}
	})
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Service) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: sendTimeout}
}

// Notify queues an event without blocking; it is dropped when the queue is full or a repeat
// within DedupeWindow. A nil Service ignores events.
func (s *Service) Notify(ev Event) {
	if s == nil {
		return
	}
	s.init()
	if ev.Time.IsZero() {
		ev.Time = s.now()
	}
	if ev.Host == "" {
		ev.Host = s.Host
	}
	if ev.Severity == "" {
		ev.Severity = SeverityInfo
	}
	key := strings.Join([]string{ev.Kind, ev.Environment, ev.Namespace, ev.Object}, "\x00")
	s.mu.Lock()
	for k, t := range s.recent {
		if ev.Time.Sub(t) >= DedupeWindow {
			delete(s.recent, k)
		}
	}
	if _, dup := s.recent[key]; dup {
		s.mu.Unlock()
		return
	}
	s.recent[key] = ev.Time
	s.mu.Unlock()
	select {
	case s.queue <- ev:
	default:
		s.log().Warn("notification queue full, event dropped", "kind", ev.Kind, "title", ev.Title)
	}
}

// Run sends queued events until ctx ends.
func (s *Service) Run(ctx context.Context) {
	s.init()
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-s.queue:
			s.dispatch(ctx, ev)
		}
	}
}

func (s *Service) dispatch(ctx context.Context, ev Event) {
	rows, err := s.Store.ListNotificationChannels(ctx)
	if err != nil {
		s.log().Error("list notification channels", "error", err)
		return
	}
	for _, r := range rows {
		ch, err := FromRow(r)
		if err != nil {
			s.log().Error("notification channel", "error", err)
			continue
		}
		if !ch.Enabled || !slices.Contains(ch.Events, ev.Kind) {
			continue
		}
		_ = s.Send(ctx, ch, ev)
	}
}

// Send delivers one event to a channel now and records the result on the channel.
func (s *Service) Send(ctx context.Context, ch Channel, ev Event) error {
	s.init()
	if ev.Time.IsZero() {
		ev.Time = s.now()
	}
	if ev.Host == "" {
		ev.Host = s.Host
	}
	if ev.Severity == "" {
		ev.Severity = SeverityInfo
	}
	snd, ok := senders[ch.Type]
	if !ok {
		return fmt.Errorf("%w: unknown channel type %q", errdefs.ErrInvalidArgument, ch.Type)
	}
	sctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	err := snd.send(sctx, s.client(), ch.Config, ev)
	res := db.SetNotificationResultParams{ID: ch.ID, SentAt: s.now().Unix()}
	if err != nil {
		res.Error = truncate(err.Error(), 300)
		s.log().Warn("notification not sent", "channel", ch.Name, "type", ch.Type, "kind", ev.Kind, "error", err)
	}
	if ch.ID != 0 {
		if serr := s.Store.SetNotificationResult(context.WithoutCancel(ctx), res); serr != nil {
			s.log().Error("store notification result", "channel", ch.Name, "error", serr)
		}
	}
	return err
}

// TestEvent is sent by "Send test".
func TestEvent() Event {
	return Event{Kind: KindTest, Severity: SeverityInfo, Title: "Test notification from nerdui",
		Message: "This channel is set up correctly."}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// errStatus describes an unsuccessful HTTP response, with the start of its body.
func errStatus(resp *http.Response, body []byte) error {
	return fmt.Errorf("%s: %s", resp.Status, truncate(strings.TrimSpace(string(body)), 200))
}

var errNoRecipients = errors.New("no recipients")
