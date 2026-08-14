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

// attestation_handler.go transforms the Verify, VerifyChain and
// AttestContainer requests and responses behind Security().Attestation() for
// the MCP transport.
//
// The quicbaasd MCP tool schema is JSON with string-typed properties, so
// binary arguments travel base64-encoded and certificate chains travel as a
// single concatenated PEM bundle rather than an array of blobs.

package mcp

import (
	"encoding/base64"
	"encoding/pem"
	"strings"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// attestationServiceName is the SDK service identifier for the Attestation
// surface.
const attestationServiceName = attestationclient.ServiceName

// isMCPAttestationService reports whether the service name targets the
// Attestation surface.
func isMCPAttestationService(service string) bool {
	return strings.HasPrefix(service, attestationServiceName) || service == "Attestation"
}

// attestationHandler adapts the leaf attestation types to the MCP tool
// argument and result shapes.
type attestationHandler struct{}

// newAttestationHandler creates an attestation MCP handler. It carries no
// state — every transformation is a pure function of its input — so the zero
// value is complete.
func newAttestationHandler() *attestationHandler { return &attestationHandler{} }

// transformRequest converts an SDK attestation request into MCP tool
// arguments.
func (h *attestationHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	switch method {
	case "Verify":
		return h.transformVerifyRequest(req)
	case "VerifyChain":
		return h.transformVerifyChainRequest(req)
	case "AttestContainer":
		return h.transformAttestContainerRequest(req)
	default:
		return nil, errors.NewUnimplemented("mcp.attestation."+method,
			"method not exposed by the Attestation MCP tool set")
	}
}

// transformResponse converts an MCP tool result into the leaf attestation
// response type.
func (h *attestationHandler) transformResponse(method string, result interface{}) (interface{}, error) {
	switch method {
	case "Verify":
		return h.transformVerifyResponse(result)
	case "VerifyChain":
		return h.transformVerifyChainResponse(result)
	case "AttestContainer":
		return h.transformAttestContainerResponse(result)
	default:
		return nil, errors.NewUnimplemented("mcp.attestation."+method,
			"method not exposed by the Attestation MCP tool set")
	}
}

func (h *attestationHandler) transformVerifyRequest(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*attestationclient.VerifyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.attestation.Verify",
			"invalid request type, expected *attestation.VerifyRequest")
	}
	if r.Format == "" {
		return nil, errors.NewInvalidInput("mcp.attestation.Verify", "format is required")
	}
	if len(r.ChainPEM) == 0 {
		return nil, errors.NewInvalidInput("mcp.attestation.Verify", "chain is required")
	}

	args := map[string]interface{}{
		"format":              r.Format,
		"backend":             r.Backend,
		"chain_pem":           joinPEMBundle(r.ChainPEM),
		"attested_key_der":    base64.StdEncoding.EncodeToString(r.AttestedKeyDER),
		"attestation_data":    base64.StdEncoding.EncodeToString(r.AttestationData),
		"signature":           base64.StdEncoding.EncodeToString(r.Signature),
		"signature_algorithm": r.SignatureAlgorithm,
		"created_at":          r.CreatedAt,
	}
	if len(r.Nonce) > 0 {
		args["nonce"] = base64.StdEncoding.EncodeToString(r.Nonce)
	}
	if r.Options != nil {
		args["check_freshness"] = r.Options.CheckFreshness
		args["allow_self_signed"] = r.Options.AllowSelfSigned
	}
	return args, nil
}

func (h *attestationHandler) transformVerifyChainRequest(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*attestationclient.VerifyChainRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.attestation.VerifyChain",
			"invalid request type, expected *attestation.VerifyChainRequest")
	}
	if len(r.ChainPEM) == 0 {
		return nil, errors.NewInvalidInput("mcp.attestation.VerifyChain", "chain is required")
	}

	args := map[string]interface{}{
		"format":    r.Format,
		"chain_pem": joinPEMBundle(r.ChainPEM),
	}
	if r.Options != nil {
		args["allow_self_signed"] = r.Options.AllowSelfSigned
	}
	return args, nil
}

