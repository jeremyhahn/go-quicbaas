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
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// fakeTenantDEKClient implements the six tenant keyring methods and records
// each request; any other call panics on the nil embedded interface.
type fakeTenantDEKClient struct {
	quicbaas.QRDBDEKClient

	resp *qrdbtransport.TenantDEKStatusResponse
	err  error

	calls       []string
	tenantIDs   []string
	retireReqs  []*qrdbtransport.TenantDEKRetireRequest
	encryptReqs []*qrdbtransport.TenantDEKEncryptRequest
	decryptReqs []*qrdbtransport.TenantDEKDecryptRequest
	rewrapReqs  []*qrdbtransport.TenantDEKRewrapRequest
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

func (f *fakeTenantDEKClient) TenantDEKEncrypt(_ context.Context, req *qrdbtransport.TenantDEKEncryptRequest) (*qrdbtransport.TenantDEKEncryptResponse, error) {
	f.calls = append(f.calls, "encrypt")
	f.tenantIDs = append(f.tenantIDs, req.TenantID)
	f.encryptReqs = append(f.encryptReqs, req)
	if f.err != nil {
		return nil, f.err
	}
	return &qrdbtransport.TenantDEKEncryptResponse{Ciphertext: append([]byte("ct:"), req.Plaintext...), Version: 2}, nil
}

func (f *fakeTenantDEKClient) TenantDEKDecrypt(_ context.Context, req *qrdbtransport.TenantDEKDecryptRequest) (*qrdbtransport.TenantDEKDecryptResponse, error) {
	f.calls = append(f.calls, "decrypt")
	f.tenantIDs = append(f.tenantIDs, req.TenantID)
	f.decryptReqs = append(f.decryptReqs, req)
	if f.err != nil {
		return nil, f.err
	}
	return &qrdbtransport.TenantDEKDecryptResponse{Plaintext: []byte("card 4111"), Version: 1}, nil
}

func (f *fakeTenantDEKClient) TenantDEKRewrap(_ context.Context, req *qrdbtransport.TenantDEKRewrapRequest) (*qrdbtransport.TenantDEKRewrapResponse, error) {
	f.calls = append(f.calls, "rewrap")
	f.tenantIDs = append(f.tenantIDs, req.TenantID)
	f.rewrapReqs = append(f.rewrapReqs, req)
	if f.err != nil {
		return nil, f.err
	}
	return &qrdbtransport.TenantDEKRewrapResponse{Ciphertext: []byte("ct2"), FromVersion: 1, ToVersion: 2}, nil
}

// TestDEKTenant_DataVerbs covers encrypt, decrypt and rewrap: the request
// each sends, its printed result, and its refusals and failures.
func TestDEKTenant_DataVerbs(t *testing.T) {
	clientErr := errors.New("keyring unavailable")
	ct := base64.StdEncoding.EncodeToString([]byte("ct:x"))

	tests := []struct {
		name      string
		args      []string
		clientErr error
		wantCalls []string
		wantErr   string
		check     func(t *testing.T, out string, fc *fakeTenantDEKClient)
	}{
		{
			name:      "encrypt sends plaintext and aad and prints base64 ciphertext",
			args:      []string{"encrypt", "--plaintext", "card 4111", "--aad", "order-42"},
			wantCalls: []string{"encrypt"},
			check: func(t *testing.T, out string, fc *fakeTenantDEKClient) {
				require.Len(t, fc.encryptReqs, 1)
				assert.Equal(t, []byte("card 4111"), fc.encryptReqs[0].Plaintext)
				assert.Equal(t, []byte("order-42"), fc.encryptReqs[0].AAD)
				assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("ct:card 4111"))+"\n", out)
			},
		},
		{
			name:      "encrypt prints json with version",
			args:      []string{"encrypt", "--plaintext", "x", "--output", "json"},
			wantCalls: []string{"encrypt"},
			check: func(t *testing.T, out string, fc *fakeTenantDEKClient) {
				assert.Empty(t, fc.encryptReqs[0].AAD, "no --aad sends no associated data")
				var got map[string]any
				require.NoError(t, json.Unmarshal([]byte(out), &got))
				assert.Equal(t, ct, got["ciphertext"])
				assert.EqualValues(t, 2, got["version"])
			},
		},
		{
			name:      "encrypt accepts empty plaintext when given",
			args:      []string{"encrypt", "--plaintext", ""},
			wantCalls: []string{"encrypt"},
		},
		{
			name:    "encrypt without --plaintext fails before any call",
			args:    []string{"encrypt"},
			wantErr: "plaintext",
		},
		{
			name:      "decrypt sends decoded ciphertext and prints plaintext",
			args:      []string{"decrypt", "--ciphertext", ct, "--aad", "order-42"},
			wantCalls: []string{"decrypt"},
			check: func(t *testing.T, out string, fc *fakeTenantDEKClient) {
				require.Len(t, fc.decryptReqs, 1)
				assert.Equal(t, []byte("ct:x"), fc.decryptReqs[0].Ciphertext)
				assert.Equal(t, []byte("order-42"), fc.decryptReqs[0].AAD)
				assert.Equal(t, "card 4111\n", out)
			},
		},
		{
			name:      "decrypt prints yaml",
			args:      []string{"decrypt", "--ciphertext", ct, "--output", "yaml"},
			wantCalls: []string{"decrypt"},
			check: func(t *testing.T, out string, _ *fakeTenantDEKClient) {
				assert.Contains(t, out, "plaintext: card 4111")
				assert.Contains(t, out, "version: 1")
			},
		},
		{
			name:    "decrypt without --ciphertext fails before any call",
			args:    []string{"decrypt"},
			wantErr: "ciphertext",
		},
		{
			name:    "decrypt with non-base64 ciphertext fails before any call",
			args:    []string{"decrypt", "--ciphertext", "not base64!"},
			wantErr: "base64",
		},
		{
			name:      "rewrap sends decoded ciphertext and prints the new one",
			args:      []string{"rewrap", "--ciphertext", ct, "--output", "json"},
			wantCalls: []string{"rewrap"},
			check: func(t *testing.T, out string, fc *fakeTenantDEKClient) {
				require.Len(t, fc.rewrapReqs, 1)
				assert.Equal(t, []byte("ct:x"), fc.rewrapReqs[0].Ciphertext)
				var got map[string]any
				require.NoError(t, json.Unmarshal([]byte(out), &got))
				assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("ct2")), got["ciphertext"])
				assert.EqualValues(t, 1, got["from_version"])
				assert.EqualValues(t, 2, got["to_version"])
			},
		},
		{
			name:    "rewrap with non-base64 ciphertext fails before any call",
			args:    []string{"rewrap", "--ciphertext", "%%%"},
			wantErr: "base64",
		},
		{
			name:      "encrypt client error surfaces wrapped",
			args:      []string{"encrypt", "--plaintext", "x"},
			clientErr: clientErr,
			wantCalls: []string{"encrypt"},
			wantErr:   "dek tenant encrypt: keyring unavailable",
		},
		{
			name:      "decrypt client error surfaces wrapped",
			args:      []string{"decrypt", "--ciphertext", ct},
			clientErr: clientErr,
			wantCalls: []string{"decrypt"},
			wantErr:   "dek tenant decrypt: keyring unavailable",
		},
		{
			name:      "rewrap client error surfaces wrapped",
			args:      []string{"rewrap", "--ciphertext", ct},
			clientErr: clientErr,
			wantCalls: []string{"rewrap"},
			wantErr:   "dek tenant rewrap: keyring unavailable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := &fakeTenantDEKClient{err: tc.clientErr}
			withFakeTenantDEK(t, fc, nil)

			out, err := runDEKTenantCLI(t, tc.args...)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				if tc.clientErr != nil {
					assert.ErrorIs(t, err, tc.clientErr)
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

func TestDEKTenant_DataVerbsFactoryError(t *testing.T) {
	factoryErr := errors.New("no active context")
	for _, args := range [][]string{
		{"encrypt", "--plaintext", "x"},
		{"decrypt", "--ciphertext", "eA=="},
		{"rewrap", "--ciphertext", "eA=="},
	} {
		t.Run(args[0], func(t *testing.T) {
			withFakeTenantDEK(t, &fakeTenantDEKClient{}, factoryErr)
			_, err := runDEKTenantCLI(t, args...)
			require.Error(t, err)
			assert.ErrorIs(t, err, factoryErr)
		})
	}
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
		dekTenantPlaintext = ""
		dekTenantCiphertext = ""
		dekTenantAAD = ""
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
	assert.Equal(t, map[string]bool{"status": true, "rotate": true, "retire": true, "encrypt": true, "decrypt": true, "rewrap": true}, got)
	assert.Nil(t, cmd.PersistentFlags().Lookup("tenant"))
}
