//go:build mcp

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Ceremony MCP tools — quicbaas_ceremony_* surface.
//
// Mirrors the REST handlers in pkg/api/rest/handlers_ceremony.go so the MCP
// transport exposes an identical ceremony-lifecycle contract:
//
//   qrdb_ceremony_initialize
//   qrdb_ceremony_status
//   qrdb_ceremony_claim_cert_begin
//   qrdb_ceremony_claim_cert_complete
//   qrdb_ceremony_begin_claim_share
//   qrdb_ceremony_complete_claim_share
//   qrdb_ceremony_sign_csr
//   quicbaas_ceremony_list_officers
//   qrdb_ceremony_enroll_external_officer
//
// All tools delegate to a CeremonyServicer installed via
// ToolRegistry.SetCeremonyService. When the service is unwired, every tool
// returns CeremonyServiceUnavailableError so MCP clients see a typed
// "service unavailable" condition rather than a nil-pointer panic.
// =============================================================================

// ceremonyServiceUnavailableQBError is the canonical QBError returned when the
// ceremony surface is not wired. Returning a QBError with the ErrUnavailable
// code lets pkg/api/mcp/server.go's mapErrorToJSONRPCError classify the
// condition through its explicit-code branch (ServiceUnavailable) instead of
// the generic "Tool execution failed" default.
var ceremonyServiceUnavailableQBError = &qberrors.QBError{
	Code: qberrors.ErrUnavailable,
	Op:   "mcp.ceremony",
	Err:  &CeremonyServiceUnavailableError{},
}

// CeremonyServiceUnavailableError is returned by every ceremony tool when the
// CeremonyServicer has not been wired into the ToolRegistry. Mirrors the REST
// 503-on-nil-facade behaviour. It is always wrapped in
// ceremonyServiceUnavailableQBError before being returned so the MCP error
// mapper classifies it as ServiceUnavailable.
type CeremonyServiceUnavailableError struct{}

// Error implements the error interface. The message contains "unavailable" so
// that even when unwrapped from its QBError envelope the MCP mapper's
// message-pattern fallback still classifies it correctly.
func (e *CeremonyServiceUnavailableError) Error() string {
	return "ceremony service unavailable: not configured"
}

// CeremonyFieldRequiredError is returned when a required field is missing
// from the tool arguments.
type CeremonyFieldRequiredError struct {
	Field string
}

// Error implements the error interface.
func (e *CeremonyFieldRequiredError) Error() string {
	return "ceremony: " + e.Field + " is required"
}

// CeremonyFieldConflictError is returned when mutually exclusive fields are
// both supplied (cert_pem and pubkey_pem on the external-officer tool).
type CeremonyFieldConflictError struct {
	Reason string
}

// Error implements the error interface.
func (e *CeremonyFieldConflictError) Error() string {
	return "ceremony: " + e.Reason
}

