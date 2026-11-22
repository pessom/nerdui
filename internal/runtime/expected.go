// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"sync"
	"time"
)

// expectGrace covers the delay between an exit and its event reaching a subscriber.
const expectGrace = 5 * time.Second

// expectedExits remembers the containers and compose projects that nerdui is stopping or
// restarting, so that the exits it causes are not reported as crashes: a restart starts the
// container again, which clears containerd's explicitly-stopped label before the notification
// watcher reads it.
var expectedExits = struct {
	sync.Mutex
	m map[string]*expectWindow
}{m: map[string]*expectWindow{}}

type expectWindow struct {
	from, until time.Time
	active      int
}

func containerKey(namespace, id string) string { return "container\x00" + namespace + "\x00" + id }
func projectKey(namespace, p string) string    { return "project\x00" + namespace + "\x00" + p }

// expect marks key as being stopped by nerdui until done is called.
func expect(key string) (done func()) {
	now := time.Now()
	expectedExits.Lock()
	defer expectedExits.Unlock()
	for k, w := range expectedExits.m {
		if w.active == 0 && now.Sub(w.until) > time.Minute {
			delete(expectedExits.m, k)
		}
	}
	w := expectedExits.m[key]
	if w == nil || (w.active == 0 && now.Sub(w.until) > expectGrace) {
		w = &expectWindow{from: now}
		expectedExits.m[key] = w
	}
	w.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			expectedExits.Lock()
			defer expectedExits.Unlock()
			w.active--
			w.until = time.Now()
		})
	}
}

// ExpectedExit reports whether a container that exited at the given time was being stopped,
// restarted or removed by nerdui, alone or as part of its compose project.
func ExpectedExit(namespace, id, project string, at time.Time) bool {
	expectedExits.Lock()
	defer expectedExits.Unlock()
	keys := []string{containerKey(namespace, id)}
	if project != "" {
		keys = append(keys, projectKey(namespace, project))
	}
	for _, k := range keys {
		w := expectedExits.m[k]
		if w != nil && !at.Before(w.from.Add(-expectGrace)) && (w.active > 0 || !at.After(w.until.Add(expectGrace))) {
			return true
		}
	}
	return false
}
