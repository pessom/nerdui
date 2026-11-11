// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	apievents "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/v2/core/events"
	"github.com/containerd/typeurl/v2"
)

// Event is a containerd event in the API representation (ADR-0007). Keep in sync with
// api/openapi.yaml.
type Event struct {
	Timestamp time.Time `json:"ts"`
	Namespace string    `json:"namespace,omitempty"`
	// Topic is the containerd topic, e.g. "/containers/create" or "/tasks/exit".
	Topic string `json:"topic"`
	// Kind and Action are derived from the topic: "container"/"create", "task"/"exit".
	Kind   string `json:"kind"`
	Action string `json:"action"`
	// ID is the container ID for container and task events, the name for image and
	// namespace events.
	ID         string            `json:"id,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// EventSource streams events of an environment.
type EventSource interface {
	// Subscribe verifies the connection and starts a subscription. A non-nil error means
	// the subscription could not be established. Otherwise events are delivered until ctx
	// ends or the connection breaks; the error channel then receives the reason and both
	// channels are closed.
	Subscribe(ctx context.Context) (<-chan Event, <-chan error, error)
}

// subscribeCheckTimeout bounds the connectivity check before subscribing.
const subscribeCheckTimeout = 5 * time.Second

// Events returns the event source of the local containerd.
func (c *Client) Events() EventSource { return clientEvents{c} }

type clientEvents struct{ c *Client }

func (e clientEvents) Subscribe(ctx context.Context) (<-chan Event, <-chan error, error) {
	cl, err := e.c.conn()
	if err != nil {
		return nil, nil, err
	}
	// The event stream itself does not report a missing daemon promptly, so check first.
	checkCtx, cancel := context.WithTimeout(ctx, subscribeCheckTimeout)
	_, err = cl.Version(checkCtx)
	cancel()
	if err != nil {
		return nil, nil, classify(err)
	}
	out := make(chan Event, 64)
	errc := make(chan error, 1)
	envs, errs := cl.Subscribe(ctx)
	go func() {
		defer close(out)
		defer close(errc)
		for {
			select {
			case env, ok := <-envs:
				if !ok {
					errc <- fmt.Errorf("event subscription closed: %w", ErrNotConnected)
					return
				}
				select {
				case out <- convertEnvelope(env):
				case <-ctx.Done():
					return
				}
			case err, ok := <-errs:
				if !ok || err == nil {
					err = fmt.Errorf("event subscription closed: %w", ErrNotConnected)
				}
				if ctx.Err() == nil {
					errc <- classify(err)
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, errc, nil
}

// convertEnvelope turns a containerd envelope into an Event, decoding the payloads of
// the common container, task, image and namespace events.
func convertEnvelope(env *events.Envelope) Event {
	ev := Event{Timestamp: env.Timestamp.UTC(), Namespace: env.Namespace, Topic: env.Topic}
	ev.Kind, ev.Action = topicParts(env.Topic)
	if env.Event == nil {
		return ev
	}
	payload, err := typeurl.UnmarshalAny(env.Event)
	if err != nil {
		return ev
	}
	attr := map[string]string{}
	u32 := func(k string, v uint32) { attr[k] = strconv.FormatUint(uint64(v), 10) }
	switch p := payload.(type) {
	case *apievents.ContainerCreate:
		ev.ID, attr["image"] = p.ID, p.Image
		if p.Runtime != nil {
			attr["runtime"] = p.Runtime.Name
		}
	case *apievents.ContainerUpdate:
		ev.ID, attr["image"] = p.ID, p.Image
	case *apievents.ContainerDelete:
		ev.ID = p.ID
	case *apievents.TaskCreate:
		ev.ID = p.ContainerID
		u32("pid", p.Pid)
	case *apievents.TaskStart:
		ev.ID = p.ContainerID
		u32("pid", p.Pid)
	case *apievents.TaskDelete:
		ev.ID = p.ContainerID
		u32("pid", p.Pid)
		u32("exitStatus", p.ExitStatus)
	case *apievents.TaskExit:
		ev.ID = p.ContainerID
		u32("pid", p.Pid)
		u32("exitStatus", p.ExitStatus)
		if p.ID != "" && p.ID != p.ContainerID {
			attr["execId"] = p.ID
		}
	case *apievents.TaskOOM:
		ev.ID = p.ContainerID
	case *apievents.TaskExecAdded:
		ev.ID, attr["execId"] = p.ContainerID, p.ExecID
	case *apievents.TaskExecStarted:
		ev.ID, attr["execId"] = p.ContainerID, p.ExecID
		u32("pid", p.Pid)
	case *apievents.TaskPaused:
		ev.ID = p.ContainerID
	case *apievents.TaskResumed:
		ev.ID = p.ContainerID
	case *apievents.ImageCreate:
		ev.ID = p.Name
	case *apievents.ImageUpdate:
		ev.ID = p.Name
	case *apievents.ImageDelete:
		ev.ID = p.Name
	case *apievents.NamespaceCreate:
		ev.ID = p.Name
	case *apievents.NamespaceUpdate:
		ev.ID = p.Name
	case *apievents.NamespaceDelete:
		ev.ID = p.Name
	}
	for k, v := range attr {
		if v == "" {
			delete(attr, k)
		}
	}
	if len(attr) > 0 {
		ev.Attributes = attr
	}
	return ev
}

// topicParts splits "/containers/create" into ("container", "create").
func topicParts(topic string) (kind, action string) {
	parts := strings.Split(strings.Trim(topic, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	kind = parts[0]
	switch kind {
	case "containers", "tasks", "images", "namespaces", "sandboxes", "snapshots":
		kind = strings.TrimSuffix(kind, "s")
		if kind == "sandboxe" {
			kind = "sandbox"
		}
	}
	if len(parts) > 1 {
		action = strings.Join(parts[1:], "/")
	}
	return kind, action
}