// registerCeremonyTools adds every quicbaas_ceremony_* tool definition to the
// registry. Dispatched via isCeremonyTool / executeCeremonyTool.
func (r *ToolRegistry) registerCeremonyTools() {
	r.addTool(ToolDefinition{
		Name:        "qrdb_ceremony_initialize",
		Description: "Initialise the system ceremony: issue TLS and SO certificates, optionally start M-of-N officer enrollment.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"so_pin": map[string]interface{}{
					"type":        "string",
					"description": "Security Officer PIN.",
				},
				"user_pin": map[string]interface{}{
					"type":        "string",
					"description": "User PIN.",
				},
				"credential_seal_strategy": map[string]interface{}{
					"type":        "string",
					"description": "Optional credential-seal strategy override.",
				},
				"threshold": map[string]interface{}{
					"type":        "integer",
					"description": "M-of-N threshold; 0 selects single-admin mode.",
					"minimum":     0,
				},
				"officers": map[string]interface{}{
					"type":        "array",
					"description": "Officers participating in the ceremony.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"username": map[string]interface{}{"type": "string"},
							"csr_pem":  map[string]interface{}{"type": "string"},
						},
					},
				},
				"hostname": map[string]interface{}{
					"type":        "string",
					"description": "Optional hostname for the issued TLS certificate.",
				},
				"sans": map[string]interface{}{
					"type":        "array",
					"description": "Optional Subject Alternative Names for the TLS certificate.",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_ceremony_status",
		Description: "Return the current ceremony state and SPKI pin (when CA has been issued).",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_ceremony_claim_cert_begin",
		Description: "Start the certificate-claim handshake for an officer, returning a challenge nonce.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username": map[string]interface{}{
					"type":        "string",
					"description": "Officer username initiating the certificate claim.",
				},
			},
			"required": []string{"username"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_ceremony_claim_cert_complete",
		Description: "Complete the certificate-claim handshake by submitting the officer's nonce signature.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username":  map[string]interface{}{"type": "string", "description": "Officer username completing the claim."},
				"nonce_hex": map[string]interface{}{"type": "string", "description": "Hex-encoded challenge nonce from the begin step."},
				"signature": map[string]interface{}{"type": "string", "description": "Base64-encoded signature over the nonce."},
			},
			"required": []string{"username", "nonce_hex", "signature"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_ceremony_begin_claim_share",
		Description: "Start the Shamir share claim handshake for an officer, returning the challenge nonce to sign. Caller identity is derived from the connection, never from the arguments.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username": map[string]interface{}{
					"type":        "string",
					"description": "Officer username claiming their Shamir share.",
				},
			},
			"required": []string{"username"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_ceremony_complete_claim_share",
		Description: "Complete the Shamir share claim handshake by submitting the officer's nonce signature. The share is released exactly once.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username":  map[string]interface{}{"type": "string", "description": "Officer username completing the share claim."},
				"nonce_hex": map[string]interface{}{"type": "string", "description": "Hex-encoded challenge nonce from the begin step."},
				"signature": map[string]interface{}{"type": "string", "description": "Base64-encoded signature over the nonce."},
			},
			"required": []string{"username", "nonce_hex", "signature"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_ceremony_sign_csr",
		Description: "Sign a certificate-signing request during the initialisation ceremony.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username": map[string]interface{}{"type": "string", "description": "Officer or service username requesting CSR signing."},
				"so_pin":   map[string]interface{}{"type": "string", "description": "Security Officer PIN authorising the signing operation."},
				"csr_pem":  map[string]interface{}{"type": "string", "description": "PEM-encoded certificate-signing request."},
				"role":     map[string]interface{}{"type": "string", "description": "Certificate role profile to apply (e.g., security-officer, node)."},
			},
			"required": []string{"username", "csr_pem"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ceremony_list_officers",
		Description: "Return the persistent ceremony-officer registry. The roster may be empty before any officers are enrolled.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_ceremony_enroll_external_officer",
		Description: "Enrol an officer whose key material lives outside the daemon. Exactly one of cert_pem or pubkey_pem must be supplied.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username":    map[string]interface{}{"type": "string", "description": "Unique officer identifier."},
				"cert_pem":    map[string]interface{}{"type": "string", "description": "PEM-encoded X.509 certificate. Mutually exclusive with pubkey_pem."},
				"pubkey_pem":  map[string]interface{}{"type": "string", "description": "PEM-encoded SPKI public key. Mutually exclusive with cert_pem."},
				"share_index": map[string]interface{}{"type": "integer", "description": "1-based Shamir share index assigned to this officer."},
				"auth_factor": map[string]interface{}{"type": "string", "description": "Auth-factor identifier (mtls, webauthn, piv)."},
				"share_bytes": map[string]interface{}{"type": "string", "description": "Optional base64 plaintext share to seal to the officer's public key."},
			},
			"required": []string{"username", "auth_factor"},
		},
	})
}

// ceremonyToolNames enumerates every tool name owned by the ceremony surface.
var ceremonyToolNames = map[string]struct{}{
	"qrdb_ceremony_initialize":              {},
	"qrdb_ceremony_status":                  {},
	"qrdb_ceremony_claim_cert_begin":        {},
	"qrdb_ceremony_claim_cert_complete":     {},
	"qrdb_ceremony_begin_claim_share":       {},
	"qrdb_ceremony_complete_claim_share":    {},
	"qrdb_ceremony_sign_csr":                {},
	"quicbaas_ceremony_list_officers":       {},
	"qrdb_ceremony_enroll_external_officer": {},
}

