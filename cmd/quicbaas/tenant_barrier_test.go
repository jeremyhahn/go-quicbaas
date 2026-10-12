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

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// fakeTenantBarrierClient records requests so tests can assert what would be
// sent, and returns injected responses.
type fakeTenantBarrierClient struct {
	statusFunc  func(ctx context.Context, req *qrdbtransport.TenantBarrierStatusRequest) (*qrdbtransport.TenantBarrierStatusResponse, error)
	initFunc    func(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error)
	unsealFunc  func(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealRequest) error
	shareFunc   func(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealShareRequest) (*qrdbtransport.TenantBarrierUnsealProgress, error)
	sealFunc    func(ctx context.Context, req *qrdbtransport.TenantBarrierSealRequest) error
	officerFunc func(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error)

	lastStatusReq *qrdbtransport.TenantBarrierStatusRequest
	lastInitReq   *qrdbtransport.TenantBarrierInitRequest
	lastUnsealReq *qrdbtransport.TenantBarrierUnsealRequest
	lastShareReq  *qrdbtransport.TenantBarrierUnsealShareRequest
	lastSealReq   *qrdbtransport.TenantBarrierSealRequest
	lastOfficerID string
}

func (f *fakeTenantBarrierClient) TenantBarrierStatus(ctx context.Context, req *qrdbtransport.TenantBarrierStatusRequest) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	f.lastStatusReq = req
	if f.statusFunc != nil {
		return f.statusFunc(ctx, req)
	}
	return &qrdbtransport.TenantBarrierStatusResponse{TenantID: req.TenantID}, nil
}

func (f *fakeTenantBarrierClient) TenantBarrierInit(ctx context.Context, req *qrdbtransport.TenantBarrierInitRequest) (*qrdbtransport.TenantBarrierInitResponse, error) {
	f.lastInitReq = cloneTenantBarrierInitReq(req)
	if f.initFunc != nil {
		return f.initFunc(ctx, req)
	}
	return &qrdbtransport.TenantBarrierInitResponse{TenantID: req.TenantID}, nil
}

func (f *fakeTenantBarrierClient) TenantBarrierUnseal(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealRequest) error {
	f.lastUnsealReq = req
	if f.unsealFunc != nil {
		return f.unsealFunc(ctx, req)
	}
	return nil
}

func (f *fakeTenantBarrierClient) TenantBarrierUnsealShare(ctx context.Context, req *qrdbtransport.TenantBarrierUnsealShareRequest) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
	f.lastShareReq = req
	if f.shareFunc != nil {
		return f.shareFunc(ctx, req)
	}
	return &qrdbtransport.TenantBarrierUnsealProgress{TenantID: req.TenantID}, nil
}

func (f *fakeTenantBarrierClient) TenantBarrierSeal(ctx context.Context, req *qrdbtransport.TenantBarrierSealRequest) error {
	f.lastSealReq = req
	if f.sealFunc != nil {
		return f.sealFunc(ctx, req)
	}
	return nil
}

func (f *fakeTenantBarrierClient) TenantBarrierOfficerList(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error) {
	f.lastOfficerID = tenantID
	if f.officerFunc != nil {
		return f.officerFunc(ctx, tenantID)
	}
	return nil, nil
}

func (f *fakeTenantBarrierClient) TenantBarrierOfficerClaimStatus(ctx context.Context, tenantID string) ([]*qrdbtransport.OfficerStatus, error) {
	f.lastOfficerID = tenantID
	if f.officerFunc != nil {
		return f.officerFunc(ctx, tenantID)
	}
	return nil, nil
}

// withFakeTenantBarrier installs fc as the active factory and returns a
// restore func.
func withFakeTenantBarrier(fc *fakeTenantBarrierClient) func() {
	orig := tenantBarrierClientFactory
	tenantBarrierClientFactory = func() (TenantBarrierClient, error) { return fc, nil }
	return func() { tenantBarrierClientFactory = orig }
}

// resetTenantBarrierFlags clears package-level flag state between cases.
func resetTenantBarrierFlags() {
	tenantBarrierTenantID = ""
	tenantBarrierStrategy = ""
	tenantBarrierSecret = ""
	tenantBarrierThreshold = 0
	tenantBarrierShares = 0
	tenantBarrierShare = ""
	tenantBarrierOutputFmt = "table"
	barrierAssumeYes = false
}

// TestTenantBarrier_EveryVerbRequiresATenant is the safety property for this
// whole surface. Defaulting to the system tenant would let a destructive verb
// silently hit the wrong barrier — sealing the control plane instead of one
// tenant. Every verb must refuse without --tenant, and must send nothing.
func TestTenantBarrier_EveryVerbRequiresATenant(t *testing.T) {
	verbs := map[string]func(*cobra.Command, []string) error{
		"status":         runTenantBarrierStatusCommand,
		"init":           runTenantBarrierInitCommand,
		"unseal":         runTenantBarrierUnsealCommand,
		"unseal-share":   runTenantBarrierUnsealShareCommand,
		"seal":           runTenantBarrierSealCommand,
		"officer list":   runTenantBarrierOfficerListCommand,
		"officer status": runTenantBarrierOfficerStatusCommand,
	}
	for name, run := range verbs {
		t.Run(name, func(t *testing.T) {
			resetTenantBarrierFlags()
			fc := &fakeTenantBarrierClient{}
			restore := withFakeTenantBarrier(fc)
			defer restore()

			err := run(&cobra.Command{Use: name}, nil)
			require.Error(t, err, "%s must refuse without --tenant", name)

			assert.Nil(t, fc.lastStatusReq)
			assert.Nil(t, fc.lastInitReq)
			assert.Nil(t, fc.lastUnsealReq)
			assert.Nil(t, fc.lastShareReq)
			assert.Nil(t, fc.lastSealReq)
			assert.Empty(t, fc.lastOfficerID)
		})
	}
}

