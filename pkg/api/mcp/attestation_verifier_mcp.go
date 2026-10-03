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
	"encoding/base64"
	"strconv"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The attestation Verifier over MCP. Certificates and public keys travel as
// PEM text; the signed CoRIM, quote, signature, nonce and event log as
// base64; PCR values as an object of index -> base64 digest.

var (
	// AttestationPutEndorsementTool stores a Verifier trust anchor.
	AttestationPutEndorsementTool = Tool{
		Name:        "quicbaas_attestation_put_endorsement",
		Description: "Store (or replace) an attestation Verifier trust anchor: a reference-signer (CA certificate or pinned public key that signed CoRIM reference values must verify under) or an ak-root (CA certificate attestation-key certificates must chain to)",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name":            {Type: "string", Description: "Endorsement name (no '/')"},
				"kind":            {Type: "string", Description: "reference-signer or ak-root"},
				"certificate_pem": {Type: "string", Description: "PEM CA certificate (exactly one of certificate_pem and public_key_pem)"},
				"public_key_pem":  {Type: "string", Description: "PEM PKIX public key, reference-signer only"},
			},
			Required: []string{"name", "kind"},
		},
	}

	// AttestationListEndorsementsTool lists endorsements.
	AttestationListEndorsementsTool = Tool{
		Name:        "quicbaas_attestation_list_endorsements",
		Description: "List the attestation Verifier's endorsements, optionally of one kind",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"kind": {Type: "string", Description: "reference-signer or ak-root; empty lists all"},
			},
		},
	}

	// AttestationDeleteEndorsementTool removes an endorsement.
	AttestationDeleteEndorsementTool = Tool{
		Name:        "quicbaas_attestation_delete_endorsement",
		Description: "Delete an attestation Verifier endorsement; reference values it signed stop appraising",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{"name": {Type: "string", Description: "Endorsement name"}},
			Required:   []string{"name"},
		},
	}

	// AttestationPutReferenceValuesTool stores signed reference values.
	AttestationPutReferenceValuesTool = Tool{
		Name:        "quicbaas_attestation_put_reference_values",
		Description: "Store signed reference values (a COSE_Sign1 signed CoRIM). They must verify against a stored reference-signer endorsement; unsigned or unendorsed values are refused",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name":         {Type: "string", Description: "Reference-values name (no '/')"},
				"signed_corim": {Type: "string", Description: "Base64-encoded COSE_Sign1 signed CoRIM"},
			},
			Required: []string{"name", "signed_corim"},
		},
	}

	// AttestationGetReferenceValuesTool returns stored reference values.
	AttestationGetReferenceValuesTool = Tool{
		Name:        "quicbaas_attestation_get_reference_values",
		Description: "Get stored reference values, including the signed CoRIM",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{"name": {Type: "string", Description: "Reference-values name"}},
			Required:   []string{"name"},
		},
	}

	// AttestationListReferenceValuesTool lists stored reference values.
	AttestationListReferenceValuesTool = Tool{
		Name:        "quicbaas_attestation_list_reference_values",
		Description: "List stored reference values (without the signed CoRIM bodies)",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
	}

	// AttestationDeleteReferenceValuesTool removes stored reference values.
	AttestationDeleteReferenceValuesTool = Tool{
		Name:        "quicbaas_attestation_delete_reference_values",
		Description: "Delete stored reference values",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{"name": {Type: "string", Description: "Reference-values name"}},
			Required:   []string{"name"},
		},
	}

	// AttestationAppraiseQuoteTool appraises TPM quote evidence.
	AttestationAppraiseQuoteTool = Tool{
		Name:        "quicbaas_attestation_appraise_quote",
		Description: "Appraise TPM quote evidence (attestation-key certificate, quote, signature, nonce, PCR values, optional event log) against stored, signed reference values and return the attestation result",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"ak_certificate_pem": {Type: "string", Description: "PEM attestation-key certificate, leaf first, then intermediates"},
				"quote":              {Type: "string", Description: "Base64-encoded TPMS_ATTEST"},
				"signature":          {Type: "string", Description: "Base64-encoded signature over the quote"},
				"nonce":              {Type: "string", Description: "Base64-encoded qualifying data the quote must carry"},
				"pcrs":               {Type: "object", Description: "Quoted PCR values: PCR index -> base64 digest"},
				"event_log":          {Type: "string", Description: "Base64-encoded TCG event log (optional)"},
				"reference_values":   {Type: "string", Description: "Name of the stored reference values"},
			},
			Required: []string{"ak_certificate_pem", "quote", "signature", "nonce", "pcrs", "reference_values"},
		},
	}
)

