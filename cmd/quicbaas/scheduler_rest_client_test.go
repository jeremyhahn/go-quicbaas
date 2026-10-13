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

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetSchedulerLogLevel_Default(t *testing.T) {
	origLevel := logLevel
	defer func() { logLevel = origLevel }()

	logLevel = ""
	result := getSchedulerLogLevel()
	assert.Equal(t, "info", result)
}

func TestGetSchedulerLogLevel_Custom(t *testing.T) {
	origLevel := logLevel
	defer func() { logLevel = origLevel }()

	logLevel = "debug"
	result := getSchedulerLogLevel()
	assert.Equal(t, "debug", result)
}

func TestConstructSchedulerRESTBaseURL_PlainHTTP(t *testing.T) {
	origTLS := tlsEnabled
	defer func() { tlsEnabled = origTLS }()

	tlsEnabled = false
	url := constructSchedulerRESTBaseURL("localhost:8080")
	assert.Equal(t, "http://localhost:8080", url)
}

func TestConstructSchedulerRESTBaseURL_HTTPS(t *testing.T) {
	origTLS := tlsEnabled
	defer func() { tlsEnabled = origTLS }()

	tlsEnabled = true
	url := constructSchedulerRESTBaseURL("localhost:8080")
	assert.Equal(t, "https://localhost:8080", url)
}

func TestConstructSchedulerRESTBaseURL_WithScheme(t *testing.T) {
	url := constructSchedulerRESTBaseURL("http://already-has-scheme:8080")
	assert.Equal(t, "http://already-has-scheme:8080", url)
}

func TestConstructSchedulerRESTBaseURL_WithHTTPSScheme(t *testing.T) {
	url := constructSchedulerRESTBaseURL("https://secure:8443")
	assert.Equal(t, "https://secure:8443", url)
}