// TestTenantBarrierStatus_ReportsStrategyAndQuorum verifies the operator sees
// which strategy protects the tenant and whether it is hardware-backed —
// the facts that decide who is able to unseal it.
func TestTenantBarrierStatus_ReportsStrategyAndQuorum(t *testing.T) {
	resetTenantBarrierFlags()
	fc := &fakeTenantBarrierClient{
		statusFunc: func(_ context.Context, _ *qrdbtransport.TenantBarrierStatusRequest) (*qrdbtransport.TenantBarrierStatusResponse, error) {
			return &qrdbtransport.TenantBarrierStatusResponse{
				TenantID:        "acme",
				Initialized:     true,
				Sealed:          true,
				Strategy:        "shamir",
				HardwareBacked:  false,
				ShamirThreshold: 2,
				ShamirTotal:     3,
			}, nil
		},
	}
	restore := withFakeTenantBarrier(fc)
	defer restore()

	tenantBarrierTenantID = "acme"
	cmd := &cobra.Command{Use: "status"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runTenantBarrierStatusCommand(cmd, nil))

	require.NotNil(t, fc.lastStatusReq)
	assert.Equal(t, "acme", fc.lastStatusReq.TenantID)

	got := out.String()
	assert.Contains(t, got, "acme")
	assert.Contains(t, got, "shamir")
	assert.Contains(t, got, "2-of-3")
}

// TestTenantBarrierInit_SendsTheStrategy verifies the tenant's chosen strategy
// and its parameters reach the server unchanged.
func TestTenantBarrierInit_SendsTheStrategy(t *testing.T) {
	resetTenantBarrierFlags()
	fc := &fakeTenantBarrierClient{}
	restore := withFakeTenantBarrier(fc)
	defer restore()

	tenantBarrierTenantID = "acme"
	tenantBarrierStrategy = "shamir"
	tenantBarrierThreshold = 2
	tenantBarrierShares = 3

	require.NoError(t, runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil))

	req := fc.lastInitReq
	require.NotNil(t, req)
	assert.Equal(t, "acme", req.TenantID)
	assert.Equal(t, "shamir", req.Strategy)
	assert.Equal(t, 2, req.Threshold)
	assert.Equal(t, 3, req.Shares)
}

// TestTenantBarrierInit_ValidatesLocally verifies unusable combinations are
// refused before a barrier is created. Initializing is not something a tenant
// admin can simply retry differently, so failing early matters.
func TestTenantBarrierInit_ValidatesLocally(t *testing.T) {
	cases := []struct {
		name      string
		strategy  string
		secret    string
		threshold int
		shares    int
	}{
		{"no strategy", "", "", 0, 0},
		{"passphrase without passphrase", "passphrase", "", 0, 0},
		{"shamir threshold below 2", "shamir", "", 1, 3},
		{"shamir shares below threshold", "shamir", "", 3, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetTenantBarrierFlags()
			fc := &fakeTenantBarrierClient{}
			restore := withFakeTenantBarrier(fc)
			defer restore()

			tenantBarrierTenantID = "acme"
			tenantBarrierStrategy = tc.strategy
			tenantBarrierSecret = tc.secret
			tenantBarrierThreshold = tc.threshold
			tenantBarrierShares = tc.shares

			err := runTenantBarrierInitCommand(&cobra.Command{Use: "init"}, nil)
			require.Error(t, err)
			assert.Nil(t, fc.lastInitReq, "an unusable barrier config must not be created")
		})
	}
}

// TestTenantBarrierSeal_RequiresConfirmation is the guard on the one verb here
// that destroys access to data. Sealing is irreversible without the tenant's
// own credentials, so it must not happen on a bare invocation.
func TestTenantBarrierSeal_RequiresConfirmation(t *testing.T) {
	resetTenantBarrierFlags()
	fc := &fakeTenantBarrierClient{}
	restore := withFakeTenantBarrier(fc)
	defer restore()

	tenantBarrierTenantID = "acme"

	err := runTenantBarrierSealCommand(&cobra.Command{Use: "seal"}, nil)
	require.Error(t, err)
	assert.Nil(t, fc.lastSealReq, "seal must not proceed without confirmation")

	// With confirmation it goes through.
	barrierAssumeYes = true
	cmd := &cobra.Command{Use: "seal"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runTenantBarrierSealCommand(cmd, nil))
	require.NotNil(t, fc.lastSealReq)
	assert.Equal(t, "acme", fc.lastSealReq.TenantID)
}

