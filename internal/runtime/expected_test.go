// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"testing"
	"time"
)

func TestExpectedExit(t *testing.T) {
	now := time.Now()
	if ExpectedExit("default", "c1", "", now) {
		t.Fatal("unmarked container expected")
	}
	done := expect(containerKey("default", "c1"))
	if !ExpectedExit("default", "c1", "", time.Now()) {
		t.Error("container being restarted not expected")
	}
	if ExpectedExit("other", "c1", "", time.Now()) || ExpectedExit("default", "c2", "", time.Now()) {
		t.Error("other containers expected")
	}
	done()
	done() // idempotent
	if !ExpectedExit("default", "c1", "", time.Now()) {
		t.Error("exit event right after the restart not expected")
	}
	if ExpectedExit("default", "c1", "", time.Now().Add(time.Minute)) {
		t.Error("a later crash expected")
	}
	if ExpectedExit("default", "c1", "", now.Add(-time.Minute)) {
		t.Error("an earlier crash expected")
	}

	doneProject := expect(projectKey("default", "shop"))
	if !ExpectedExit("default", "c9", "shop", time.Now()) || ExpectedExit("default", "c9", "blog", time.Now()) {
		t.Error("project restart")
	}
	doneProject()
}
