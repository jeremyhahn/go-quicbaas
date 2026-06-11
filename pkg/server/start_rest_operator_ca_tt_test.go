// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

//go:build rest

// start_rest_operator_ca_tt_test.go covers the operator-CA error branch of
// startRESTServer: when iam.mtls's CA pool wiring fails (malformed / missing
// PEM), startRESTServer aborts before constructing the REST server. The branch
// is driven via the appendOperatorClientCAsFn seam.

package server

import (
	"crypto/tls"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServer_StartRESTServer_TT_OperatorCAFailureAborts injects an
// appendOperatorClientCAsFn failure so the operator-CA error return of
// startRESTServer runs. A non-nil cloned TLS config (from s.tlsConfig) is
// required for the branch to be reached.
func TestServer_StartRESTServer_TT_OperatorCAFailureAborts(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.REST.Enabled = true
	// A non-nil TLS config makes cloneTLSConfig return a non-nil clone so the
	// operator-CA wiring branch executes.
	srv.tlsConfig = &tls.Config{}

	sentinel := errors.New("operator CA pool malformed")
	orig := appendOperatorClientCAsFn
	appendOperatorClientCAsFn = func(*Server, *tls.Config) error { return sentinel }
	t.Cleanup(func() { appendOperatorClientCAsFn = orig })

	err := srv.startRESTServer()
	require.Error(t, err, "an operator-CA wiring failure must abort startRESTServer")
	assert.ErrorIs(t, err, sentinel, "the operator-CA error must propagate verbatim")
}
