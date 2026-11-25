// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package hostmetrics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/containerd/errdefs"

	"github.com/pessom/nerdui/internal/notify"
	"github.com/pessom/nerdui/internal/store/db"
)

// SettingKey stores the alert thresholds.
const SettingKey = "host.alerts"

// hysteresis is how far below its threshold a value must fall to count as recovered, so a
// value hovering around the threshold does not alert again and again.
const hysteresis = 5.0

// Threshold alerts when a usage percent stays at or above Percent for Minutes (0: at once).
// Keep in sync with api/openapi.yaml.
type Threshold struct {
	Enabled bool    `json:"enabled"`
	Percent float64 `json:"percent"`
	Minutes int     `json:"minutes"`
}

// Alerts are the thresholds of the host (#64). Disk applies to each watched filesystem.
type Alerts struct {
	CPU    Threshold `json:"cpu"`
	Memory Threshold `json:"memory"`
	Disk   Threshold `json:"disk"`
}

// DefaultAlerts are off, with common values to start from.
func DefaultAlerts() Alerts {
	return Alerts{
		CPU:    Threshold{Percent: 90, Minutes: 5},
		Memory: Threshold{Percent: 90, Minutes: 5},
		Disk:   Threshold{Percent: 90},
	}
}

// Validate checks the thresholds.
func (a Alerts) Validate() error {
	for name, t := range map[string]Threshold{"cpu": a.CPU, "memory": a.Memory, "disk": a.Disk} {
		if t.Percent < 1 || t.Percent > 100 {
			return fmt.Errorf("%w: %s: the percent must be 1 to 100", errdefs.ErrInvalidArgument, name)
		}
		if t.Minutes < 0 || t.Minutes > 1440 {
			return fmt.Errorf("%w: %s: the minutes must be 0 to 1440", errdefs.ErrInvalidArgument, name)
		}
	}
	return nil
}

// Settings stores the thresholds; *db.Queries implements it.
type Settings interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, arg db.SetSettingParams) error
}

// LoadAlerts returns the stored thresholds, or the defaults.
func LoadAlerts(ctx context.Context, st Settings) (Alerts, error) {
	v, err := st.GetSetting(ctx, SettingKey)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultAlerts(), nil
	}
	if err != nil {
		return Alerts{}, err
	}
	a := DefaultAlerts()
	if err := json.Unmarshal([]byte(v), &a); err != nil {
		return Alerts{}, fmt.Errorf("stored host alerts: %w", err)
	}
	return a, nil
}

// SaveAlerts validates and stores the thresholds.
func SaveAlerts(ctx context.Context, st Settings, a Alerts) error {
	if err := a.Validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(a)
	return st.SetSetting(ctx, db.SetSettingParams{Key: SettingKey, Value: string(b), Now: time.Now().Unix()})
}

// alerter remembers, per watched value, since when it is above its threshold and whether an
// alert was sent.
type alerter struct {
	state map[string]*alertState
}

type alertState struct {
	since   time.Time
	alerted bool
}

// check returns the alerts and recoveries of a sample.
func (al *alerter) check(a Alerts, s Sample) []notify.Event {
	if al.state == nil {
		al.state = map[string]*alertState{}
	}
	var out []notify.Event
	eval := func(key, what string, t Threshold, value float64) {
		st := al.state[key]
		if st == nil {
			st = &alertState{}
			al.state[key] = st
		}
		if !t.Enabled {
			*st = alertState{}
			return
		}
		if value >= t.Percent {
			if st.since.IsZero() {
				st.since = s.Time
			}
			if !st.alerted && s.Time.Sub(st.since) >= time.Duration(t.Minutes)*time.Minute {
				st.alerted = true
				msg := fmt.Sprintf("%s is at %.0f%%, the threshold is %.0f%%", what, value, t.Percent)
				if t.Minutes > 0 {
					msg += fmt.Sprintf(" for %d min", t.Minutes)
				}
				out = append(out, notify.Event{
					Severity: notify.SeverityWarning, Object: key,
					Title: fmt.Sprintf("%s above %.0f%%", what, t.Percent), Message: msg, Time: s.Time,
				})
			}
			return
		}
		st.since = time.Time{}
		if st.alerted && value < t.Percent-hysteresis {
			st.alerted = false
			out = append(out, notify.Event{
				Severity: notify.SeverityInfo, Object: key + " recovered",
				Title:   fmt.Sprintf("%s back below %.0f%%", what, t.Percent),
				Message: fmt.Sprintf("%s is at %.0f%%", what, value), Time: s.Time,
			})
		}
	}
	eval("cpu", "CPU usage", a.CPU, s.CPU)
	eval("memory", "Memory usage", a.Memory, s.MemoryPercent())
	for _, d := range s.Disks {
		eval("disk "+d.Path, "Disk usage of "+d.Path, a.Disk, d.Percent())
	}
	return out
}
