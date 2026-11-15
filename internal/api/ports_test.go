// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pessom/nerdui/internal/runtime"
)

func TestPorts(t *testing.T) {
	rt := &fakeRuntime{ports: runtime.PortsReport{HostChecked: true, Ports: []runtime.PortEntry{
		{Namespace: "default", ContainerID: "a", HostPort: 80, ContainerPort: 80, Protocol: "tcp", Published: true,
			Conflicts: []string{"host process on 0.0.0.0:80/tcp"}},
	}}}
	rec := send(t, newRouter(rt), http.MethodGet, "/api/v1/environments/local/ports", "")
	var got runtime.PortsReport
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || !got.HostChecked || len(got.Ports) != 1 || len(got.Ports[0].Conflicts) != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