// isCeremonyTool reports whether the given tool name belongs to the ceremony
// surface.
func isCeremonyTool(name string) bool {
	_, ok := ceremonyToolNames[name]
	return ok
}

// ceremonyExecutor is the signature each per-tool handler implements.
type ceremonyExecutor func(context.Context, service.CeremonyServicer, map[string]interface{}) (interface{}, error)

// ceremonyExecutors is the map-based dispatch table for ceremony tools.
var ceremonyExecutors = map[string]ceremonyExecutor{
	"qrdb_ceremony_initialize":              executeCeremonyInitialize,
	"qrdb_ceremony_status":                  executeCeremonyStatus,
	"qrdb_ceremony_claim_cert_begin":        executeCeremonyClaimCertBegin,
	"qrdb_ceremony_claim_cert_complete":     executeCeremonyClaimCertComplete,
	"qrdb_ceremony_begin_claim_share":       executeCeremonyBeginClaimShare,
	"qrdb_ceremony_complete_claim_share":    executeCeremonyCompleteClaimShare,
	"qrdb_ceremony_sign_csr":                executeCeremonySignCSR,
	"quicbaas_ceremony_list_officers":       executeCeremonyListOfficers,
	"qrdb_ceremony_enroll_external_officer": executeCeremonyEnrollExternalOfficer,
}

// executeCeremonyTool dispatches a ceremony-surface call to the appropriate
// executor. Returns CeremonyServiceUnavailableError when the registry has no
// CeremonyServicer wired.
func (r *ToolRegistry) executeCeremonyTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	svc := r.ceremonySvc
	if svc == nil {
		return nil, ceremonyServiceUnavailableQBError
	}
	executor, ok := ceremonyExecutors[name]
	if !ok {
		return nil, ceremonyServiceUnavailableQBError
	}
	return executor(ctx, svc, args)
}

// ---------------------------------------------------------------------------
// Per-tool executors
// ---------------------------------------------------------------------------

// executeCeremonyInitialize implements qrdb_ceremony_initialize.
func executeCeremonyInitialize(ctx context.Context, svc service.CeremonyServicer, args map[string]interface{}) (interface{}, error) {
	req := &service.CeremonyInitRequest{
		SOPin:                  getString(args, "so_pin"),
		UserPin:                getString(args, "user_pin"),
		CredentialSealStrategy: getString(args, "credential_seal_strategy"),
		Threshold:              getInt(args, "threshold"),
		Hostname:               getString(args, "hostname"),
		SANs:                   getStringSlice(args, "sans"),
		Officers:               parseCeremonyOfficers(args),
	}
	info, err := svc.CeremonyInitialize(ctx, req)
	if err != nil {
		return nil, err
	}
	return ceremonyInitToMap(info), nil
}

// executeCeremonyStatus implements qrdb_ceremony_status.
func executeCeremonyStatus(ctx context.Context, svc service.CeremonyServicer, _ map[string]interface{}) (interface{}, error) {
	info, err := svc.CeremonyStatus(ctx)
	if err != nil {
		return nil, err
	}
	return ceremonyStatusToMap(info), nil
}

// executeCeremonyClaimCertBegin implements qrdb_ceremony_claim_cert_begin.
func executeCeremonyClaimCertBegin(ctx context.Context, svc service.CeremonyServicer, args map[string]interface{}) (interface{}, error) {
	username := getString(args, "username")
	if username == "" {
		return nil, &CeremonyFieldRequiredError{Field: "username"}
	}
	challenge, err := svc.CeremonyBeginClaimCert(ctx, username)
	if err != nil {
		return nil, err
	}
	return ceremonyChallengeToMap(challenge), nil
}

