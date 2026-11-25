// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package hostmetrics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pessom/nerdui/internal/notify"
)

// fakeProc writes a procfs with the given CPU jiffies (busy, idle), available memory and
// network counters.
func fakeProc(t *testing.T, dir string, busy, idle, availKB, rx, tx uint64) {
	t.Helper()
	files := map[string]string{
		"stat":    fmt.Sprintf("cpu  %d 0 0 %d 0 0 0 0 0 0\ncpu0 1 2 3 4\n", busy, idle),
		"meminfo": fmt.Sprintf("MemTotal:       1000 kB\nMemFree:         10 kB\nMemAvailable:   %d kB\nSwapTotal:       100 kB\nSwapFree:         75 kB\n", availKB),
		"loadavg": "0.52 0.40 0.30 1/200 1234\n",
		"net/dev": "Inter-|   Receive  |  Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes\n" +
			fmt.Sprintf("    lo: 999 1 0 0 0 0 0 0 999 1 0 0 0 0 0 0\n  eth0: %d 1 0 0 0 0 0 0 %d 1 0 0 0 0 0 0\nveth12: 555 1 0 0 0 0 0 0 555 1 0 0 0 0 0 0\n", rx, tx),
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSampler(t *testing.T) {
	proc := t.TempDir()
	now := time.Date(2027, 3, 1, 10, 0, 0, 0, time.UTC)
	var events []notify.Event
	alerts := DefaultAlerts()
	alerts.CPU = Threshold{Enabled: true, Percent: 80, Minutes: 1}
	alerts.Disk = Threshold{Enabled: true, Percent: 90}
	diskFree := uint64(500)
	s := &Sampler{
		Proc: proc, Paths: []string{"/", "/var/lib/nerdui", "/srv"}, Environment: "local",
		Alerts: func(context.Context) (Alerts, error) { return alerts, nil },
		Notify: func(ev notify.Event) { events = append(events, ev) },
		now:    func() time.Time { return now },
		statfs: func(p string) (string, uint64, uint64, error) {
			switch p {
			case "/", "/var/lib/nerdui":
				return "root", 1000, diskFree, nil
			}
			return "", 0, 0, errors.New("no such file")
		},
	}
	ctx := context.Background()

	fakeProc(t, proc, 100, 900, 400, 1000, 2000)
	s.Collect(ctx)
	if _, err := s.Current(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("first reading only sets the counters: %v", err)
	}
	// 15 s later: 90 of 100 jiffies busy, 1500 bytes received, 3000 sent.
	now = now.Add(15 * time.Second)
	fakeProc(t, proc, 190, 910, 400, 2500, 5000)
	s.Collect(ctx)
	c, err := s.Current()
	if err != nil {
		t.Fatal(err)
	}
	if c.CPU != 90 || c.MemoryUsed != 600*1024 || c.MemoryTotal != 1000*1024 || c.SwapUsed != 25*1024 || c.Load1 != 0.52 ||
		c.NetRx != 100 || c.NetTx != 200 || c.MemoryPercent() != 60 {
		t.Errorf("sample %+v", c)
	}
	if len(c.Disks) != 1 || c.Disks[0].Path != "/" || len(c.Disks[0].Paths) != 2 || c.Disks[0].Percent() != 50 {
		t.Errorf("disks %+v", c.Disks)
	}
	if len(events) != 0 {
		t.Errorf("CPU alert before a minute: %+v", events)
	}

	// CPU stays high for a minute: one alert. The disk fills up: an alert at once.
	diskFree = 50
	for range 4 {
		now = now.Add(15 * time.Second)
		// Each step adds 90 busy and 10 idle jiffies.
		fakeProc(t, proc, 190+90*uint64(len(s.points)), 910+10*uint64(len(s.points)), 400, 2500, 5000)
		s.Collect(ctx)
	}
	if len(events) != 2 || events[0].Kind != notify.KindHostAlert || events[0].Object != "disk /" || events[1].Object != "cpu" ||
		events[1].Severity != notify.SeverityWarning || events[1].Environment != "local" {
		t.Fatalf("alerts %+v", events)
	}
	// Below the threshold but within the hysteresis: no recovery yet; then clearly below.
	diskFree = 120 // 88%
	now = now.Add(15 * time.Second)
	fakeProc(t, proc, 190+90*uint64(len(s.points))+80, 910+10*uint64(len(s.points))+20, 400, 2500, 5000)
	s.Collect(ctx)
	diskFree = 500
	now = now.Add(15 * time.Second)
	n := uint64(len(s.points))
	fakeProc(t, proc, 190+90*n+80+10, 910+10*n+20+90, 400, 2500, 5000)
	s.Collect(ctx)
	var recovered []string
	for _, ev := range events[2:] {
		recovered = append(recovered, ev.Object)
	}
	if fmt.Sprint(recovered) != "[cpu recovered disk / recovered]" {
		t.Errorf("recoveries %v (%+v)", recovered, events)
	}

	if h := s.History(time.Hour); len(h) != len(s.points) || h[0].Disks["/"] != 50 {
		t.Errorf("history %+v", h)
	}
}

func TestHistoryDownsamples(t *testing.T) {
	now := time.Date(2027, 3, 1, 10, 0, 0, 0, time.UTC)
	s := &Sampler{now: func() time.Time { return now }}
	for i := range 2000 {
		s.points = append(s.points, Point{T: now.Add(time.Duration(i-2000) * 15 * time.Second).Unix(), CPU: float64(i % 2 * 10), Disks: map[string]float64{"/": float64(i)}})
	}
	h := s.History(24 * time.Hour)
	if len(h) > MaxPoints || len(h) < MaxPoints/2 || h[0].CPU != 5 || h[len(h)-1].Disks["/"] != 1999 {
		t.Errorf("%d points, first %+v last %+v", len(h), h[0], h[len(h)-1])
	}
	if h := s.History(time.Hour); len(h) != 240 {
		t.Errorf("last hour: %d points", len(h))
	}
}

func TestAlertsValidate(t *testing.T) {
	a := DefaultAlerts()
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	a.Disk.Percent = 0
	if a.Validate() == nil {
		t.Error("0%")
	}
	a = DefaultAlerts()
	a.CPU.Minutes = -1
	if a.Validate() == nil {
		t.Error("negative minutes")
	}
}

func TestSkipInterface(t *testing.T) {
	for name, skip := range map[string]bool{"lo": true, "veth1a2b": true, "nerdctl0": true, "cni0": true, "eth0": false, "ens3": false, "wlan0": false} {
		if skipInterface(name) != skip {
			t.Errorf("%s: %v", name, !skip)
		}
	}
}
