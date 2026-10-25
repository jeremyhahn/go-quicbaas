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
	"encoding/json"

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
//   qrdb_ceremony_sign_csr
//   quicbaas_ceremony_list_officers
//
// Share-holder claims moved to the barrier holder tools
// (tools_barrier_holders.go) with go-qrdb v0.47.
//
// All tools delegate to a CeremonyServicer installed via
// ToolRegistry.SetCeremonyService. When the service is unwired, every tool
// returns CeremonyServiceUnavailableError so MCP clients see a typed
// "service unavailable" condition rather than a nil-pointer panic.
// =============================================================================

// errCeremonyServiceUnavailableQB is the canonical QBError returned when the
// ceremony surface is not wired. Returning a QBError with the ErrUnavailable
// code lets pkg/api/mcp/server.go's mapErrorToJSONRPCError classify the
// condition through its explicit-code branch (ServiceUnavailable) instead of
// the generic "Tool execution failed" default.
var errCeremonyServiceUnavailableQB = &qberrors.QBError{
	Code: qberrors.ErrUnavailable,
	Op:   "mcp.ceremony",
	Err:  &CeremonyServiceUnavailableError{},
}

// CeremonyServiceUnavailableError is returned by every ceremony tool when the
// CeremonyServicer has not been wired into the ToolRegistry. Mirrors the REST
// 503-on-nil-facade behaviour. It is always wrapped in
// errCeremonyServiceUnavailableQB before being returned so the MCP error
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

// CeremonyOfficersArgError reports a qrdb_ceremony_initialize officers
// argument that does not decode into go-qrdb's officer request type.
type CeremonyOfficersArgError struct {
	Cause error
}

// Error implements the error interface.
func (e *CeremonyOfficersArgError) Error() string {
	return "ceremony.initialize: invalid officers argument: " + e.Cause.Error()
}

// Unwrap returns the decoding error.
func (e *CeremonyOfficersArgError) Unwrap() error {
	return e.Cause
}

// ErrorCode classifies the error as invalid input.
func (e *CeremonyOfficersArgError) ErrorCode() qberrors.ErrorCode {
	return qberrors.ErrInvalidInput
}

// registerCeremonyTools adds every quicbaas_ceremony_* tool definition to the
// registry. Dispatched via isCeremonyTool / executeCeremonyTool.
func (r *ToolRegistry) registerCeremonyTools() {
	r.addTool(ToolDefinition{
		Name:        "qrdb_ceremony_initialize",
		Description: "Initialise the system ceremony: issue the TLS certificate, optionally start M-of-N officer enrollment.",
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
					"description": "Officers participating in the ceremony (go-qrdb CeremonyOfficerRequest objects: username, role, csr_pem, encryption_kem, encryption_key, factors, designated).",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"username":       map[string]interface{}{"type": "string"},
							"role":           map[string]interface{}{"type": "string", "description": "Holder role: so (default) or custodian."},
							"csr_pem":        map[string]interface{}{"type": "string"},
							"encryption_kem": map[string]interface{}{"type": "string", "description": "HPKE suite of a separate share-envelope key."},
							"encryption_key": map[string]interface{}{"type": "string", "description": "Base64 of the separate share-envelope public key."},
							"factors": map[string]interface{}{
								"type":        "array",
								"description": "Additional factor enrolments.",
								"items": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"name":   map[string]interface{}{"type": "string"},
										"params": map[string]interface{}{"type": "object"},
									},
								},
							},
							"designated": map[string]interface{}{"type": "string", "description": "The factor fixed at enrolment under the designated policy; mtls when empty."},
						},
					},
				},
				"policy": map[string]interface{}{
					"type":        "string",
					"description": "Shamir custody policy for threshold >= 2: designated (default) or any-enrolled.",
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
				"admin_csr_pem": map[string]interface{}{
					"type":        "string",
					"description": "Bootstrap admin CSR to sign. Exactly one of admin_csr_pem, admin_cert_pem or admin_generate is required.",
				},
				"admin_cert_pem": map[string]interface{}{
					"type":        "string",
					"description": "Bootstrap admin's own certificate (bring-your-own).",
				},
				"admin_cert_pool_pem": map[string]interface{}{
					"type":        "string",
					"description": "CA pool that verifies admin_cert_pem.",
				},
				"admin_generate": map[string]interface{}{
					"type":        "boolean",
					"description": "Generate the bootstrap admin credential. Dev/test only.",
				},
				"secret": map[string]interface{}{
					"type":        "string",
					"description": "System barrier passphrase; required when the barrier strategy needs one.",
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
}

// ceremonyToolNames enumerates every tool name owned by the ceremony surface.
var ceremonyToolNames = map[string]struct{}{
	"qrdb_ceremony_initialize":        {},
	"qrdb_ceremony_status":            {},
	"qrdb_ceremony_sign_csr":          {},
	"quicbaas_ceremony_list_officers": {},
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
	"qrdb_ceremony_initialize":        executeCeremonyInitialize,
	"qrdb_ceremony_status":            executeCeremonyStatus,
	"qrdb_ceremony_sign_csr":          executeCeremonySignCSR,
	"quicbaas_ceremony_list_officers": executeCeremonyListOfficers,
}

// executeCeremonyTool dispatches a ceremony-surface call to the appropriate
// executor. Returns CeremonyServiceUnavailableError when the registry has no
// CeremonyServicer wired.
func (r *ToolRegistry) executeCeremonyTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	svc := r.ceremonySvc
	if svc == nil {
		return nil, errCeremonyServiceUnavailableQB
	}
	executor, ok := ceremonyExecutors[name]
	if !ok {
		return nil, errCeremonyServiceUnavailableQB
	}
	return executor(ctx, svc, args)
}