// getAllAttestationVerifierTools returns the Verifier tools (8 tools).
func getAllAttestationVerifierTools() []Tool {
	return []Tool{
		AttestationPutEndorsementTool,
		AttestationListEndorsementsTool,
		AttestationDeleteEndorsementTool,
		AttestationPutReferenceValuesTool,
		AttestationGetReferenceValuesTool,
		AttestationListReferenceValuesTool,
		AttestationDeleteReferenceValuesTool,
		AttestationAppraiseQuoteTool,
	}
}

// attestationVerifierExecutors dispatches the Verifier tools.
var attestationVerifierExecutors = map[string]attestationToolExecutor{
	"quicbaas_attestation_put_endorsement":         (*ToolRegistry).handleAttestationPutEndorsement,
	"quicbaas_attestation_list_endorsements":       (*ToolRegistry).handleAttestationListEndorsements,
	"quicbaas_attestation_delete_endorsement":      (*ToolRegistry).handleAttestationDeleteEndorsement,
	"quicbaas_attestation_put_reference_values":    (*ToolRegistry).handleAttestationPutReferenceValues,
	"quicbaas_attestation_get_reference_values":    (*ToolRegistry).handleAttestationGetReferenceValues,
	"quicbaas_attestation_list_reference_values":   (*ToolRegistry).handleAttestationListReferenceValues,
	"quicbaas_attestation_delete_reference_values": (*ToolRegistry).handleAttestationDeleteReferenceValues,
	"quicbaas_attestation_appraise_quote":          (*ToolRegistry).handleAttestationAppraiseQuote,
}

