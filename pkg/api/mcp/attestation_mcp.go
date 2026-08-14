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

// Package mcp implements Attestation tool execution handlers for the Model
// Context Protocol. The handlers delegate to the service-layer
// AttestationService, which composes the go-xkms attestation verifier and the
// container attestation orchestration seam.
package mcp

import (
	"context"
	"encoding/base64"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// attestationToolExecutor serves one attestation tool.
type attestationToolExecutor func(r *ToolRegistry, ctx context.Context, svc service.AttestationService, args map[string]interface{}) (interface{}, error)

// attestationExecutors dispatches on the tool name in constant time.
var attestationExecutors = map[string]attestationToolExecutor{
	"quicbaas_attestation_verify":           (*ToolRegistry).handleAttestationVerify,
	"quicbaas_attestation_verify_chain":     (*ToolRegistry).handleAttestationVerifyChain,
	"quicbaas_attestation_attest_container": (*ToolRegistry).handleAttestationAttestContainer,
}

// registerAttestationTools registers all Attestation tool handlers on the
// ToolRegistry.
func (r *ToolRegistry) registerAttestationTools() {
	for _, tool := range getAllAttestationTools() {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// executeAttestationTool executes an Attestation tool and returns the result.
func (r *ToolRegistry) executeAttestationTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	attestationSvc := r.service.Attestation()
	if attestationSvc == nil {
		return nil, api.NewServiceNotConfiguredError("attestation")
	}

	executor, ok := attestationExecutors[name]
	if !ok {
		return nil, api.NewUnknownToolError(name)
	}
	return executor(r, ctx, attestationSvc, args)
}

// handleAttestationVerify handles quicbaas_attestation_verify.
func (r *ToolRegistry) handleAttestationVerify(ctx context.Context, svc service.AttestationService, args map[string]interface{}) (interface{}, error) {
	format := getString(args, "format")
	if format == "" {
		return nil, api.NewRequiredFieldError("format")
	}

	chainPEM := getString(args, "chain_pem")
	if chainPEM == "" {
		return nil, api.NewRequiredFieldError("chain_pem")
	}
	chain := splitPEMBundle([]byte(chainPEM))
	if len(chain) == 0 {
		return nil, api.NewRequiredFieldError("chain_pem")
	}

	attestedKeyDER, err := decodeBase64Arg(args, "attested_key_der")
	if err != nil {
		return nil, err
	}
	if len(attestedKeyDER) == 0 {
		return nil, api.NewRequiredFieldError("attested_key_der")
	}

	signature, err := decodeBase64Arg(args, "signature")
	if err != nil {
		return nil, err
	}
	if len(signature) == 0 {
		return nil, api.NewRequiredFieldError("signature")
	}

	attestationData, err := decodeBase64Arg(args, "attestation_data")
	if err != nil {
		return nil, err
	}
	nonce, err := decodeBase64Arg(args, "nonce")
	if err != nil {
		return nil, err
	}

	resp, err := svc.Verify(ctx, &service.VerifyAttestationRequest{
		Format:             format,
		Backend:            getString(args, "backend"),
		ChainPEM:           chain,
		AttestedKeyDER:     attestedKeyDER,
		AttestationData:    attestationData,
		Signature:          signature,
		SignatureAlgorithm: getString(args, "signature_algorithm"),
		Nonce:              nonce,
		CreatedAt:          getString(args, "created_at"),
		Options: &service.VerifyAttestationOptions{
			CheckFreshness:  getBool(args, "check_freshness"),
			ExpectedNonce:   nonce,
			AllowSelfSigned: getBool(args, "allow_self_signed"),
		},
	})
	if err != nil {
		return nil, api.NewOperationFailedError("attestation verify", err)
	}

	return map[string]interface{}{
		"valid":           resp.Valid,
		"format":          resp.Format,
		"backend":         resp.Backend,
		"chain_valid":     resp.ChainValid,
		"signature_valid": resp.SignatureValid,
		"freshness_valid": resp.FreshnessValid,
		"pcrs_valid":      resp.PCRsValid,
		"reason":          resp.Reason,
	}, nil
}

// handleAttestationVerifyChain handles quicbaas_attestation_verify_chain.
func (r *ToolRegistry) handleAttestationVerifyChain(ctx context.Context, svc service.AttestationService, args map[string]interface{}) (interface{}, error) {
	chainPEM := getString(args, "chain_pem")
	if chainPEM == "" {
		return nil, api.NewRequiredFieldError("chain_pem")
	}
	chain := splitPEMBundle([]byte(chainPEM))
	if len(chain) == 0 {
		return nil, api.NewRequiredFieldError("chain_pem")
	}

	resp, err := svc.VerifyChain(ctx, &service.VerifyAttestationChainRequest{
		Format:   getString(args, "format"),
		ChainPEM: chain,
		Options: &service.VerifyAttestationOptions{
			AllowSelfSigned: getBool(args, "allow_self_signed"),
		},
	})
	if err != nil {
		return nil, api.NewOperationFailedError("attestation verify chain", err)
	}

	return map[string]interface{}{
		"valid":  resp.Valid,
		"reason": resp.Reason,
	}, nil
}

// handleAttestationAttestContainer handles
// quicbaas_attestation_attest_container.
func (r *ToolRegistry) handleAttestationAttestContainer(ctx context.Context, svc service.AttestationService, args map[string]interface{}) (interface{}, error) {
	image := getString(args, "image")
	if image == "" {
		return nil, api.NewRequiredFieldError("image")
	}

	resp, err := svc.AttestContainer(ctx, &service.AttestContainerRequest{
		DeviceID:        getString(args, "device_id"),
		ContainerID:     getString(args, "container_id"),
		Image:           image,
		ImageDigest:     getString(args, "image_digest"),
		TrustLabel:      getString(args, "trust_label"),
		TrustLevel:      getString(args, "trust_level"),
		MeasurementPCR:  getInt(args, "measurement_pcr"),
		MeasurementBank: getString(args, "measurement_bank"),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("attest container", err)
	}

	out := map[string]interface{}{
		"attested":         resp.Attested,
		"device_id":        resp.DeviceID,
		"container_id":     resp.ContainerID,
		"image":            resp.Image,
		"image_digest":     resp.ImageDigest,
		"config_digest":    resp.ConfigDigest,
		"trust_label":      resp.TrustLabel,
		"trust_level":      resp.TrustLevel,
		"measured":         resp.Measured,
		"measurement_pcr":  resp.MeasurementPCR,
		"measurement_bank": resp.MeasurementBank,
		"quoted":           base64.StdEncoding.EncodeToString(resp.Quoted),
		"signature":        base64.StdEncoding.EncodeToString(resp.Signature),
		"nonce":            base64.StdEncoding.EncodeToString(resp.Nonce),
	}
	if !resp.AttestedAt.IsZero() {
		out["attested_at"] = resp.AttestedAt.UTC().Format(time.RFC3339Nano)
	}
	if len(resp.PCRs) > 0 {
		pcrs := make(map[string]string, len(resp.PCRs))
		for index, value := range resp.PCRs {
			pcrs[intToDecimal(index)] = base64.StdEncoding.EncodeToString(value)
		}
		out["pcrs"] = pcrs
	}
	return out, nil
}

// splitPEMBundle splits a PEM bundle into one blob per CERTIFICATE block,
// preserving order. It reuses decodePEMChainToDER (trust_mcp.go) and re-wraps
// each DER blob, since the service layer accepts PEM or bare DER per entry.
func splitPEMBundle(bundle []byte) [][]byte {
	return decodePEMChainToDER(bundle)
}

// intToDecimal renders a PCR index as a decimal map key.
func intToDecimal(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	pos := len(buf)
	for v > 0 {
		pos--
		buf[pos] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[pos:])
}

// isAttestationTool returns true if the tool name is an Attestation tool.
func isAttestationTool(name string) bool {
	_, ok := attestationExecutors[name]
	return ok
}
