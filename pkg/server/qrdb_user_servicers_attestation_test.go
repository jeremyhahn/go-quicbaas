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
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// recordingAuthFactorServicer captures what the proxy forwards to qrdb.
type recordingAuthFactorServicer struct {
	qrdbsdk.UserAuthFactorServicer
	added   []*qrdbsdk.AuthFactor
	rotated []*qrdbsdk.AuthFactor
}

func (r *recordingAuthFactorServicer) Add(_ context.Context, _ uint64, factor *qrdbsdk.AuthFactor) error {
	r.added = append(r.added, factor)
	return nil
}

func (r *recordingAuthFactorServicer) Rotate(_ context.Context, _ string, factor *qrdbsdk.AuthFactor) error {
	r.rotated = append(r.rotated, factor)
	return nil
}

// newRecordingProxy wires the proxy over a recording servicer.
func newRecordingProxy(rec *recordingAuthFactorServicer) *qrdbUserAuthFactorServicerProxy {
	return &qrdbUserAuthFactorServicerProxy{
		resolve: func() qrdbsdk.UserAuthFactorServicer { return rec },
	}
}

// TestAuthFactorProxy_RefusesCallerSuppliedAttestation is the bypass guard on
// the RAW auth-factor surfaces.
//
// The gRPC UserAuthFactorService and the MCP qrdb_user_authfactor_* tools both
// write through this proxy, and neither runs an enrollment ceremony. A caller
// holding user_authfactors:add could otherwise persist attest_level=hardware on
// a factor nothing verified — which login policy would then trust.
func TestAuthFactorProxy_RefusesCallerSuppliedAttestation(t *testing.T) {
	for _, field := range []string{iam.AuthFactorMetadataAttestLevel, iam.AuthFactorMetadataAttestAAGUID} {
		t.Run(field, func(t *testing.T) {
			rec := &recordingAuthFactorServicer{}
			proxy := newRecordingProxy(rec)
			factor := &qrdbsdk.AuthFactor{
				Type:     qrdbsdk.AuthFactorFIDO2,
				Metadata: map[string]string{field: "hardware"},
			}

			addErr := proxy.Add(context.Background(), 42, factor)
			if addErr == nil {
				t.Fatalf("Add accepted a caller-supplied %s", field)
			}
			var typed *iam.AuthFactorAttestationMetadataError
			if !errors.As(addErr, &typed) {
				t.Fatalf("want *iam.AuthFactorAttestationMetadataError, got %T", addErr)
			}
			if len(rec.added) != 0 {
				t.Error("a refused Add still reached qrdb")
			}

			// Rotate writes a new factor and must enforce the same contract; a
			// rule on Add alone would move the hole one verb along.
			if err := proxy.Rotate(context.Background(), "old", factor); err == nil {
				t.Fatalf("Rotate accepted a caller-supplied %s", field)
			}
			if len(rec.rotated) != 0 {
				t.Error("a refused Rotate still reached qrdb")
			}
		})
	}
}

// TestAuthFactorProxy_ForwardsMetadataWithoutStamping pins the post-v0.16.2
// contract: the proxy forwards a directly-written factor's metadata untouched
// and stamps nothing, because the node service itself records
// attest_level=none on every plain Add/Rotate and refuses any caller — this
// proxy included — that writes into the attest_* namespace.
func TestAuthFactorProxy_ForwardsMetadataWithoutStamping(t *testing.T) {
	rec := &recordingAuthFactorServicer{}
	proxy := newRecordingProxy(rec)

	factor := &qrdbsdk.AuthFactor{
		Type:     qrdbsdk.AuthFactorFIDO2,
		Metadata: map[string]string{"tenant_id": "acme"},
	}
	if err := proxy.Add(context.Background(), 42, factor); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(rec.added) != 1 {
		t.Fatalf("want 1 forwarded factor, got %d", len(rec.added))
	}
	meta := rec.added[0].Metadata
	if got, present := meta[iam.AuthFactorMetadataAttestLevel]; present {
		t.Errorf("the proxy stamped %s = %q; the node service owns that key", iam.AuthFactorMetadataAttestLevel, got)
	}
	if meta["tenant_id"] != "acme" {
		t.Errorf("existing metadata was altered: %+v", meta)
	}

	// A factor with no metadata at all is forwarded with none.
	bare := &qrdbsdk.AuthFactor{Type: qrdbsdk.AuthFactorFIDO2}
	if err := proxy.Add(context.Background(), 42, bare); err != nil {
		t.Fatalf("Add(bare): %v", err)
	}
	if len(rec.added[1].Metadata) != 0 {
		t.Errorf("bare factor gained metadata: %+v", rec.added[1].Metadata)
	}
}

// TestAuthFactorProxy_NilFactorIsDelegated leaves nil validation to the
// underlying service so the caller sees its canonical error rather than one
// invented here.
func TestAuthFactorProxy_NilFactorIsDelegated(t *testing.T) {
	rec := &recordingAuthFactorServicer{}
	proxy := newRecordingProxy(rec)

	if err := proxy.Add(context.Background(), 42, nil); err != nil {
		t.Fatalf("Add(nil) should delegate, got %v", err)
	}
	if len(rec.added) != 1 {
		t.Error("a nil factor was not delegated to the underlying service")
	}
}

// TestPIVLoginModuleEnabled is the "one PIV path, not two" guard.
//
// go-xkms's composed module serves its own PIV login at
// /api/v1/auth/login/piv and cannot verify a smartcard attestation. Mounting it
// beside the hardened handler at /api/v1/iam/login/piv would make the
// attestation requirement bypassable by choosing the other URL — an attacker
// picks the door. So it is mounted only where the operator has explicitly
// accepted unattested PIV login.
func TestPIVLoginModuleEnabled(t *testing.T) {
	for _, tc := range []struct {
		name            string
		enabled         bool
		allowUnattested bool
		want            bool
	}{
		{"piv login disabled", false, false, false},
		{"piv login disabled, unattested allowed", false, true, false},
		{"attested deployment does not mount the unverifiable path", true, false, false},
		{"unattested deployment may mount it", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
			if err != nil {
				t.Fatalf("logger: %v", err)
			}
			s := &Server{config: &config.Config{}, logger: log}
			s.config.IAM.PIVLogin.Enabled = tc.enabled
			s.config.IAM.PIVLogin.AllowUnattested = tc.allowUnattested

			if got := s.pivLoginModuleEnabled(); got != tc.want {
				t.Errorf("pivLoginModuleEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
