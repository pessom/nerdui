// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/pessom/nerdui/internal/store/db"
)

// Settings stores nerdui settings; *db.Queries implements it.
type Settings interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, arg db.SetSettingParams) error
}

// intervalCron turns an image update check interval of nerdui 0.2.0-alpha.1 into cron.
func intervalCron(d time.Duration) (expr string, on bool) {
	switch d {
	case time.Hour:
		return "0 * * * *", true
	case 12 * time.Hour:
		return "0 */12 * * *", true
	case 24 * time.Hour:
		return "0 3 * * *", true
	case 0:
		return "0 */6 * * *", false
	}
	return "0 */6 * * *", true
}

// MigrateImageChecks creates, once per environment, the schedule of image update checks from
// the interval set before the scheduler existed (#82, ADR-0048).
func (s *Service) MigrateImageChecks(ctx context.Context, settings Settings, envs []string, interval func(ctx context.Context, env string) (time.Duration, error)) error {
	for _, env := range envs {
		key := "scheduler/image-updates-migrated/" + env
		if _, err := settings.GetSetting(ctx, key); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		d, err := interval(ctx, env)
		if err != nil {
			return err
		}
		expr, on := intervalCron(d)
		if _, err := s.Create(ctx, Input{
			Name: "Image update check (" + env + ")", Kind: KindImageUpdates, Environment: env, Cron: expr, Enabled: on,
		}, User); err != nil {
			return err
		}
		if err := settings.SetSetting(ctx, db.SetSettingParams{Key: key, Value: "1", Now: s.now().Unix()}); err != nil {
			return err
		}
		s.log().Info("scheduler: image update checks are now a schedule", "environment", env, "cron", expr, "enabled", on)
	}
	return nil
}

// NextOf is the earliest next run of the enabled schedules of a kind in an environment.
func (s *Service) NextOf(ctx context.Context, kind, env string) *time.Time {
	rows, err := s.Store.ListSchedules(ctx)
	if err != nil {
		return nil
	}
	var next *time.Time
	for _, r := range rows {
		if r.Kind != kind || r.Environment != env {
			continue
		}
		if n := s.next(r); n != nil && (next == nil || n.Before(*next)) {
			next = n
		}
	}
	return next
}
