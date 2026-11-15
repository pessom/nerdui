// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package prometheus

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/containerd/containerd/v2/pkg/identifiers"
	"github.com/containerd/errdefs"
)

// Metric names of containerd's cgroup v2 task metrics ("container" namespace, unit suffix
// added by docker/go-metrics; labels container_id and namespace). Verified against a real
// containerd by TestIntegrationContainerdMetrics.
const (
	MetricCPU          = "container_cpu_usage_usec_microseconds"
	MetricMemory       = "container_memory_usage_bytes"
	MetricInactiveFile = "container_memory_inactive_file_bytes"
	MetricMemoryLimit  = "container_memory_usage_limit_bytes"
	MetricIORead       = "container_io_rbytes_bytes"
	MetricIOWrite      = "container_io_wbytes_bytes"
	MetricPIDs         = "container_pids_current"
)

// Range is a history period offered by the UI.
type Range struct {
	Name     string
	Duration time.Duration
	// Step is the resolution; Window the rate() window, at least two scrapes long.
	Step   time.Duration
	Window time.Duration
}

// Ranges are the supported periods, about 240–360 points each.
var Ranges = []Range{
	{Name: "1h", Duration: time.Hour, Step: 15 * time.Second, Window: 2 * time.Minute},
	{Name: "6h", Duration: 6 * time.Hour, Step: time.Minute, Window: 4 * time.Minute},
	{Name: "24h", Duration: 24 * time.Hour, Step: 5 * time.Minute, Window: 20 * time.Minute},
	{Name: "7d", Duration: 7 * 24 * time.Hour, Step: 30 * time.Minute, Window: 2 * time.Hour},
}

// RangeByName returns the range named name.
func RangeByName(name string) (Range, bool) {
	i := slices.IndexFunc(Ranges, func(r Range) bool { return r.Name == name })
	if i < 0 {
		return Range{}, false
	}
	return Ranges[i], true
}

// History is the metrics history of one container on a common time axis; a nil value is a
// gap (no data). Keep in sync with api/openapi.yaml (StatsHistory).
type History struct {
	Range string `json:"range"`
	// Step is the distance between points in seconds.
	Step  int64   `json:"step"`
	Times []int64 `json:"times"`
	// CPU in percent of one core, like nerdctl stats.
	CPU []*float64 `json:"cpu"`
	// Memory without inactive file cache (like nerdctl stats), and its limit (null when
	// unlimited).
	Memory      []*float64 `json:"memory"`
	MemoryLimit []*float64 `json:"memoryLimit"`
	// Block I/O rates in bytes per second.
	BlockRead  []*float64 `json:"blockRead"`
	BlockWrite []*float64 `json:"blockWrite"`
	PIDs       []*float64 `json:"pids"`
}

// unlimited is above any real memory limit; cgroup v2 "max" is reported as MaxUint64.
const unlimited = 1 << 62

// Queries returns the PromQL of each history column for a container. id and namespace are
// validated containerd identifiers and quoted, so they cannot change the query.
func Queries(namespace, id string, window time.Duration) (map[string]string, error) {
	if err := identifiers.Validate(id); err != nil {
		return nil, fmt.Errorf("%w: container id: %w", errdefs.ErrInvalidArgument, err)
	}
	if err := identifiers.Validate(namespace); err != nil {
		return nil, fmt.Errorf("%w: namespace: %w", errdefs.ErrInvalidArgument, err)
	}
	sel := fmt.Sprintf(`{container_id=%s,namespace=%s}`, strconv.Quote(id), strconv.Quote(namespace))
	w := fmt.Sprintf("[%ds]", int64(window.Seconds()))
	// max() collapses duplicates when the same containerd is scraped by several jobs;
	// block I/O is summed over devices.
	return map[string]string{
		"cpu":         fmt.Sprintf(`max(rate(%s%s%s)) / 10000`, MetricCPU, sel, w),
		"memory":      fmt.Sprintf(`max(clamp_min(%s%s - %s%s, 0))`, MetricMemory, sel, MetricInactiveFile, sel),
		"memoryLimit": fmt.Sprintf(`max(%s%s < %d)`, MetricMemoryLimit, sel, int64(unlimited)),
		"blockRead":   fmt.Sprintf(`sum(max by (major, minor) (rate(%s%s%s)))`, MetricIORead, sel, w),
		"blockWrite":  fmt.Sprintf(`sum(max by (major, minor) (rate(%s%s%s)))`, MetricIOWrite, sel, w),
		"pids":        fmt.Sprintf(`max(%s%s)`, MetricPIDs, sel),
	}, nil
}

// History queries the metrics of a container over r, ending at end.
func (c *Client) History(ctx context.Context, namespace, id string, r Range, end time.Time) (*History, error) {
	queries, err := Queries(namespace, id, r.Window)
	if err != nil {
		return nil, err
	}
	step := int64(r.Step.Seconds())
	// Align to the step so that consecutive requests return the same points.
	last := end.Unix() / step * step
	first := last - int64(r.Duration.Seconds())
	h := &History{Range: r.Name, Step: step}
	for t := first; t <= last; t += step {
		h.Times = append(h.Times, t)
	}
	cols := map[string]*[]*float64{
		"cpu": &h.CPU, "memory": &h.Memory, "memoryLimit": &h.MemoryLimit,
		"blockRead": &h.BlockRead, "blockWrite": &h.BlockWrite, "pids": &h.PIDs,
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for name, q := range queries {
		wg.Go(func() {
			series, err := c.QueryRange(ctx, q, time.Unix(first, 0), time.Unix(last, 0), r.Step)
			col := make([]*float64, len(h.Times))
			if err == nil {
				for _, s := range series {
					for _, p := range s.Samples {
						i := int(math.Round((p.T - float64(first)) / float64(step)))
						if i >= 0 && i < len(col) && !math.IsNaN(p.V) && !math.IsInf(p.V, 0) {
							col[i] = &p.V
						}
					}
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
			}
			*cols[name] = col
		})
	}
	wg.Wait()
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return h, nil
}

// Check verifies the connection and whether containerd task metrics are scraped: it
// returns the server version ("" if unknown) and the number of containers with CPU metrics.
func (c *Client) Check(ctx context.Context) (version string, containers int, err error) {
	vals, err := c.Query(ctx, fmt.Sprintf(`count(%s) or vector(0)`, MetricCPU))
	if err != nil {
		return "", 0, err
	}
	if len(vals) > 0 {
		containers = int(vals[0])
	}
	return c.Version(ctx), containers, nil
}