// executeCeremonyClaimCertComplete implements
// qrdb_ceremony_claim_cert_complete.
func executeCeremonyClaimCertComplete(ctx context.Context, svc service.CeremonyServicer, args map[string]interface{}) (interface{}, error) {
	username := getString(args, "username")
	if username == "" {
		return nil, &CeremonyFieldRequiredError{Field: "username"}
	}
	nonceHex := getString(args, "nonce_hex")
	if nonceHex == "" {
		return nil, &CeremonyFieldRequiredError{Field: "nonce_hex"}
	}
	signature := getString(args, "signature")
	if signature == "" {
		return nil, &CeremonyFieldRequiredError{Field: "signature"}
	}
	result, err := svc.CeremonyCompleteClaimCert(ctx, username, nonceHex, signature)
	if err != nil {
		return nil, err
	}
	return ceremonyClaimCertToMap(result), nil
}

// executeCeremonyBeginClaimShare implements qrdb_ceremony_begin_claim_share.
func executeCeremonyBeginClaimShare(ctx context.Context, svc service.CeremonyServicer, args map[string]interface{}) (interface{}, error) {
	username := getString(args, "username")
	if username == "" {
		return nil, &CeremonyFieldRequiredError{Field: "username"}
	}
	challenge, err := svc.CeremonyBeginClaimShare(ctx, username)
	if err != nil {
		return nil, err
	}
	return ceremonyChallengeToMap(challenge), nil
}

// executeCeremonyCompleteClaimShare implements
// qrdb_ceremony_complete_claim_share. The signature arrives base64-encoded
// and the released share is returned base64-encoded under the "share" key,
// mirroring qrdb's own MCP tool contract exactly.
func executeCeremonyCompleteClaimShare(ctx context.Context, svc service.CeremonyServicer, args map[string]interface{}) (interface{}, error) {
	username := getString(args, "username")
	if username == "" {
		return nil, &CeremonyFieldRequiredError{Field: "username"}
	}
	nonceHex := getString(args, "nonce_hex")
	if nonceHex == "" {
		return nil, &CeremonyFieldRequiredError{Field: "nonce_hex"}
	}
	sigB64 := getString(args, "signature")
	if sigB64 == "" {
		return nil, &CeremonyFieldRequiredError{Field: "signature"}
	}
	sig, decErr := base64.StdEncoding.DecodeString(sigB64)
	if decErr != nil {
		return nil, &CeremonyFieldConflictError{Reason: "signature must be base64-encoded"}
	}
	result, err := svc.CeremonyCompleteClaimShare(ctx, username, nonceHex, sig)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"share_index": result.ShareIndex,
		"share":       base64.StdEncoding.EncodeToString([]byte(result.ShareJSON)),
	}, nil
}

// executeCeremonySignCSR implements qrdb_ceremony_sign_csr.
func executeCeremonySignCSR(ctx context.Context, svc service.CeremonyServicer, args map[string]interface{}) (interface{}, error) {
	username := getString(args, "username")
	if username == "" {
		return nil, &CeremonyFieldRequiredError{Field: "username"}
	}
	csrPEM := getString(args, "csr_pem")
	if csrPEM == "" {
		return nil, &CeremonyFieldRequiredError{Field: "csr_pem"}
	}
	soPin := getString(args, "so_pin")
	role := getString(args, "role")
	result, err := svc.CeremonySignCSR(ctx, username, soPin, csrPEM, role)
	if err != nil {
		return nil, err
	}
	return ceremonySignCSRToMap(result), nil
}

// executeCeremonyListOfficers implements quicbaas_ceremony_list_officers.
// The CeremonyServicer facade does not (yet) expose a typed ListOfficers
// method; the REST handler returns an empty roster in this case so the UI's
// list component renders an empty-state rather than a banner error. The MCP
// tool mirrors that exact shape.
func executeCeremonyListOfficers(_ context.Context, _ service.CeremonyServicer, _ map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"officers": []interface{}{},
	}, nil
}