// TestTenantBarrierUnsealShare_ReportsQuorumProgress verifies each officer sees
// how far the quorum has got without any share being echoed back.
func TestTenantBarrierUnsealShare_ReportsQuorumProgress(t *testing.T) {
	resetTenantBarrierFlags()
	fc := &fakeTenantBarrierClient{
		shareFunc: func(_ context.Context, _ *qrdbtransport.TenantBarrierUnsealShareRequest) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
			return &qrdbtransport.TenantBarrierUnsealProgress{
				TenantID:  "acme",
				Required:  3,
				Submitted: 2,
				Complete:  false,
			}, nil
		},
	}
	restore := withFakeTenantBarrier(fc)
	defer restore()

	tenantBarrierTenantID = "acme"
	tenantBarrierShare = "share-secret-value"

	cmd := &cobra.Command{Use: "unseal-share"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runTenantBarrierUnsealShareCommand(cmd, nil))

	got := out.String()
	assert.Contains(t, got, "2 of 3")
	assert.NotContains(t, got, "share-secret-value",
		"a submitted share must never be echoed back to the terminal")
}

// TestTenantBarrierUnseal_RequiresAPassphrase verifies the passphrase path
// fails closed and points at the Shamir verb instead of sending an empty key.
func TestTenantBarrierUnseal_RequiresAPassphrase(t *testing.T) {
	resetTenantBarrierFlags()
	fc := &fakeTenantBarrierClient{}
	restore := withFakeTenantBarrier(fc)
	defer restore()

	tenantBarrierTenantID = "acme"

	err := runTenantBarrierUnsealCommand(&cobra.Command{Use: "unseal"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unseal-share")
	assert.Nil(t, fc.lastUnsealReq)
}

// TestCanonicalSurface_TenantBarrier pins the per-tenant barrier verb set, so
// the tenant-admin surface cannot quietly narrow.
func TestCanonicalSurface_TenantBarrier(t *testing.T) {
	got := subcommandNames(newTenantBarrierCommand())
	for _, verb := range []string{
		"status", "init", "unseal", "unseal-share", "seal", "officer",
	} {
		assert.True(t, got[verb], "`tenant barrier %s` is missing", verb)
	}

	officer := findSubcommand(newTenantBarrierCommand(), "officer")
	require.NotNil(t, officer)
	officerVerbs := subcommandNames(officer)
	for _, verb := range []string{"list", "status"} {
		assert.True(t, officerVerbs[verb], "`tenant barrier officer %s` is missing", verb)
	}
}

// TestTenantBarrierExamples_ResolveThroughTheRealRoot pins #228: the tenant
// barrier group hangs under `iam tenant`, and every example once said
// `quicbaas tenant barrier ...`, a path that does not exist. Each example's
// command words must resolve, through the real cobra root, to a command whose
// path is exactly those words; cobra's Find stops at the deepest match and
// hands the rest back as args, so a wrong path resolves short rather than
// failing, and only the path comparison catches it.
func TestTenantBarrierExamples_ResolveThroughTheRealRoot(t *testing.T) {
	group, _, err := rootCmd.Find([]string{"iam", "tenant", "barrier"})
	require.NoError(t, err)
	require.Equal(t, "quicbaas iam tenant barrier", group.CommandPath())

	var examples int
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, words := range exampleCommandWords(cmd.Long + "\n" + cmd.Example) {
			examples++
			want := "quicbaas " + strings.Join(words, " ")
			found, _, err := rootCmd.Find(words)
			require.NoError(t, err, "%s example %q does not resolve", cmd.CommandPath(), want)
			assert.Equal(t, want, found.CommandPath(),
				"%s example %q names a command that does not exist", cmd.CommandPath(), want)
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(group)

	assert.Positive(t, examples, "no examples were found; the scan no longer reads them")
}

// exampleCommandWords returns, for each example invocation in a command's help
// text, the command words after `quicbaas` up to the first flag. An invocation
// is a line that starts with `quicbaas` or pipes into it; prose that mentions
// a command in passing is not one.
func exampleCommandWords(help string) [][]string {
	var out [][]string
	for _, line := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(line)
		invocation, ok := strings.CutPrefix(trimmed, "quicbaas ")
		if !ok {
			_, invocation, ok = strings.Cut(trimmed, "| quicbaas ")
		}
		if !ok {
			continue
		}
		var words []string
		for _, field := range strings.Fields(invocation) {
			if strings.HasPrefix(field, "-") {
				break
			}
			words = append(words, field)
		}
		out = append(out, words)
	}
	return out
}

// cloneTenantBarrierInitReq copies the borrowed passphrase out of the request.
// See the note on cloneBarrierInitReq: the CLI's buffer is destroyed when the
// command returns, so a retained header faults rather than reading zeros.
func cloneTenantBarrierInitReq(req *qrdbtransport.TenantBarrierInitRequest) *qrdbtransport.TenantBarrierInitRequest {
	if req == nil {
		return nil
	}
	out := *req
	out.Passphrase = bytes.Clone(req.Passphrase)
	return &out
}
