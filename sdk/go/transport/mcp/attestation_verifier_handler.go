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

// attestation_verifier_handler.go transforms the attestation Verifier's
// requests and tool results for the MCP transport. Certificates and keys
// travel as PEM text, binary evidence as base64 and PCR values as an object
// of index -> base64 digest, matching the quicbaas_attestation_* tool schema.

package mcp

import (
	"encoding/base64"
	"strconv"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// verifierRequestTransforms build the Verifier tools' arguments.
var verifierRequestTransforms = map[string]func(interface{}) (map[string]interface{}, error){
	"PutEndorsement": func(req interface{}) (map[string]interface{}, error) {
		r, ok := req.(*attestationclient.PutEndorsementRequest)
		if !ok || r == nil {
			return nil, invalidVerifierMCP("PutEndorsement")
		}
		args := map[string]interface{}{"name": r.Name, "kind": r.Kind}
		if len(r.CertificatePEM) > 0 {
			args["certificate_pem"] = string(r.CertificatePEM)
		}
		if len(r.PublicKeyPEM) > 0 {
			args["public_key_pem"] = string(r.PublicKeyPEM)
		}
		return args, nil
	},
	"ListEndorsements": func(req interface{}) (map[string]interface{}, error) {
		r, ok := req.(*attestationclient.ListEndorsementsRequest)
		if !ok || r == nil {
			return nil, invalidVerifierMCP("ListEndorsements")
		}
		return map[string]interface{}{"kind": r.Kind}, nil
	},
	"DeleteEndorsement":     nameArguments("DeleteEndorsement"),
	"GetReferenceValues":    nameArguments("GetReferenceValues"),
	"DeleteReferenceValues": nameArguments("DeleteReferenceValues"),
	"ListReferenceValues": func(interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{}, nil
	},
	"PutReferenceValues": func(req interface{}) (map[string]interface{}, error) {
		r, ok := req.(*attestationclient.PutReferenceValuesRequest)
		if !ok || r == nil {
			return nil, invalidVerifierMCP("PutReferenceValues")
		}
		return map[string]interface{}{"name": r.Name, "signed_corim": base64.StdEncoding.EncodeToString(r.SignedCoRIM)}, nil
	},
	"AppraiseQuote": func(req interface{}) (map[string]interface{}, error) {
		r, ok := req.(*attestationclient.AppraiseQuoteRequest)
		if !ok || r == nil {
			return nil, invalidVerifierMCP("AppraiseQuote")
		}
		pcrs := make(map[string]interface{}, len(r.PCRs))
		for idx, digest := range r.PCRs {
			pcrs[strconv.FormatUint(uint64(idx), 10)] = base64.StdEncoding.EncodeToString(digest)
		}
		args := map[string]interface{}{
			"ak_certificate_pem": string(r.AKCertificatePEM),
			"quote":              base64.StdEncoding.EncodeToString(r.Quote),
			"signature":          base64.StdEncoding.EncodeToString(r.Signature),
			"nonce":              base64.StdEncoding.EncodeToString(r.Nonce),
			"pcrs":               pcrs,
			"reference_values":   r.ReferenceValues,
		}
		if len(r.EventLog) > 0 {
			args["event_log"] = base64.StdEncoding.EncodeToString(r.EventLog)
		}
		return args, nil
	},
}

// verifierResponseTransforms decode the Verifier tools' results.
var verifierResponseTransforms = map[string]func(map[string]interface{}) interface{}{
	"PutEndorsement": func(m map[string]interface{}) interface{} { return endorsementFromResult(m) },
	"ListEndorsements": func(m map[string]interface{}) interface{} {
		out := &attestationclient.ListEndorsementsResponse{Endorsements: []*attestationclient.Endorsement{}}
		for _, item := range resultObjects(m, "endorsements") {
			out.Endorsements = append(out.Endorsements, endorsementFromResult(item))
		}
		return out
	},
	"PutReferenceValues": func(m map[string]interface{}) interface{} { return referenceValuesFromResult(m) },
	"GetReferenceValues": func(m map[string]interface{}) interface{} { return referenceValuesFromResult(m) },
	"ListReferenceValues": func(m map[string]interface{}) interface{} {
		out := &attestationclient.ListReferenceValuesResponse{ReferenceValues: []*attestationclient.ReferenceValues{}}
		for _, item := range resultObjects(m, "reference_values") {
			out.ReferenceValues = append(out.ReferenceValues, referenceValuesFromResult(item))
		}
		return out
	},
	"AppraiseQuote": func(m map[string]interface{}) interface{} {
		out := &attestationclient.AttestationResult{
			Verified:             getResultBool(m, "verified"),
			CryptographicFailure: getResultBool(m, "cryptographic_failure"),
			Checks:               []attestationclient.AppraisalCheck{},
			ReferenceValues:      getResultString(m, "reference_values"),
			ManifestID:           getResultString(m, "manifest_id"),
			Signer:               getResultString(m, "signer"),
			BaselineSource:       getResultString(m, "baseline_source"),
			AppraisedAt:          getResultString(m, "appraised_at"),
		}
		for _, check := range resultObjects(m, "checks") {
			out.Checks = append(out.Checks, attestationclient.AppraisalCheck{
				Name:    getResultString(check, "name"),
				Outcome: getResultString(check, "outcome"),
				Reason:  getResultString(check, "reason"),
			})
		}
		return out
	},
}

func invalidVerifierMCP(method string) error {
	return errors.NewInvalidInput("mcp.attestation."+method, "invalid request type")
}

// nameArguments builds the arguments of a verb that takes only a name.
func nameArguments(method string) func(interface{}) (map[string]interface{}, error) {
	return func(req interface{}) (map[string]interface{}, error) {
		r, ok := req.(*attestationclient.NameRequest)
		if !ok || r == nil {
			return nil, invalidVerifierMCP(method)
		}
		return map[string]interface{}{"name": r.Name}, nil
	}
}

// resultObjects reads an array of objects from a tool result.
func resultObjects(m map[string]interface{}, key string) []map[string]interface{} {
	items, _ := m[key].([]interface{})
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if obj, ok := item.(map[string]interface{}); ok {
			out = append(out, obj)
		}
	}
	return out
}

func endorsementFromResult(m map[string]interface{}) *attestationclient.Endorsement {
	e := &attestationclient.Endorsement{
		Name:      getResultString(m, "name"),
		Kind:      getResultString(m, "kind"),
		CreatedAt: getResultString(m, "created_at"),
	}
	if pemText := getResultString(m, "certificate_pem"); pemText != "" {
		e.CertificatePEM = []byte(pemText)
	}
	if pemText := getResultString(m, "public_key_pem"); pemText != "" {
		e.PublicKeyPEM = []byte(pemText)
	}
	return e
}

func referenceValuesFromResult(m map[string]interface{}) *attestationclient.ReferenceValues {
	rv := &attestationclient.ReferenceValues{
		Name:        getResultString(m, "name"),
		ManifestID:  getResultString(m, "manifest_id"),
		Profile:     getResultString(m, "profile"),
		Signer:      getResultString(m, "signer"),
		NotBefore:   getResultString(m, "not_before"),
		NotAfter:    getResultString(m, "not_after"),
		PCRIndices:  []uint32{},
		StoredAt:    getResultString(m, "stored_at"),
		SignedCoRIM: decodeResultBase64(m, "signed_corim"),
	}
	indices, _ := m["pcr_indices"].([]interface{})
	for _, idx := range indices {
		if f, ok := idx.(float64); ok {
			rv.PCRIndices = append(rv.PCRIndices, uint32(f))
		}
	}
	return rv
}
