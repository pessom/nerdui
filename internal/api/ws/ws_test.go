// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package ws

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// serve starts a test server whose handler accepts the stream and runs fn.
func serve(t *testing.T, opts Options, fn func(c *Conn)) (url string, base context.CancelFunc) {
	t.Helper()
	baseCtx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Accept(w, r, opts)
		if err != nil {
			return
		}
		fn(c)
		<-c.Done()
	}))
	srv.Config.BaseContext = func(net.Listener) context.Context { return baseCtx }
	srv.Start()
	t.Cleanup(func() { cancel(); srv.Close() })
	return "ws" + strings.TrimPrefix(srv.URL, "http"), cancel
}

func dial(t *testing.T, url string, hdr http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: hdr})
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if c != nil {
		t.Cleanup(func() { _ = c.CloseNow() })
	}
	return c, resp, err
}

func readMsg(t *testing.T, c *websocket.Conn) Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("frame type %v", typ)
	}
	var m Message
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return m
}

// closeStatus reads until the connection closes and returns the close code.
func closeStatus(t *testing.T, c *websocket.Conn) websocket.StatusCode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		if _, _, err := c.Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

func TestCrossOriginRejected(t *testing.T) {
	url, _ := serve(t, Options{}, func(c *Conn) { c.End() })
	_, resp, err := dial(t, url, http.Header{"Origin": {"https://evil.example"}})
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin upgrade: resp=%v err=%v", resp, err)
	}
}

func TestDataErrorAndEnd(t *testing.T) {
	url, _ := serve(t, Options{}, func(c *Conn) {
		c.SendData(map[string]int{"n": 1})
		c.SendError("not_found", "gone")
		c.SendBinary(nil) // ignored, must not be taken as the close sentinel
		c.End()
	})
	// Same-origin browsers send a matching Origin; non-browser clients send none.
	c, _, err := dial(t, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m := readMsg(t, c); m.Type != TypeData || m.TS.IsZero() || m.Data.(map[string]any)["n"] != 1.0 {
		t.Errorf("data: %+v", m)
	}
	if m := readMsg(t, c); m.Type != TypeError || m.Error.Code != "not_found" {
		t.Errorf("error: %+v", m)
	}
	if m := readMsg(t, c); m.Type != TypeEnd {
		t.Errorf("end: %+v", m)
	}
	if code := closeStatus(t, c); code != websocket.StatusNormalClosure {
		t.Errorf("close code %v", code)
	}
}

func TestSlowConsumerDoesNotBlockProducer(t *testing.T) {
	type result struct {
		sent    int
		dropped bool
		elapsed time.Duration
	}
	res := make(chan result, 1)
	url, _ := serve(t, Options{SendQueue: 4}, func(c *Conn) {
		payload := strings.Repeat("x", 64<<10)
		start := time.Now()
		r := result{}
		for i := 0; i < 2000; i++ { // ~128 MiB: far more than socket buffers
			if !c.SendData(payload) {
				r.dropped = true
				break
			}
			r.sent++
		}
		r.elapsed = time.Since(start)
		res <- r
	})
	c, _, err := dial(t, url, nil) // does not read until the producer is done
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(1 << 20)
	select {
	case r := <-res:
		if !r.dropped {
			t.Fatalf("slow client not dropped after %d messages", r.sent)
		}
		if r.elapsed > 2*time.Second {
			t.Errorf("producer blocked for %v", r.elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("producer blocked by a slow consumer")
	}
	if code := closeStatus(t, c); code != websocket.StatusPolicyViolation {
		t.Errorf("close code %v, want policy violation", code)
	}
}

func TestOneWayStreamClosesOnClientData(t *testing.T) {
	ended := make(chan struct{})
	url, _ := serve(t, Options{}, func(c *Conn) {
		go func() { <-c.Context().Done(); close(ended) }()
	})
	c, _, err := dial(t, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(context.Background(), websocket.MessageText, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	// Like a browser, the client keeps reading and answers the close handshake.
	if code := closeStatus(t, c); code != websocket.StatusPolicyViolation {
		t.Errorf("close code %v, want policy violation", code)
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("stream context not canceled after unexpected client data")
	}
}

func TestClientDisconnectCancelsContext(t *testing.T) {
	ended := make(chan struct{})
	url, _ := serve(t, Options{}, func(c *Conn) {
		go func() { <-c.Context().Done(); close(ended) }()
	})
	c, _, err := dial(t, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close(websocket.StatusNormalClosure, "bye")
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("context not canceled after client disconnect")
	}
}

func TestPingTimeoutClosesDeadClient(t *testing.T) {
	ended := make(chan struct{})
	url, _ := serve(t, Options{PingInterval: 50 * time.Millisecond, WriteTimeout: 100 * time.Millisecond}, func(c *Conn) {
		go func() { <-c.Done(); close(ended) }()
	})
	// The client never reads, so it never answers pings.
	if _, _, err := dial(t, url, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("unresponsive client not closed")
	}
}

func TestServerShutdownClosesStreams(t *testing.T) {
	url, cancelBase := serve(t, Options{}, func(*Conn) {})
	c, _, err := dial(t, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancelBase()
	if code := closeStatus(t, c); code != websocket.StatusGoingAway {
		t.Errorf("close code %v, want going away", code)
	}
}

func TestBidirectional(t *testing.T) {
	url, _ := serve(t, Options{Bidirectional: true}, func(c *Conn) {
		typ, p, err := c.Read()
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			c.SendBinary(append([]byte("echo:"), p...))
		}
		c.End()
	})
	c, _, err := dial(t, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(context.Background(), websocket.MessageBinary, []byte("ls")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, p, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(p) != "echo:ls" {
		t.Fatalf("echo: %v %q %v", typ, p, err)
	}
	if m := readMsg(t, c); m.Type != TypeEnd {
		t.Errorf("end: %+v", m)
	}
}

func TestReadOnOneWayConnection(t *testing.T) {
	errc := make(chan error, 1)
	url, _ := serve(t, Options{}, func(c *Conn) {
		_, _, err := c.Read()
		errc <- err
		c.End()
	})
	if _, _, err := dial(t, url, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-errc; err == nil {
		t.Error("Read allowed on a one-way connection")
	}
}
