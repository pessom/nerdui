// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIJSON(t *testing.T) {
	b, err := OpenAPIJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI == "" || len(doc.Paths) < 20 || doc.Paths["/api/v1/version"]["get"] == nil {
		t.Errorf("spec: openapi %q, %d paths", doc.OpenAPI, len(doc.Paths))
	}
}
