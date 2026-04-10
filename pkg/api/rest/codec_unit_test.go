//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package rest

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectPreferredCodec_Unit(t *testing.T) {
	tests := []struct {
		name         string
		queryParam   string
		acceptHeader string
		expected     string
	}{
		{
			name:       "query parameter json",
			queryParam: "json",
			expected:   "json",
		},
		{
			name:       "query parameter yaml",
			queryParam: "yaml",
			expected:   "yaml",
		},
		{
			name:       "query parameter msgpack",
			queryParam: "msgpack",
			expected:   "msgpack",
		},
		{
			name:       "query parameter invalid - fallback to json",
			queryParam: "invalid",
			expected:   "json",
		},
		{
			name:         "accept header json",
			acceptHeader: "application/json",
			expected:     "json",
		},
		{
			name:         "accept header yaml",
			acceptHeader: "application/yaml",
			expected:     "yaml",
		},
		{
			name:         "accept header x-yaml",
			acceptHeader: "application/x-yaml",
			expected:     "yaml",
		},
		{
			name:         "accept header text/yaml",
			acceptHeader: "text/yaml",
			expected:     "yaml",
		},
		{
			name:         "accept header msgpack",
			acceptHeader: "application/msgpack",
			expected:     "msgpack",
		},
		{
			name:         "accept header x-msgpack",
			acceptHeader: "application/x-msgpack",
			expected:     "msgpack",
		},
		{
			name:         "accept header with quality values",
			acceptHeader: "application/json;q=0.9, application/yaml;q=1.0",
			expected:     "json", // First match
		},
		{
			name:     "no preference - default to json",
			expected: "json",
		},
		{
			name:         "unknown accept header - default to json",
			acceptHeader: "text/html",
			expected:     "json",
		},
		{
			name:         "query param overrides accept header",
			queryParam:   "msgpack",
			acceptHeader: "application/json",
			expected:     "msgpack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/test"
			if tt.queryParam != "" {
				url += "?codec=" + tt.queryParam
			}

			req := httptest.NewRequest("GET", url, nil)
			if tt.acceptHeader != "" {
				req.Header.Set("Accept", tt.acceptHeader)
			}

			result := detectPreferredCodec(req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetContentType_Unit(t *testing.T) {
	tests := []struct {
		name      string
		codecName string
		expected  string
	}{
		{
			name:      "json codec",
			codecName: "json",
			expected:  "application/json",
		},
		{
			name:      "yaml codec",
			codecName: "yaml",
			expected:  "application/x-yaml",
		},
		{
			name:      "msgpack codec",
			codecName: "msgpack",
			expected:  "application/msgpack",
		},
		{
			name:      "unknown codec",
			codecName: "unknown",
			expected:  "application/octet-stream",
		},
		{
			name:      "empty codec",
			codecName: "",
			expected:  "application/octet-stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			setContentType(w, tt.codecName)

			result := w.Header().Get("Content-Type")
			assert.Equal(t, tt.expected, result)
		})
	}
}
