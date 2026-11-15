// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	"github.com/pessom/nerdui/internal/httpjson"
)

// listPorts returns the published and exposed ports of every container of the host, across
// namespaces, with conflicts (#62):
//
//	GET /api/v1/environments/{env}/ports
func (h *Handler) listPorts(w http.ResponseWriter, r *http.Request) {
	report, err := EnvFrom(r.Context()).Runtime.System().Ports(r.Context())
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, report)
}
