// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/runtime/restart"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/nerdctl/v2/pkg/clientutil"
	"github.com/containerd/nerdctl/v2/pkg/containerutil"
	"github.com/containerd/nerdctl/v2/pkg/formatter"
	"github.com/containerd/nerdctl/v2/pkg/healthcheck"
	"github.com/containerd/nerdctl/v2/pkg/labels"
	"github.com/containerd/nerdctl/v2/pkg/portutil"
)

// Container states (API).
const (
	StateCreated    = "created"
	StateRunning    = "running"
	StatePaused     = "paused"
	StateRestarting = "restarting"
	StateExited     = "exited"
	StateUnknown    = "unknown"
)

// Container is the list representation of a container. Keep in sync with api/openapi.yaml.
type Container struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Image     string `json:"image"`
	Command   string `json:"command"`
	// State is one of created, running, paused, restarting, exited, unknown.
	State string `json:"state"`
	// Health is healthy, unhealthy or starting when a healthcheck is configured.
	Health    string     `json:"health,omitempty"`
	ExitCode  *int       `json:"exitCode,omitempty"`
	ExitedAt  *time.Time `json:"exitedAt,omitempty"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	Ports     []Port     `json:"ports"`
	// Compose is set for containers that belong to a compose project (ADR-0008).
	Compose  *ComposeRef       `json:"compose,omitempty"`
	Platform string            `json:"platform,omitempty"`
	Runtime  string            `json:"runtime,omitempty"`
	Labels   map[string]string `json:"labels"`
}

// Port is a published port.
type Port struct {
	HostIP        string `json:"hostIp,omitempty"`
	HostPort      int32  `json:"hostPort"`
	ContainerPort int32  `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

// ComposeRef links a container to its compose project and service.
type ComposeRef struct {
	Project string `json:"project"`
	Service string `json:"service,omitempty"`
}

// Containers covers container operations of an environment.
type Containers interface {
	// List returns all containers of a namespace (any state), newest first.
	List(ctx context.Context, namespace string) ([]Container, error)
}

// Containers returns container operations of the local containerd.
func (c *Client) Containers() Containers { return clientContainers{c} }

type clientContainers struct{ c *Client }

// listConcurrency bounds parallel per-container lookups (task status, spec, ports).
const listConcurrency = 16

func (cc clientContainers) List(ctx context.Context, namespace string) ([]Container, error) {
	cl, err := cc.c.conn()
	if err != nil {
		return nil, err
	}
	ctx = namespaces.WithNamespace(ctx, namespace)
	list, err := cl.Containers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", classify(err))
	}
	dataStore, err := cc.c.dataStore()
	if err != nil {
		return nil, err
	}

	out := make([]Container, len(list))
	keep := make([]bool, len(list))
	sem := make(chan struct{}, listConcurrency)
	var wg sync.WaitGroup
	for i, c := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			item, err := describe(ctx, c, namespace, dataStore)
			if err != nil {
				return // removed while listing, or unreadable: skip like nerdctl ps
			}
			out[i], keep[i] = item, true
		}()
	}
	wg.Wait()

	result := make([]Container, 0, len(out))
	for i := range out {
		if keep[i] {
			result = append(result, out[i])
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

// describe builds the API view of one container from the same sources nerdctl ps uses.
func describe(ctx context.Context, c containerd.Container, namespace, dataStore string) (Container, error) {
	info, err := c.Info(ctx, containerd.WithoutRefreshedMetadata)
	if err != nil {
		return Container{}, err
	}
	item := Container{
		ID:        c.ID(),
		Name:      containerutil.GetContainerName(info.Labels),
		Namespace: namespace,
		Image:     info.Image,
		CreatedAt: info.CreatedAt.UTC(),
		Platform:  info.Labels[labels.Platform],
		Runtime:   info.Runtime.Name,
		Labels:    info.Labels,
		Ports:     []Port{},
	}
	if item.Labels == nil {
		item.Labels = map[string]string{}
	}
	if spec, err := c.Spec(ctx); err == nil {
		item.Command = formatter.InspectContainerCommand(spec, false, false)
	}
	if p := info.Labels[labels.ComposeProject]; p != "" {
		item.Compose = &ComposeRef{Project: p, Service: info.Labels[labels.ComposeService]}
	}
	if mappings, err := portutil.LoadPortMappings(dataStore, namespace, c.ID(), info.Labels); err == nil {
		for _, m := range mappings {
			item.Ports = append(item.Ports, Port{
				HostIP: m.HostIP, HostPort: m.HostPort, ContainerPort: m.ContainerPort, Protocol: m.Protocol,
			})
		}
	}
	item.State, item.ExitCode, item.ExitedAt, item.StartedAt = taskState(ctx, c, info.Labels)
	if item.State == StateRunning && info.Labels[labels.HealthState] != "" {
		if hs, err := healthcheck.HealthStateFromJSON(info.Labels[labels.HealthState]); err == nil {
			item.Health = hs.Status
		}
	}
	return item, nil
}

// taskState mirrors nerdctl's formatter.ContainerStatus, returning structured values.
func taskState(ctx context.Context, c containerd.Container, lbls map[string]string) (state string, exitCode *int, exitedAt, startedAt *time.Time) {
	task, err := c.Task(ctx, nil)
	if err != nil {
		// No task: never started, or the task was deleted after exiting (CRI behavior).
		if errdefs.IsNotFound(err) {
			return StateCreated, nil, nil, nil
		}
		return StateUnknown, nil, nil, nil
	}
	st, err := task.Status(ctx)
	if err != nil {
		return StateUnknown, nil, nil, nil
	}
	switch st.Status {
	case containerd.Running:
		return StateRunning, nil, nil, processStartTime(task.Pid())
	case containerd.Paused, containerd.Pausing:
		return StatePaused, nil, nil, processStartTime(task.Pid())
	case containerd.Created:
		return StateCreated, nil, nil, nil
	case containerd.Stopped:
		code := int(st.ExitStatus)
		var at *time.Time
		if !st.ExitTime.IsZero() {
			t := st.ExitTime.UTC()
			at = &t
		}
		if lbls[restart.StatusLabel] == string(containerd.Running) && restart.Reconcile(st, lbls) {
			return StateRestarting, &code, at, nil
		}
		return StateExited, &code, at, nil
	default:
		return StateUnknown, nil, nil, nil
	}
}

// dataStore is nerdctl's per-address state directory (names, ports, logs).
func (c *Client) dataStore() (string, error) {
	return clientutil.DataStore(c.nerdctlDataRoot, c.address)
}