func (h *attestationHandler) transformAttestContainerRequest(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*attestationclient.AttestContainerRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.attestation.AttestContainer",
			"invalid request type, expected *attestation.AttestContainerRequest")
	}
	if r.Image == "" {
		return nil, errors.NewInvalidInput("mcp.attestation.AttestContainer", "image is required")
	}

	return map[string]interface{}{
		"device_id":        r.DeviceID,
		"container_id":     r.ContainerID,
		"image":            r.Image,
		"image_digest":     r.ImageDigest,
		"trust_label":      r.TrustLabel,
		"trust_level":      r.TrustLevel,
		"measurement_pcr":  r.MeasurementPCR,
		"measurement_bank": r.MeasurementBank,
	}, nil
}

func (h *attestationHandler) transformVerifyResponse(result interface{}) (interface{}, error) {
	m, err := asResultMap(result, "mcp.attestation.Verify")
	if err != nil {
		return nil, err
	}
	return &attestationclient.VerifyResponse{
		Valid:          getResultBool(m, "valid"),
		Format:         getResultString(m, "format"),
		Backend:        getResultString(m, "backend"),
		ChainValid:     getResultBool(m, "chain_valid"),
		SignatureValid: getResultBool(m, "signature_valid"),
		FreshnessValid: getResultBool(m, "freshness_valid"),
		PCRsValid:      getResultBool(m, "pcrs_valid"),
		Reason:         getResultString(m, "reason"),
	}, nil
}

func (h *attestationHandler) transformVerifyChainResponse(result interface{}) (interface{}, error) {
	m, err := asResultMap(result, "mcp.attestation.VerifyChain")
	if err != nil {
		return nil, err
	}
	return &attestationclient.VerifyChainResponse{
		Valid:  getResultBool(m, "valid"),
		Reason: getResultString(m, "reason"),
	}, nil
}

func (h *attestationHandler) transformAttestContainerResponse(result interface{}) (interface{}, error) {
	m, err := asResultMap(result, "mcp.attestation.AttestContainer")
	if err != nil {
		return nil, err
	}

	resp := &attestationclient.AttestContainerResponse{
		Attested:        getResultBool(m, "attested"),
		DeviceID:        getResultString(m, "device_id"),
		ContainerID:     getResultString(m, "container_id"),
		Image:           getResultString(m, "image"),
		ImageDigest:     getResultString(m, "image_digest"),
		ConfigDigest:    getResultString(m, "config_digest"),
		TrustLabel:      getResultString(m, "trust_label"),
		TrustLevel:      getResultString(m, "trust_level"),
		Measured:        getResultBool(m, "measured"),
		MeasurementPCR:  int(getResultInt64(m, "measurement_pcr")),
		MeasurementBank: getResultString(m, "measurement_bank"),
		AttestedAt:      getResultString(m, "attested_at"),
	}
	resp.Nonce = decodeResultBase64(m, "nonce")
	resp.Quoted = decodeResultBase64(m, "quoted")
	resp.Signature = decodeResultBase64(m, "signature")
	return resp, nil
}

// joinPEMBundle concatenates a chain of PEM- or DER-encoded certificates into
// a single PEM bundle, which is the shape the MCP tool schema accepts. DER
// entries are PEM-wrapped so the bundle is uniform.
func joinPEMBundle(chain [][]byte) string {
	var b strings.Builder
	for _, blob := range chain {
		if len(blob) == 0 {
			continue
		}
		if block, _ := pem.Decode(blob); block != nil {
			b.Write(blob)
			if !strings.HasSuffix(string(blob), "\n") {
				b.WriteByte('\n')
			}
			continue
		}
		b.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: blob}))
	}
	return b.String()
}

// decodeResultBase64 reads a base64-encoded binary field from a tool result,
// returning nil when the field is absent or malformed. A malformed field is
// not an error here: the authoritative outcome flags are separate scalars, and
// failing the whole call over an unreadable evidence blob would turn a
// successful attestation into a transport error.
func decodeResultBase64(m map[string]interface{}, key string) []byte {
	encoded := getResultString(m, key)
	if encoded == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	return decoded
}
