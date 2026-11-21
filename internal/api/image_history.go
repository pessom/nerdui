// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	"github.com/pessom/nerdui/internal/httpjson"
)

// imageHistory returns the build steps and layer sizes of an image (#47):
//
//	GET /api/v1/environments/{env}/images/history?namespace=&ref=<image name>
func (h *Handler) imageHistory(w http.ResponseWriter, r *http.Request) {
	ns, err := Namespace(r)
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	list, err := EnvFrom(r.Context()).Runtime.Images().History(r.Context(), ns, r.URL.Query().Get("ref"))
	if err != nil {
		httpjson.WriteErr(w, r, h.Logger, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"history": list})
}
