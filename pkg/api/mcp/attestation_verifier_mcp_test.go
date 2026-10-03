//go:build mcp

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

package mcp

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestAttestationVerifierTools_EndToEnd(t *testing.T) {
	store := attestationtest.NewMemoryStore()
	reg := newAttestationRegistry(t, service.NewAttestationService(nil, nil, service.WithAttestationStore(store)))
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	exec := func(tool string, args map[string]interface{}) map[string]interface{} {
		t.Helper()
		out, err := reg.executeAttestationTool(ctx, tool, args)
		require.NoError(t, err, tool)
		return out.(map[string]interface{})
	}

	akCA, err := attestationtest.NewAuthority("AK CA")
	require.NoError(t, err)
	signer, err := attestationtest.NewAuthority("pinned signer")
	require.NoError(t, err)
	pub, err := signer.PublicKeyPEM()
	require.NoError(t, err)
	ak, err := attestationtest.IssueAttestationKey(akCA, true)
	require.NoError(t, err)
	pcrs := attestationtest.PCRValues(0, 7)
	corim, err := attestationtest.SignCoRIM(pcrs, signer.Key, []*x509.Certificate(nil))
	require.NoError(t, err)

	e := exec("quicbaas_attestation_put_endorsement", map[string]interface{}{"name": "ak-ca", "kind": "ak-root", "certificate_pem": string(akCA.CertificatePEM())})
	assert.Equal(t, "ak-root", e["kind"])
	exec("quicbaas_attestation_put_endorsement", map[string]interface{}{"name": "signer", "kind": "reference-signer", "public_key_pem": string(pub)})
	listed := exec("quicbaas_attestation_list_endorsements", map[string]interface{}{})
	assert.Len(t, listed["endorsements"], 2)

	rv := exec("quicbaas_attestation_put_reference_values", map[string]interface{}{"name": "golden", "signed_corim": base64.StdEncoding.EncodeToString(corim)})
	assert.Equal(t, []uint32{0, 7}, rv["pcr_indices"])
	got := exec("quicbaas_attestation_get_reference_values", map[string]interface{}{"name": "golden"})
	assert.Equal(t, base64.StdEncoding.EncodeToString(corim), got["signed_corim"])
	all := exec("quicbaas_attestation_list_reference_values", nil)
	assert.Len(t, all["reference_values"], 1)

	quote, err := attestationtest.SignQuote(ak, pcrs, []byte("n"))
	require.NoError(t, err)
	pcrArg := map[string]interface{}{}
	for idx, v := range pcrs {
		pcrArg[intToDecimal(idx)] = base64.StdEncoding.EncodeToString(v)
	}
	result := exec("quicbaas_attestation_appraise_quote", map[string]interface{}{
		"ak_certificate_pem": string(ak.CertificatePEM()),
		"quote":              base64.StdEncoding.EncodeToString(quote.Attest),
		"signature":          base64.StdEncoding.EncodeToString(quote.Signature),
		"nonce":              base64.StdEncoding.EncodeToString([]byte("n")),
		"pcrs":               pcrArg,
		"reference_values":   "golden",
	})
	assert.Equal(t, true, result["verified"], "%+v", result)
	assert.Equal(t, "signed", result["baseline_source"])

	assert.Equal(t, true, exec("quicbaas_attestation_delete_reference_values", map[string]interface{}{"name": "golden"})["deleted"])
	assert.Equal(t, true, exec("quicbaas_attestation_delete_endorsement", map[string]interface{}{"name": "signer"})["deleted"])
}

func TestAttestationVerifierTools_Errors(t *testing.T) {
	store := attestationtest.NewMemoryStore()
	reg := newAttestationRegistry(t, service.NewAttestationService(nil, nil, service.WithAttestationStore(store)))
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	for tool, args := range map[string]map[string]interface{}{
		"quicbaas_attestation_put_endorsement":         {"name": "x", "kind": "nope"},
		"quicbaas_attestation_delete_endorsement":      {"name": "missing"},
		"quicbaas_attestation_put_reference_values":    {"name": "x", "signed_corim": "!!!"},
		"quicbaas_attestation_get_reference_values":    {"name": "missing"},
		"quicbaas_attestation_delete_reference_values": {"name": "missing"},
		"quicbaas_attestation_appraise_quote":          {"quote": "!!!"},
	} {
		_, err := reg.executeAttestationTool(ctx, tool, args)
		assert.Error(t, err, tool)
	}
	_, err := reg.executeAttestationTool(ctx, "quicbaas_attestation_put_reference_values", map[string]interface{}{"name": "x"})
	assert.Error(t, err, "missing signed_corim")
	_, err = reg.executeAttestationTool(ctx, "quicbaas_attestation_appraise_quote", map[string]interface{}{"pcrs": map[string]interface{}{"zero": "AA=="}})
	assert.Error(t, err, "bad PCR index")
	_, err = reg.executeAttestationTool(ctx, "quicbaas_attestation_appraise_quote", map[string]interface{}{"pcrs": map[string]interface{}{"0": "!!"}})
	assert.Error(t, err, "bad PCR digest")

	store.Fail = errors.New("down")
	for _, tool := range []string{"quicbaas_attestation_list_endorsements", "quicbaas_attestation_list_reference_values"} {
		_, err := reg.executeAttestationTool(ctx, tool, map[string]interface{}{})
		assert.Error(t, err, tool)
	}
	assert.True(t, isAttestationTool("quicbaas_attestation_appraise_quote"))
	assert.False(t, isAttestationTool("quicbaas_attestation_nope"))
}