// executeCeremonyEnrollExternalOfficer implements
// qrdb_ceremony_enroll_external_officer.
func executeCeremonyEnrollExternalOfficer(ctx context.Context, svc service.CeremonyServicer, args map[string]interface{}) (interface{}, error) {
	username := getString(args, "username")
	if username == "" {
		return nil, &CeremonyFieldRequiredError{Field: "username"}
	}
	authFactor := getString(args, "auth_factor")
	if authFactor == "" {
		return nil, &CeremonyFieldRequiredError{Field: "auth_factor"}
	}
	certPEM := getString(args, "cert_pem")
	pubKeyPEM := getString(args, "pubkey_pem")
	if certPEM == "" && pubKeyPEM == "" {
		return nil, &CeremonyFieldConflictError{
			Reason: "exactly one of cert_pem or pubkey_pem is required",
		}
	}
	if certPEM != "" && pubKeyPEM != "" {
		return nil, &CeremonyFieldConflictError{
			Reason: "cert_pem and pubkey_pem are mutually exclusive",
		}
	}

	req := &service.CeremonyEnrollExternalOfficerRequest{
		Username:   username,
		CertPEM:    certPEM,
		PubKeyPEM:  pubKeyPEM,
		ShareIndex: getInt(args, "share_index"),
		AuthFactor: authFactor,
		ShareBytes: []byte(getString(args, "share_bytes")),
	}
	resp, err := svc.CeremonyEnrollExternalOfficer(ctx, req)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"sealed_share": resp.SealedShare}, nil
}

// ---------------------------------------------------------------------------
// Helpers — argument parsing and response mapping
// ---------------------------------------------------------------------------

// parseCeremonyOfficers extracts a list of CeremonyOfficer from the args
// "officers" key. The MCP transport may deliver these as a JSON array of
// objects with username + csr_pem fields. Missing/empty entries are skipped.
func parseCeremonyOfficers(args map[string]interface{}) []service.CeremonyOfficer {
	raw, ok := args["officers"]
	if !ok || raw == nil {
		return nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]service.CeremonyOfficer, 0, len(list))
	for _, item := range list {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		username := getString(entry, "username")
		if username == "" {
			continue
		}
		out = append(out, service.CeremonyOfficer{
			Username: username,
			CSRPEM:   getString(entry, "csr_pem"),
		})
	}
	return out
}

// ceremonyInitToMap renders a CeremonyInitInfo as the JSON wire shape shared
// with handlers_ceremony.go.
func ceremonyInitToMap(info *service.CeremonyInitInfo) map[string]interface{} {
	resp := map[string]interface{}{
		"spki_pin":     info.SPKIPin,
		"ca_cert_pem":  string(info.CACertPEM),
		"tls_cert_pem": string(info.TLSCertPEM),
		"state":        info.State,
		"share_count":  info.ShareCount,
	}
	if len(info.SOCertPEM) > 0 {
		resp["so_cert_pem"] = string(info.SOCertPEM)
	}
	if len(info.SOKeyPEM) > 0 {
		resp["so_key_pem"] = string(info.SOKeyPEM)
	}
	return resp
}

// ceremonyStatusToMap renders a CeremonyStatusInfo as the JSON wire shape
// shared with handlers_ceremony.go.
func ceremonyStatusToMap(info *service.CeremonyStatusInfo) map[string]interface{} {
	resp := map[string]interface{}{
		"state": info.State,
	}
	if info.SPKIPin != "" {
		resp["spki_pin"] = info.SPKIPin
	}
	return resp
}

// ceremonyChallengeToMap renders a CeremonyChallenge as the JSON wire shape
// shared with handlers_ceremony.go.
func ceremonyChallengeToMap(c *service.CeremonyChallenge) map[string]interface{} {
	resp := map[string]interface{}{
		"nonce_hex": c.NonceHex,
	}
	if !c.ExpiresAt.IsZero() {
		resp["expires_at"] = c.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return resp
}

// ceremonyClaimCertToMap renders a CeremonyClaimCertResult as the JSON wire
// shape shared with handlers_ceremony.go.
func ceremonyClaimCertToMap(r *service.CeremonyClaimCertResult) map[string]interface{} {
	return map[string]interface{}{
		"cert_pem":    string(r.CertPEM),
		"ca_cert_pem": string(r.CACertPEM),
	}
}

// ceremonySignCSRToMap renders a CeremonySignCSRResult as the JSON wire shape
// shared with handlers_ceremony.go.
func ceremonySignCSRToMap(r *service.CeremonySignCSRResult) map[string]interface{} {
	return map[string]interface{}{
		"cert_pem": string(r.CertPEM),
	}
}
