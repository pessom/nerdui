// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package ws is the shared WebSocket layer for streams (ADR-0007): logs, stats, events,
// job progress and exec.
//
// Security: Accept rejects cross-origin upgrades (the browser's Origin must match Host),
// which is the CSRF protection for WebSocket; session and permission checks run as regular
// route middleware before the upgrade.
//
// Flow control: Send never blocks the producer. Each connection has a bounded queue; a
// client that cannot keep up is disconnected with StatusPolicyViolation instead of slowing
// down containerd readers.
package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/pessom/nerdui/internal/httpjson"
)

// Message types of the text-frame envelope.
const (
	TypeData  = "data"
	TypeError = "error"
	TypeEnd   = "end"
)

// Message is the JSON envelope of text frames:
//
//	{"type": "data", "data": ..., "ts": "2026-11-11T12:00:00Z"}
//	{"type": "error", "error": {"code": "...", "message": "..."}, "ts": "..."}
//	{"type": "end", "ts": "..."}
type Message struct {
	Type  string                `json:"type"`
	Data  any                   `json:"data,omitempty"`
	Error *httpjson.ErrorDetail `json:"error,omitempty"`
	TS    time.Time             `json:"ts"`
}

// Options configures a connection. Zero values select the defaults.
type Options struct {
	// SendQueue is the number of queued outgoing frames before the client is considered
	// too slow and disconnected (default 256).
	SendQueue int
	// PingInterval is the keep-alive interval (default 30s).
	PingInterval time.Duration
	// WriteTimeout bounds a single frame write or ping (default 10s).
	WriteTimeout time.Duration
	// ReadLimit is the maximum size of an incoming message (default 64 KiB).
	ReadLimit int64
	// Bidirectional keeps incoming data messages for Read (exec). Otherwise incoming data
	// closes the connection with StatusPolicyViolation and Context ends when the client
	// disconnects.
	Bidirectional bool
}

func (o *Options) defaults() {
	if o.SendQueue <= 0 {
		o.SendQueue = 256
	}
	if o.PingInterval <= 0 {
		o.PingInterval = 30 * time.Second
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 10 * time.Second
	}
	if o.ReadLimit <= 0 {
		o.ReadLimit = 64 << 10
	}
}

// StatusTryAgainLater (1013) tells clients to reconnect, e.g. after falling behind.
const StatusTryAgainLater = websocket.StatusTryAgainLater

// Close reasons sent to the client.
const (
	ReasonSlowConsumer = "slow consumer"
	ReasonServerClose  = "server closing"
)

type frame struct {
	typ  websocket.MessageType
	data []byte
}

// Conn is a WebSocket stream with a non-blocking send queue.
type Conn struct {
	c    *websocket.Conn
	opts Options

	ctx    context.Context
	cancel context.CancelFunc
	queue  chan frame
	done   chan struct{}

	closeOnce sync.Once
	mu        sync.Mutex
	closed    bool
}

// Accept upgrades the request. On failure the response has already been written
// (403 for cross-origin requests) and an error is returned.
func Accept(w http.ResponseWriter, r *http.Request, opts Options) (*Conn, error) {
	opts.defaults()
	c, err := websocket.Accept(w, r, nil) // nil options: same-origin only
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(opts.ReadLimit)

	// The stream context ends when the client goes away, the server closes the stream or
	// the request context ends (server shutdown).
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	conn := &Conn{
		c: c, opts: opts, ctx: ctx, cancel: cancel,
		queue: make(chan frame, opts.SendQueue),
		done:  make(chan struct{}),
	}
	if !opts.Bidirectional {
		go conn.discardReads()
	}
	go conn.writeLoop()
	go func() {
		select {
		case <-r.Context().Done():
			conn.Close(websocket.StatusGoingAway, ReasonServerClose)
		case <-conn.done:
		}
	}()
	return conn, nil
}

// Context is canceled when the connection ends for any reason.
func (c *Conn) Context() context.Context { return c.ctx }

// Done is closed when the connection has been closed.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Send queues a JSON message. It never blocks: when the queue is full the client is
// disconnected as a slow consumer and false is returned.
func (c *Conn) Send(m Message) bool {
	if m.TS.IsZero() {
		m.TS = time.Now().UTC()
	}
	b, err := json.Marshal(m)
	if err != nil {
		return c.SendError(httpjson.CodeInternal, "cannot encode message")
	}
	return c.enqueue(frame{typ: websocket.MessageText, data: b})
}

// SendData queues a data message.
func (c *Conn) SendData(v any) bool { return c.Send(Message{Type: TypeData, Data: v}) }

