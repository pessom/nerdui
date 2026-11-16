// SPDX-FileCopyrightText: 2026 The nerdui Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package api embeds the OpenAPI specification of nerdui's REST API (#87): it is served by the
// server, so the documentation always matches the running version.
package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"go.yaml.in/yaml/v3"
)

// OpenAPIYAML is api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPIYAML []byte

var (
	jsonOnce sync.Once
	jsonSpec []byte
	jsonErr  error
)

// OpenAPIJSON returns the specification as JSON, converted once.
func OpenAPIJSON() ([]byte, error) {
	jsonOnce.Do(func() {
		var doc any
		if err := yaml.Unmarshal(OpenAPIYAML, &doc); err != nil {
			jsonErr = fmt.Errorf("parse openapi.yaml: %w", err)
			return
		}
		jsonSpec, jsonErr = json.Marshal(doc)
	})
	return jsonSpec, jsonErr
}
