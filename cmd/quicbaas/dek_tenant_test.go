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
	"encoding/json"
	"errors"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// fakeTenantDEKClient implements only the three keyring lifecycle methods;
// any other call panics on the nil embedded interface.
type fakeTenantDEKClient struct {
	quicbaas.QRDBDEKClient

	resp *qrdbtransport.TenantDEKStatusResponse
	err  error

	calls      []string
	tenantIDs  []string
	retireReqs []*qrdbtransport.TenantDEKRetireRequest
}

func (f *fakeTenantDEKClient) TenantDEKStatus(_ context.Context, req *qrdbtransport.TenantDEKRequest) (*qrdbtransport.TenantDEKStatusResponse, error) {
	f.calls = append(f.calls, "status")
	f.tenantIDs = append(f.tenantIDs, req.TenantID)
	return f.resp, f.err
}

func (f *fakeTenantDEKClient) TenantDEKRotate(_ context.Context, req *qrdbtransport.TenantDEKRequest) (*qrdbtransport.TenantDEKStatusResponse, error) {
	f.calls = append(f.calls, "rotate")
	f.tenantIDs = append(f.tenantIDs, req.TenantID)
	return f.resp, f.err
}

func (f *fakeTenantDEKClient) TenantDEKRetire(_ context.Context, req *qrdbtransport.TenantDEKRetireRequest) (*qrdbtransport.TenantDEKStatusResponse, error) {
	f.calls = append(f.calls, "retire")
	f.tenantIDs = append(f.tenantIDs, req.TenantID)
	f.retireReqs = append(f.retireReqs, req)
	return f.resp, f.err
}

func withFakeTenantDEK(t *testing.T, fc *fakeTenantDEKClient, factoryErr error) {
	t.Helper()
	origFactory := sdkTenantDEKClientFactory
	origTimeout := globalTimeout
	origOutput := dekOutputFmt

	sdkTenantDEKClientFactory = func() (quicbaas.QRDBDEKClient, error) {
		if factoryErr != nil {
			return nil, factoryErr
		}
		return fc, nil
	}
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}

	t.Cleanup(func() {
		sdkTenantDEKClientFactory = origFactory
		globalTimeout = origTimeout
		dekOutputFmt = origOutput
		dekTenantRetireVersion = 0
	})
}

func runDEKTenantCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newDEKCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"tenant"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func tenantDEKResp(current uint64) *qrdbtransport.TenantDEKStatusResponse {
	created := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	retired := created.Add(time.Hour)
	resp := &qrdbtransport.TenantDEKStatusResponse{
		TenantID:       "acme",
		Algorithm:      "AES-256-GCM",
		CurrentVersion: current,
	}
	for v := uint64(1); v <= current; v++ {
		ver := qrdbtransport.TenantDEKVersion{Version: v, CreatedAt: created}
		if v < current {
			ver.RetiredAt = &retired
		}
		resp.Versions = append(resp.Versions, ver)
	}
	return resp
}

func TestDEKTenant_Verbs(t *testing.T) {
	clientErr := errors.New("keyring unavailable")
	factoryErr := errors.New("no active context")

	tests := []struct {
		name       string
		args       []string
		resp       *qrdbtransport.TenantDEKStatusResponse
		clientErr  error
		factoryErr error
		wantCalls  []string
		wantErr    string
		check      func(t *testing.T, out string, fc *fakeTenantDEKClient)
	}{
		{
			name:      "status prints json with tenant and versions",
			args:      []string{"status", "--output", "json"},
			resp:      tenantDEKResp(2),
			wantCalls: []string{"status"},
			check: func(t *testing.T, out string, _ *fakeTenantDEKClient) {
				var got qrdbtransport.TenantDEKStatusResponse
				require.NoError(t, json.Unmarshal([]byte(out), &got))
				assert.Equal(t, "acme", got.TenantID)
				assert.Equal(t, uint64(2), got.CurrentVersion)
				require.Len(t, got.Versions, 2)
				assert.NotNil(t, got.Versions[0].RetiredAt)
				assert.Nil(t, got.Versions[1].RetiredAt)
			},
		},
		{
			name:      "status prints the table",
			args:      []string{"status"},
			resp:      tenantDEKResp(2),
			wantCalls: []string{"status"},
			check: func(t *testing.T, out string, _ *fakeTenantDEKClient) {
				assert.Contains(t, out, "acme")
				assert.Contains(t, out, "AES-256-GCM")
				assert.Contains(t, out, "2026-11-01T01:00:00Z")
				assert.Contains(t, out, "-")
			},
		},
		{
			name:      "rotate prints the new current version",
			args:      []string{"rotate", "--output", "yaml"},
			resp:      tenantDEKResp(3),
			wantCalls: []string{"rotate"},
			check: func(t *testing.T, out string, _ *fakeTenantDEKClient) {
				assert.Contains(t, out, "currentversion: 3")
			},
		},
		{
			name:      "retire sends the version",
			args:      []string{"retire", "--version", "3"},
			resp:      tenantDEKResp(4),
			wantCalls: []string{"retire"},
			check: func(t *testing.T, _ string, fc *fakeTenantDEKClient) {
				require.Len(t, fc.retireReqs, 1)
				assert.Equal(t, uint64(3), fc.retireReqs[0].Version)
			},
		},
		{
			name:    "retire without --version fails before any call",
			args:    []string{"retire"},
			wantErr: "version",
		},
		{
			name:    "retire with --version 0 fails before any call",
			args:    []string{"retire", "--version", "0"},
			wantErr: "version",
		},
		{
			name:      "client error surfaces wrapped",
			args:      []string{"rotate"},
			clientErr: clientErr,
			wantCalls: []string{"rotate"},
			wantErr:   "dek tenant rotate: keyring unavailable",
		},
		{
			name:       "factory error surfaces",
			args:       []string{"status"},
			factoryErr: factoryErr,
			wantErr:    "no active context",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeTenantDEKClient{resp: tc.resp, err: tc.clientErr}
			withFakeTenantDEK(t, fc, tc.factoryErr)

			out, err := runDEKTenantCLI(t, tc.args...)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				if tc.clientErr != nil {
					assert.ErrorIs(t, err, tc.clientErr)
				}
				if tc.factoryErr != nil {
					assert.ErrorIs(t, err, tc.factoryErr)
				}
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantCalls, fc.calls)
			for _, id := range fc.tenantIDs {
				assert.Empty(t, id, "the CLI must never name a tenant")
			}
			if tc.check != nil {
				tc.check(t, out, fc)
			}
		})
	}
}

func TestDEKTenant_VerbTree(t *testing.T) {
	cmd := newDEKTenantCommand()
	require.Equal(t, "tenant", cmd.Use)
	got := map[string]bool{}
	for _, c := range cmd.Commands() {
		got[c.Name()] = true
	}
	assert.Equal(t, map[string]bool{"status": true, "rotate": true, "retire": true}, got)
	assert.Nil(t, cmd.PersistentFlags().Lookup("tenant"))
}
