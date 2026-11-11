// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"sync"
	"time"
)

// Limiter throttles failed authentication attempts per key (client IP, username).
// After MaxFailures failures within Window the key is blocked; every further block doubles,
// from BaseBlock up to MaxBlock (ADR-0005).
type Limiter struct {
	MaxFailures int
	Window      time.Duration
	BaseBlock   time.Duration
	MaxBlock    time.Duration

	now func() time.Time

	mu        sync.Mutex
	entries   map[string]*limitEntry
	lastSweep time.Time
}

type limitEntry struct {
	failures     int
	windowStart  time.Time
	strikes      int
	blockedUntil time.Time
	lastSeen     time.Time
}

// NewLimiter returns a limiter with the defaults: 5 failures per minute, blocks from 1 to 15 minutes.
func NewLimiter(now func() time.Time) *Limiter {
	return &Limiter{
		MaxFailures: 5,
		Window:      time.Minute,
		BaseBlock:   time.Minute,
		MaxBlock:    15 * time.Minute,
		now:         now,
		entries:     map[string]*limitEntry{},
	}
}

// Allow reports whether an attempt for key may proceed; otherwise it returns how long to wait.
func (l *Limiter) Allow(key string) (retryAfter time.Duration, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	e := l.entries[key]
	if e == nil || !now.Before(e.blockedUntil) {
		return 0, true
	}
	return e.blockedUntil.Sub(now), false
}

// Fail records a failed attempt.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	e := l.entries[key]
	if e == nil {
		e = &limitEntry{windowStart: now}
		l.entries[key] = e
	}
	e.lastSeen = now
	if now.Sub(e.windowStart) > l.Window {
		e.failures, e.windowStart = 0, now
		// Forgive earlier blocks after a long quiet period.
		if !e.blockedUntil.IsZero() && now.Sub(e.blockedUntil) > l.MaxBlock {
			e.strikes = 0
		}
	}
	e.failures++
	if e.failures >= l.MaxFailures {
		block := l.BaseBlock << e.strikes
		if block <= 0 || block > l.MaxBlock {
			block = l.MaxBlock
		}
		e.strikes++
		e.failures = 0
		e.blockedUntil = now.Add(block)
	}
}

// Reset forgets key, e.g. after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// sweep drops stale entries at most once a minute. Caller holds l.mu.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for k, e := range l.entries {
		if now.After(e.blockedUntil) && now.Sub(e.lastSeen) > 2*l.MaxBlock {
			delete(l.entries, k)
		}
	}
}
