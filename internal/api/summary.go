// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/pessom/nerdui/internal/api/ws"
	"github.com/pessom/nerdui/internal/httpjson"
	"github.com/pessom/nerdui/internal/runtime"
	"github.com/pessom/nerdui/internal/stacks"
)

// SummaryResponse is the dashboard of a host (#63). Keep in sync with api/openapi.yaml.
type SummaryResponse struct {
	runtime.Summary
	// Stacks counts stacks with known files and compose projects seen in container labels.
	Stacks int `json:"stacks"`
}

// summary counts the resources of all namespaces and reports disk usage (#63):
//
//	GET /api/v1/environments/{env}/summary?volumeSizes=true
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	sizes, err := boolParam(r.URL.Query().Get("volumeSizes"))
	if err != nil {
		httpjson.WriteError(w, http.StatusBadRequest, httpjson.CodeInvalidArgument, "volumeSizes: "+err.Error())
		return
	}
	env := EnvFrom(r.Context())
	sum, err := env.Runtime.System().Summary(r.Context(), sizes)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	resp := SummaryResponse{Summary: sum}
	if h.Stacks != nil {
		resp.Stacks = h.countStacks(r.Context(), env.Runtime, sum.Namespaces)
	}
	httpjson.Write(w, http.StatusOK, resp)
}

// countStacks counts the defined stacks plus projects of any namespace without known files.
func (h *Handler) countStacks(ctx context.Context, rt runtime.Runtime, nss []runtime.NamespaceSummary) int {
	defs, err := h.Stacks.Definitions(ctx)
	if err != nil {
		return 0
	}
	projects := map[string]bool{}
	for _, ns := range nss {
		list, err := rt.Containers().List(ctx, ns.Name)
		if err != nil {
			continue
		}
		for _, p := range stacks.Unmanaged(defs, list) {
			projects[p] = true
		}
	}
	return len(defs) + len(projects)
}

// TopContainer is a running container's latest sample (#63).
type TopContainer struct {
	ID            string  `json:"id"`
	Name          string  `json:"name,omitempty"`
	Namespace     string  `json:"namespace"`
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryUsage   uint64  `json:"memoryUsage"`
	MemoryPercent float64 `json:"memoryPercent"`
}

// TopStats is one message of the top stream.
type TopStats struct {
	Time    time.Time      `json:"ts"`
	Running int            `json:"running"`
	CPU     []TopContainer `json:"cpu"`
	Memory  []TopContainer `json:"memory"`
}

// topInterval is the sampling and sending interval (a variable for tests).
var topInterval = 5 * time.Second

const topCount = 5

// topStream sends the running containers using most CPU and memory every 5 s while the client
// is connected (#63). Every running container of every namespace is sampled like `nerdctl
// stats`; containers that start later join at the next round.
//
//	GET /api/v1/environments/{env}/stats/stream
func (h *Handler) topStream(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.Accept(w, r, ws.Options{})
	if err != nil {
		return
	}
	rt := EnvFrom(r.Context()).Runtime
	ctx := conn.Context()
	var (
		mu      sync.Mutex
		latest  = map[string]TopContainer{}
		running = map[string]bool{}
		wg      sync.WaitGroup
	)
	defer wg.Wait()
	refresh := func() {
		nss, err := rt.System().Namespaces(ctx)
		if err != nil {
			return
		}
		for _, ns := range nss {
			list, err := rt.Containers().List(ctx, ns)
			if err != nil {
				continue
			}
			for _, c := range list {
				key := ns + "/" + c.ID
				mu.Lock()
				started := running[key]
				if c.State == runtime.StateRunning && !started {
					running[key] = true
				}
				mu.Unlock()
				if c.State != runtime.StateRunning || started {
					continue
				}
				wg.Go(func() {
					_ = rt.Containers().Stats(ctx, ns, c.ID, topInterval, func(s runtime.StatsSample) error {
						mu.Lock()
						latest[key] = TopContainer{
							ID: c.ID, Name: c.Name, Namespace: ns, CPUPercent: s.CPUPercent,
							MemoryUsage: s.MemoryUsage, MemoryPercent: s.MemoryPercent,
						}
						mu.Unlock()
						return ctx.Err()
					})
					mu.Lock()
					delete(running, key)
					delete(latest, key)
					mu.Unlock()
				})
			}
		}
	}
	send := func() bool {
		mu.Lock()
		all := make([]TopContainer, 0, len(latest))
		for _, t := range latest {
			all = append(all, t)
		}
		n := len(running)
		mu.Unlock()
		msg := TopStats{Time: time.Now().UTC(), Running: n, CPU: topBy(all, func(t TopContainer) float64 { return t.CPUPercent })}
		msg.Memory = topBy(all, func(t TopContainer) float64 { return float64(t.MemoryUsage) })
		return conn.SendData(msg)
	}

	refresh()
	tick := time.NewTicker(topInterval)
	defer tick.Stop()
	// The first CPU figures need two samples: send once they exist.
	first := time.NewTimer(topInterval + time.Second)
	defer first.Stop()
	for {
		select {
		case <-ctx.Done():
			return // the client is gone or the server stops
		case <-first.C:
			if !send() {
				return
			}
		case <-tick.C:
			refresh()
			if !send() {
				return
			}
		}
	}
}

func topBy(all []TopContainer, value func(TopContainer) float64) []TopContainer {
	out := append([]TopContainer(nil), all...)
	sort.SliceStable(out, func(i, j int) bool {
		if value(out[i]) != value(out[j]) {
			return value(out[i]) > value(out[j])
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > topCount {
		out = out[:topCount]
	}
	return out
}
