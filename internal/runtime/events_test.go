// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package runtime

import (
	"testing"
	"time"

	apievents "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/v2/core/events"
	"github.com/containerd/typeurl/v2"
)

func TestTopicParts(t *testing.T) {
	for topic, want := range map[string][2]string{
		"/containers/create":  {"container", "create"},
		"/tasks/exit":         {"task", "exit"},
		"/tasks/exec-added":   {"task", "exec-added"},
		"/images/delete":      {"image", "delete"},
		"/namespaces/create":  {"namespace", "create"},
		"/snapshot/prepare":   {"snapshot", "prepare"},
		"/sandboxes/create":   {"sandbox", "create"},
		"/content/delete":     {"content", "delete"},
		"/":                   {"", ""},
		"/tasks/checkpointed": {"task", "checkpointed"},
	} {
		k, a := topicParts(topic)
		if k != want[0] || a != want[1] {
			t.Errorf("topicParts(%q) = %q, %q; want %q", topic, k, a, want)
		}
	}
}

func envelope(t *testing.T, topic string, payload any) *events.Envelope {
	t.Helper()
	anyv, err := typeurl.MarshalAny(payload)
	if err != nil {
		t.Fatal(err)
	}
	return &events.Envelope{Timestamp: time.Unix(100, 0), Namespace: "default", Topic: topic, Event: anyv}
}

func TestConvertEnvelope(t *testing.T) {
	tests := []struct {
		topic   string
		payload any
		id      string
		attrs   map[string]string
	}{
		{"/containers/create", &apievents.ContainerCreate{ID: "web", Image: "nginx:1", Runtime: &apievents.ContainerCreate_Runtime{Name: "io.containerd.runc.v2"}},
			"web", map[string]string{"image": "nginx:1", "runtime": "io.containerd.runc.v2"}},
		{"/containers/delete", &apievents.ContainerDelete{ID: "web"}, "web", nil},
		{"/tasks/start", &apievents.TaskStart{ContainerID: "web", Pid: 42}, "web", map[string]string{"pid": "42"}},
		{"/tasks/exit", &apievents.TaskExit{ContainerID: "web", ID: "web", Pid: 42, ExitStatus: 137}, "web", map[string]string{"pid": "42", "exitStatus": "137"}},
		{"/tasks/exit", &apievents.TaskExit{ContainerID: "web", ID: "exec-1", Pid: 7, ExitStatus: 0}, "web", map[string]string{"pid": "7", "exitStatus": "0", "execId": "exec-1"}},
		{"/tasks/oom", &apievents.TaskOOM{ContainerID: "web"}, "web", nil},
		{"/tasks/paused", &apievents.TaskPaused{ContainerID: "web"}, "web", nil},
		{"/images/create", &apievents.ImageCreate{Name: "docker.io/library/alpine:3"}, "docker.io/library/alpine:3", nil},
		{"/namespaces/create", &apievents.NamespaceCreate{Name: "k8s.io"}, "k8s.io", nil},
	}
	for _, tt := range tests {
		ev := convertEnvelope(envelope(t, tt.topic, tt.payload))
		if ev.ID != tt.id || ev.Topic != tt.topic || ev.Namespace != "default" || !ev.Timestamp.Equal(time.Unix(100, 0)) {
			t.Errorf("%s: %+v", tt.topic, ev)
		}
		if len(ev.Attributes) != len(tt.attrs) {
			t.Errorf("%s: attributes %v, want %v", tt.topic, ev.Attributes, tt.attrs)
		}
		for k, v := range tt.attrs {
			if ev.Attributes[k] != v {
				t.Errorf("%s: attribute %s = %q, want %q", tt.topic, k, ev.Attributes[k], v)
			}
		}
	}

	// Unknown or missing payloads still produce topic-based events.
	ev := convertEnvelope(&events.Envelope{Topic: "/snapshot/remove"})
	if ev.Kind != "snapshot" || ev.Action != "remove" || ev.ID != "" {
		t.Errorf("no payload: %+v", ev)
	}
}