// ---------------------------------------------------------------------------
// Per-tool executors
// ---------------------------------------------------------------------------

// executeCeremonyInitialize implements qrdb_ceremony_initialize.
func executeCeremonyInitialize(ctx context.Context, svc service.CeremonyServicer, args map[string]interface{}) (interface{}, error) {
	adminGenerate, err := getStrictBool(args, "admin_generate")
	if err != nil {
		return nil, err
	}
	officers, err := parseCeremonyOfficers(args)
	if err != nil {
		return nil, err
	}
	secret, err := service.NewCeremonySecret(ctx, credentialArg(args, "secret"))
	if err != nil {
		return nil, err
	}
	if secret != nil {
		defer secret.Destroy()
	}
	req := &service.CeremonyInitRequest{
		SOPin:                  getString(args, "so_pin"),
		UserPin:                getString(args, "user_pin"),
		CredentialSealStrategy: getString(args, "credential_seal_strategy"),
		Threshold:              getInt(args, "threshold"),
		Hostname:               getString(args, "hostname"),
		SANs:                   getStringSlice(args, "sans"),
		Policy:                 getString(args, "policy"),
		Officers:               officers,
		AdminCSRPEM:            getString(args, "admin_csr_pem"),
		AdminCertPEM:           getString(args, "admin_cert_pem"),
		AdminCertPoolPEM:       getString(args, "admin_cert_pool_pem"),
		AdminGenerate:          adminGenerate,
		Secret:                 secret,
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

// ---------------------------------------------------------------------------
// Helpers — argument parsing and response mapping
// ---------------------------------------------------------------------------

// parseCeremonyOfficers decodes the "officers" argument into go-qrdb's own
// officer request type, so every field go-qrdb defines reaches it. An absent
// argument yields a nil roster.
func parseCeremonyOfficers(args map[string]interface{}) ([]service.CeremonyOfficer, error) {
	raw, ok := args["officers"]
	if !ok || raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, &CeremonyOfficersArgError{Cause: err}
	}
	var officers []service.CeremonyOfficer
	if err := json.Unmarshal(data, &officers); err != nil {
		return nil, &CeremonyOfficersArgError{Cause: err}
	}
	return officers, nil
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
	// qrdb returns a generated admin key exactly once: deliver it, the admin
	// certificate and the holder certificates under go-qrdb's JSON names.
	if len(info.Holders) > 0 {
		resp["holders"] = info.Holders
	}
	if len(info.AdminCertPEM) > 0 {
		resp["admin_cert_pem"] = string(info.AdminCertPEM)
	}
	if len(info.AdminKeyPEM) > 0 {
		resp["admin_key_pem"] = info.AdminKeyPEM
	}
	if info.AdminCredentialSource != "" {
		resp["admin_credential_source"] = info.AdminCredentialSource
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

// ceremonySignCSRToMap renders a CeremonySignCSRResult as the JSON wire shape
// shared with handlers_ceremony.go.
func ceremonySignCSRToMap(r *service.CeremonySignCSRResult) map[string]interface{} {
	return map[string]interface{}{
		"cert_pem": string(r.CertPEM),
	}
}
