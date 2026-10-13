//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

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

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Operator policy has to REACH the surface it governs.
//
// buildRESTServer projects the daemon config onto restapi.Config field by
// field. A field left out of that projection does not fail to compile and does
// not fail a handler test — the handler simply reads a zero value, and the
// policy is silently off however the operator configured it. That is how
// iam.self_service_registration came to validate at boot and do nothing.
// =============================================================================

// captureRESTConfig replaces the newREST seam with one that records the config
// buildRESTServer produced, so a test can assert on the projection itself.
func captureRESTConfig(t *testing.T, srv *Server) *restapi.Config {
	t.Helper()

	var captured *restapi.Config
	seamMu.Lock()
	newREST = func(_ service.QuicBaaS, cfg *restapi.Config, _ *logger.Logger) (*restapi.Server, error) {
		captured = cfg
		// Stopping here is deliberate: the config projection is complete by the
		// time the constructor is called, and everything buildRESTServer does
		// afterwards needs a fully built server. Returning an error keeps the
		// test on the projection it is about.
		return nil, errCaptureOnly
	}
	seamMu.Unlock()

	//nolint:errcheck // the seam above always returns errCaptureOnly.
	_, _ = srv.buildRESTServer()
	require.NotNil(t, captured, "buildRESTServer did not reach the REST constructor")
	return captured
}

// errCaptureOnly stops buildRESTServer once the config has been captured.
var errCaptureOnly = &captureStopError{}

// captureStopError is the sentinel the capture seam returns.
type captureStopError struct{}

// Error implements error.
func (e *captureStopError) Error() string { return "capture-only seam: stop after config projection" }

// TestBuildRESTServer_CarriesSelfServiceRegistrationPolicy is the regression
// guard for the silent disable.
//
// Every field here is set to a NON-zero value on purpose: a projection that
// drops the field yields the zero value, so asserting against zero-valued
// settings would pass against the bug. The Enabled flag is the one that gates
// the endpoints, and the rest travel with it — an operator who turns
// registration on and gets the wrong default role or isolation mode has the
// same class of problem in a quieter form.
func TestBuildRESTServer_CarriesSelfServiceRegistrationPolicy(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.config.Server.REST.Enabled = true
	srv.config.IAM.SelfServiceRegistration.Enabled = true
	srv.config.IAM.SelfServiceRegistration.RequireAdminApproval = true
	srv.config.IAM.SelfServiceRegistration.RequireEmailVerification = true
	srv.config.IAM.SelfServiceRegistration.DefaultRole = "Operator"
	srv.config.IAM.SelfServiceRegistration.AllowedIsolationModes = []string{"keyspace", "barrier"}

	captured := captureRESTConfig(t, srv)

	assert.True(t, captured.IAMSelfServiceRegistration.Enabled,
		"the REST surface never received iam.self_service_registration.enabled, so "+
			"/api/v1/auth/register/* answers self_registration_disabled however it is configured")
	assert.True(t, captured.IAMSelfServiceRegistration.RequireAdminApproval)
	assert.True(t, captured.IAMSelfServiceRegistration.RequireEmailVerification)
	assert.Equal(t, "Operator", captured.IAMSelfServiceRegistration.DefaultRole)
	assert.Equal(t, []string{"keyspace", "barrier"},
		captured.IAMSelfServiceRegistration.AllowedIsolationModes)
}

// TestBuildRESTServer_LeavesSelfServiceRegistrationOffByDefault is the control:
// the wiring must carry the operator's decision, not force one. An unset
// section still reaches the surface disabled.
func TestBuildRESTServer_LeavesSelfServiceRegistrationOffByDefault(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := createFTServerWithService(t)
	srv.config.Server.REST.Enabled = true

	captured := captureRESTConfig(t, srv)

	assert.False(t, captured.IAMSelfServiceRegistration.Enabled,
		"an unconfigured server must not open self-service registration")
}

// TestBuildRESTServer_CarriesRESTRateLimit pins the server.rest.rate_limit
// projection: the operator's value reaches the REST server, and an omitted
// key resolves to the REST default rather than to "no limit".
func TestBuildRESTServer_CarriesRESTRateLimit(t *testing.T) {
	tests := []struct {
		name      string
		rateLimit int
		want      int
	}{
		{"omitted key keeps the default limit", 0, restapi.DefaultRateLimit},
		{"explicit limit", 250, 250},
		{"opt-out disables", restapi.RateLimitDisabled, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveAndRestoreSeams(t)

			srv := createFTServerWithService(t)
			srv.config.Server.REST.Enabled = true
			srv.config.Server.REST.RateLimit = tt.rateLimit

			captured := captureRESTConfig(t, srv)

			assert.Equal(t, tt.want, captured.EffectiveRateLimit())
		})
	}
}