// SendDataWait queues a data message, waiting while the queue is full instead of
// disconnecting the client. It is for producers that can pause without harm, such as log
// files read for this connection only, unlike shared containerd readers. It returns false
// when the connection has closed.
func (c *Conn) SendDataWait(v any) bool {
	b, err := json.Marshal(Message{Type: TypeData, Data: v, TS: time.Now().UTC()})
	if err != nil {
		return c.SendError(httpjson.CodeInternal, "cannot encode message")
	}
	return c.enqueueWait(frame{typ: websocket.MessageText, data: b})
}

// SendError queues an error message; streams usually call End or Close afterwards.
func (c *Conn) SendError(code, message string) bool {
	return c.Send(Message{Type: TypeError, Error: &httpjson.ErrorDetail{Code: code, Message: message}})
}

// SendBinary queues a binary frame (exec output). Empty payloads are ignored.
func (c *Conn) SendBinary(p []byte) bool {
	if len(p) == 0 {
		return true
	}
	return c.enqueue(frame{typ: websocket.MessageBinary, data: p})
}

// SendBinaryWait queues a binary frame, waiting for queue space instead of disconnecting a
// slow client: exec output is paced to the client like logs (ADR-0014). The data is copied.
func (c *Conn) SendBinaryWait(p []byte) bool {
	if len(p) == 0 {
		return true
	}
	return c.enqueueWait(frame{typ: websocket.MessageBinary, data: bytes.Clone(p)})
}

// End sends the end message, closes the connection normally after it was delivered and
// returns when the connection is closed. It waits for queue space: the stream is over, and a full queue must not turn the end
// into a slow-consumer close.
func (c *Conn) End() {
	b, _ := json.Marshal(Message{Type: TypeEnd, TS: time.Now().UTC()})
	if c.enqueueWait(frame{typ: websocket.MessageText, data: b}) {
		c.enqueueWait(frame{}) // sentinel: close after flushing
	}
	// Handlers usually return right after End; returning ends the request context, which
	// would close the connection before the queue is flushed.
	<-c.done
}

// discardReads keeps reading on one-way streams so control frames (pong, close) are
// handled. It replaces websocket.Conn.CloseRead, which deadlocks until the handshake
// timeout when a client sends a data message: closing happens here, outside the read.
func (c *Conn) discardReads() {
	_, _, err := c.c.Read(c.ctx)
	if err != nil {
		c.abort() // client closed or the connection broke
		return
	}
	c.Close(websocket.StatusPolicyViolation, "unexpected data message")
}

// Read returns the next incoming message (Bidirectional connections only).
func (c *Conn) Read() (websocket.MessageType, []byte, error) {
	if !c.opts.Bidirectional {
		return 0, nil, errors.New("ws: Read on a one-way connection")
	}
	return c.c.Read(c.ctx)
}

func (c *Conn) enqueue(f frame) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	select {
	case c.queue <- f:
		return true
	default:
		go c.Close(websocket.StatusPolicyViolation, ReasonSlowConsumer)
		c.closed = true
		return false
	}
}

// enqueueWait queues f, waiting for space; false when the connection has closed.
func (c *Conn) enqueueWait(f frame) bool {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return false
	}
	select {
	case c.queue <- f:
		return true
	case <-c.done:
		return false
	}
}

func (c *Conn) writeLoop() {
	ping := time.NewTicker(c.opts.PingInterval)
	defer ping.Stop()
	for {
		select {
		case <-c.ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return
		case f := <-c.queue:
			if f.data == nil && f.typ == 0 {
				c.Close(websocket.StatusNormalClosure, "")
				return
			}
			ctx, cancel := context.WithTimeout(c.ctx, c.opts.WriteTimeout)
			err := c.c.Write(ctx, f.typ, f.data)
			cancel()
			if err != nil {
				c.abort()
				return
			}
		case <-ping.C:
			ctx, cancel := context.WithTimeout(c.ctx, c.opts.WriteTimeout)
			err := c.c.Ping(ctx)
			cancel()
			if err != nil {
				c.abort() // the peer is unresponsive: a close handshake would only wait
				return
			}
		}
	}
}

// Close closes the connection with code and reason after a close handshake (which waits
// up to a few seconds for an unresponsive peer). It is safe to call more than once.
func (c *Conn) Close(code websocket.StatusCode, reason string) {
	c.shutdown(func() { _ = c.c.Close(code, reason) })
}

// abort drops the connection immediately, for peers that stopped responding.
func (c *Conn) abort() {
	c.shutdown(func() { _ = c.c.CloseNow() })
}

func (c *Conn) shutdown(closeConn func()) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		// Close first: canceling a context used for reading makes the library drop the
		// connection without a close frame, and clients would not see code and reason.
		closeConn()
		c.cancel()
		close(c.done)
	})
}