func (r *ToolRegistry) handleAttestationPutEndorsement(ctx context.Context, svc service.AttestationService, args map[string]interface{}) (interface{}, error) {
	e, err := svc.PutEndorsement(ctx, &service.PutEndorsementRequest{
		Name:           getString(args, "name"),
		Kind:           service.EndorsementKind(getString(args, "kind")),
		CertificatePEM: optionalBytes(getString(args, "certificate_pem")),
		PublicKeyPEM:   optionalBytes(getString(args, "public_key_pem")),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("attestation put endorsement", err)
	}
	return endorsementResult(e), nil
}

func (r *ToolRegistry) handleAttestationListEndorsements(ctx context.Context, svc service.AttestationService, args map[string]interface{}) (interface{}, error) {
	resp, err := svc.ListEndorsements(ctx, &service.ListEndorsementsRequest{Kind: service.EndorsementKind(getString(args, "kind"))})
	if err != nil {
		return nil, api.NewOperationFailedError("attestation list endorsements", err)
	}
	out := make([]map[string]interface{}, 0, len(resp.Endorsements))
	for _, e := range resp.Endorsements {
		out = append(out, endorsementResult(e))
	}
	return map[string]interface{}{"endorsements": out}, nil
}

func (r *ToolRegistry) handleAttestationDeleteEndorsement(ctx context.Context, svc service.AttestationService, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if err := svc.DeleteEndorsement(ctx, &service.DeleteEndorsementRequest{Name: name}); err != nil {
		return nil, api.NewOperationFailedError("attestation delete endorsement", err)
	}
	return map[string]interface{}{"name": name, "deleted": true}, nil
}

func (r *ToolRegistry) handleAttestationPutReferenceValues(ctx context.Context, svc service.AttestationService, args map[string]interface{}) (interface{}, error) {
	signed, err := decodeBase64Arg(args, "signed_corim")
	if err != nil {
		return nil, err
	}
	if len(signed) == 0 {
		return nil, api.NewRequiredFieldError("signed_corim")
	}
	rv, err := svc.PutReferenceValues(ctx, &service.PutReferenceValuesRequest{Name: getString(args, "name"), SignedCoRIM: signed})
	if err != nil {
		return nil, api.NewOperationFailedError("attestation put reference values", err)
	}
	return referenceValuesResult(rv), nil
}

func (r *ToolRegistry) handleAttestationGetReferenceValues(ctx context.Context, svc service.AttestationService, args map[string]interface{}) (interface{}, error) {
	rv, err := svc.GetReferenceValues(ctx, &service.GetReferenceValuesRequest{Name: getString(args, "name")})
	if err != nil {
		return nil, api.NewOperationFailedError("attestation get reference values", err)
	}
	return referenceValuesResult(rv), nil
}

func (r *ToolRegistry) handleAttestationListReferenceValues(ctx context.Context, svc service.AttestationService, _ map[string]interface{}) (interface{}, error) {
	resp, err := svc.ListReferenceValues(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("attestation list reference values", err)
	}
	out := make([]map[string]interface{}, 0, len(resp.ReferenceValues))
	for _, rv := range resp.ReferenceValues {
		out = append(out, referenceValuesResult(rv))
	}
	return map[string]interface{}{"reference_values": out}, nil
}

func (r *ToolRegistry) handleAttestationDeleteReferenceValues(ctx context.Context, svc service.AttestationService, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if err := svc.DeleteReferenceValues(ctx, &service.DeleteReferenceValuesRequest{Name: name}); err != nil {
		return nil, api.NewOperationFailedError("attestation delete reference values", err)
	}
	return map[string]interface{}{"name": name, "deleted": true}, nil
}

func (r *ToolRegistry) handleAttestationAppraiseQuote(ctx context.Context, svc service.AttestationService, args map[string]interface{}) (interface{}, error) {
	req := &service.AppraiseQuoteRequest{
		AKCertificatePEM: optionalBytes(getString(args, "ak_certificate_pem")),
		ReferenceValues:  getString(args, "reference_values"),
	}
	for field, dst := range map[string]*[]byte{"quote": &req.Quote, "signature": &req.Signature, "nonce": &req.Nonce, "event_log": &req.EventLog} {
		decoded, err := decodeBase64Arg(args, field)
		if err != nil {
			return nil, err
		}
		*dst = decoded
	}
	pcrs, err := decodePCRArg(args, "pcrs")
	if err != nil {
		return nil, err
	}
	req.PCRs = pcrs
	result, err := svc.AppraiseQuote(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("attestation appraise quote", err)
	}
	checks := make([]map[string]interface{}, 0, len(result.Checks))
	for _, check := range result.Checks {
		checks = append(checks, map[string]interface{}{"name": check.Name, "outcome": string(check.Outcome), "reason": check.Reason})
	}
	return map[string]interface{}{
		"verified":              result.Verified,
		"cryptographic_failure": result.CryptographicFailure,
		"checks":                checks,
		"reference_values":      result.ReferenceValues,
		"manifest_id":           result.ManifestID,
		"signer":                result.Signer,
		"baseline_source":       result.BaselineSource,
		"appraised_at":          result.AppraisedAt.Format(time.RFC3339Nano),
	}, nil
}

// decodePCRArg decodes an object of PCR index -> base64 digest.
func decodePCRArg(args map[string]interface{}, key string) (map[uint32][]byte, error) {
	raw := getStringMap(args, key)
	out := make(map[uint32][]byte, len(raw))
	for index, encoded := range raw {
		idx, err := strconv.ParseUint(index, 10, 32)
		if err != nil {
			return nil, api.NewInvalidFormatError(key, err)
		}
		digest, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, api.NewInvalidBase64Error(key, err)
		}
		out[uint32(idx)] = digest
	}
	return out, nil
}

// optionalBytes keeps an absent string argument absent rather than empty.
func optionalBytes(s string) []byte {
	if s == "" {
		return nil
	}
	return []byte(s)
}

func endorsementResult(e *service.Endorsement) map[string]interface{} {
	out := map[string]interface{}{
		"name":       e.Name,
		"kind":       string(e.Kind),
		"created_at": e.CreatedAt.Format(time.RFC3339Nano),
	}
	if len(e.CertificatePEM) > 0 {
		out["certificate_pem"] = string(e.CertificatePEM)
	}
	if len(e.PublicKeyPEM) > 0 {
		out["public_key_pem"] = string(e.PublicKeyPEM)
	}
	return out
}

func referenceValuesResult(rv *service.ReferenceValues) map[string]interface{} {
	out := map[string]interface{}{
		"name":        rv.Name,
		"manifest_id": rv.ManifestID,
		"profile":     rv.Profile,
		"signer":      rv.Signer,
		"pcr_indices": rv.PCRIndices,
		"stored_at":   rv.StoredAt.Format(time.RFC3339Nano),
	}
	if !rv.NotBefore.IsZero() {
		out["not_before"] = rv.NotBefore.Format(time.RFC3339Nano)
	}
	if !rv.NotAfter.IsZero() {
		out["not_after"] = rv.NotAfter.Format(time.RFC3339Nano)
	}
	if len(rv.SignedCoRIM) > 0 {
		out["signed_corim"] = base64.StdEncoding.EncodeToString(rv.SignedCoRIM)
	}
	return out
}
