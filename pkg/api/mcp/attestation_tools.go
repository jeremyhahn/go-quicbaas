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

// Package mcp implements Attestation tool definitions for the Model Context
// Protocol. The Attestation tools expose the standalone trusted-computing
// surface behind Security().Attestation(): statement and chain verification
// composed from go-xkms, and the container attestation orchestration seam.
//
// Binary fields cross the MCP boundary base64-encoded, because MCP tool
// arguments are JSON scalars supplied by a model or a CLI, not typed structs.
package mcp

var (
	// AttestationVerifyTool validates a complete attestation statement.
	AttestationVerifyTool = Tool{
		Name: "quicbaas_attestation_verify",
		Description: "Verify a hardware attestation statement: certificate chain, " +
			"signature over the attestation data, and optionally nonce freshness " +
			"and TPM PCR values. Verification is performed by the go-xkms " +
			"attestation verifier",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"format": {
					Type:        "string",
					Description: "Attestation format, e.g. tpm2, pkcs11 or oci-image",
				},
				"backend": {
					Type:        "string",
					Description: "Backend that produced the attestation, e.g. tpm2",
				},
				"chain_pem": {
					Type:        "string",
					Description: "PEM bundle of the attesting key's certificate chain, leaf first",
				},
				"attested_key_der": {
					Type:        "string",
					Description: "Base64-encoded PKIX DER public key that was attested",
				},
				"attestation_data": {
					Type:        "string",
					Description: "Base64-encoded attestation evidence the signature covers",
				},
				"signature": {
					Type:        "string",
					Description: "Base64-encoded signature over the attestation data",
				},
				"signature_algorithm": {
					Type:        "string",
					Description: "x509 signature algorithm name, e.g. ECDSA-SHA256. Empty infers it from the leaf certificate",
				},
				"nonce": {
					Type:        "string",
					Description: "Base64-encoded challenge the attestation was bound to",
				},
				"created_at": {
					Type:        "string",
					Description: "RFC 3339 timestamp the attestation was produced at; required when checking freshness",
				},
				"check_freshness": {
					Type:        "boolean",
					Description: "Enable the nonce and timestamp freshness check",
				},
				"allow_self_signed": {
					Type:        "boolean",
					Description: "Permit a self-signed attesting certificate. Lab and simulator use only",
				},
			},
			Required: []string{"format", "chain_pem", "attested_key_der", "signature"},
		},
	}

	// AttestationVerifyChainTool validates only the certificate chain of an
	// attestation statement.
	AttestationVerifyChainTool = Tool{
		Name: "quicbaas_attestation_verify_chain",
		Description: "Verify only the certificate chain of an attestation statement, " +
			"establishing that an attesting key is genuine without verifying a " +
			"specific attestation signature",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"format": {
					Type:        "string",
					Description: "Attestation format the chain belongs to",
				},
				"chain_pem": {
					Type:        "string",
					Description: "PEM bundle of the certificate chain, leaf first",
				},
				"allow_self_signed": {
					Type:        "boolean",
					Description: "Permit a self-signed certificate in the chain. Lab and simulator use only",
				},
			},
			Required: []string{"chain_pem"},
		},
	}

	// AttestationAttestContainerTool requests container attestation evidence
	// from the registered attestor.
	AttestationAttestContainerTool = Tool{
		Name: "quicbaas_attestation_attest_container",
		Description: "Request attestation evidence for a container: a host TPM quote " +
			"verified against the expected PCR set, plus the launch measurement for " +
			"a measured launch. Returns a not-configured error until a container " +
			"attestor is registered on the server",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"device_id": {
					Type:        "string",
					Description: "Identifier of the host the container runs on",
				},
				"container_id": {
					Type:        "string",
					Description: "Identifier of the running container. Empty attests the host alone",
				},
				"image": {
					Type:        "string",
					Description: "Pinned image reference, repository@sha256:<hex>",
				},
				"image_digest": {
					Type:        "string",
					Description: "Immutable content digest of the image, sha256:<hex>",
				},
				"trust_label": {
					Type:        "string",
					Description: "Operator-assigned workload identity",
				},
				"trust_level": {
					Type:        "string",
					Description: "Required tier: pinned, attested or measured",
				},
				"reference_values": {
					Type:        "string",
					Description: "Name of the stored reference values the host quote is appraised against. Required at attested and above",
				},
				"measurement_pcr": {
					Type:        "integer",
					Description: "PCR index carrying the launch measurement. Zero selects the attestor default",
				},
				"measurement_bank": {
					Type:        "string",
					Description: "PCR bank for the launch measurement: sha256, sha384 or sha512",
				},
			},
			Required: []string{"image"},
		},
	}
)

// getAllAttestationTools returns all Attestation tools (3 tools).
func getAllAttestationTools() []Tool {
	return []Tool{
		AttestationVerifyTool,
		AttestationVerifyChainTool,
		AttestationAttestContainerTool,
	}
}
