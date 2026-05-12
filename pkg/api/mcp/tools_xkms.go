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
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/jeremyhahn/go-xkms/sdk/go/transport"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// xkmsToolPrefix is the prefix used for all xkms-compatible MCP tool names.
const xkmsToolPrefix = "xkms."

// XKMSToolHandler is the function signature for xkms tool handlers.
type XKMSToolHandler func(ctx context.Context, args map[string]interface{}) (interface{}, error)

// xkmsHandlers maps xkms tool names to their handler functions.
// Initialized lazily on first call to avoid allocating until needed.
var xkmsHandlerMap map[string]XKMSToolHandler

// ensureXKMSInitialized checks that the xkms service is initialized.
func ensureXKMSInitialized() error {
	if !xkmssdk.ServiceIsInitialized() {
		return &XKMSNotInitializedError{}
	}
	return nil
}

// initXKMSHandlers initializes the handler map for the given ToolRegistry.
// Returns a map-based dispatch table for O(1) tool name lookup.
func (r *ToolRegistry) initXKMSHandlers() map[string]XKMSToolHandler {
	return map[string]XKMSToolHandler{
		// Core key management
		"xkms.listBackends":  r.handleXKMSListBackends,
		"xkms.generateKey":   r.handleXKMSGenerateKey,
		"xkms.getKey":        r.handleXKMSGetKey,
		"xkms.deleteKey":     r.handleXKMSDeleteKey,
		"xkms.listKeys":      r.handleXKMSListKeys,
		"xkms.rotateKey":     r.handleXKMSRotateKey,
		"xkms.sign":          r.handleXKMSSign,
		"xkms.verify":        r.handleXKMSVerify,
		"xkms.encrypt":       r.handleXKMSEncrypt,
		"xkms.decrypt":       r.handleXKMSDecrypt,
		"xkms.saveCert":      r.handleXKMSSaveCert,
		"xkms.getCert":       r.handleXKMSGetCert,
		"xkms.deleteCert":    r.handleXKMSDeleteCert,
		"xkms.listCerts":     r.handleXKMSListCerts,
		"xkms.certExists":    r.handleXKMSCertExists,
		"xkms.saveCertChain": r.handleXKMSSaveCertChain,
		"xkms.getCertChain":  r.handleXKMSGetCertChain,
		"xkms.seal":          r.handleXKMSSeal,
		"xkms.unseal":        r.handleXKMSUnseal,
		"xkms.canSeal":       r.handleXKMSCanSeal,

		// PIV operations
		"xkms.piv.listSlots":   r.handleXKMSPIVListSlots,
		"xkms.piv.getCert":     r.handleXKMSPIVGetCert,
		"xkms.piv.storeCert":   r.handleXKMSPIVStoreCert,
		"xkms.piv.deleteCert":  r.handleXKMSPIVDeleteCert,
		"xkms.piv.generateKey": r.handleXKMSPIVGenerateKey,
		"xkms.piv.generateCSR": r.handleXKMSPIVGenerateCSR,

		// FIDO2 operations
		"xkms.fido2.beginRegistration":    r.handleXKMSFIDO2BeginRegistration,
		"xkms.fido2.finishRegistration":   r.handleXKMSFIDO2FinishRegistration,
		"xkms.fido2.beginAuthentication":  r.handleXKMSFIDO2BeginAuthentication,
		"xkms.fido2.finishAuthentication": r.handleXKMSFIDO2FinishAuthentication,

		// CA operations
		"xkms.ca.getBundle":   r.handleTrustedCAGetBundle,
		"xkms.ca.getCert":     r.handleTrustedCAGetCert,
		"xkms.ca.signCSR":     r.handleTrustedCASignCSR,
		"xkms.ca.issueCert":   r.handleTrustedCAIssueCert,
		"xkms.ca.revokeCert":  r.handleTrustedCARevokeCert,
		"xkms.ca.generateCRL": r.handleTrustedCAGenerateCRL,
		"xkms.ca.isRevoked":   r.handleTrustedCAIsRevoked,

		// PIN operations
		"xkms.pin.setSO":         r.handleXKMSPINSetSO,
		"xkms.pin.setUser":       r.handleXKMSPINSetUser,
		"xkms.pin.changeSO":      r.handleXKMSPINChangeSO,
		"xkms.pin.changeUser":    r.handleXKMSPINChangeUser,
		"xkms.pin.verifySO":      r.handleXKMSPINVerifySO,
		"xkms.pin.verifyUser":    r.handleXKMSPINVerifyUser,
		"xkms.pin.lockoutStatus": r.handleXKMSPINLockoutStatus,
		"xkms.pin.resetLockout":  r.handleXKMSPINResetLockout,
	}
}

// isXKMSTool returns true if the tool name is an xkms-compatible tool.
func isXKMSTool(name string) bool {
	return strings.HasPrefix(name, xkmsToolPrefix)
}

// executeXKMSTool dispatches an xkms tool call to the appropriate handler.
func (r *ToolRegistry) executeXKMSTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	if xkmsHandlerMap == nil {
		xkmsHandlerMap = r.initXKMSHandlers()
	}

	handler, ok := xkmsHandlerMap[name]
	if !ok {
		return nil, api.NewUnknownToolError(name)
	}

	return handler(ctx, args)
}

// registerXKMSTools registers all xkms-compatible MCP tool definitions.
func (r *ToolRegistry) registerXKMSTools() {
	r.registerXKMSCoreTools()
	r.registerXKMSPIVTools()
	r.registerXKMSFIDO2Tools()
	r.registerTrustedCATools()
	r.registerXKMSPINTools()
}

// registerXKMSCoreTools registers the core xkms key management tool definitions.
func (r *ToolRegistry) registerXKMSCoreTools() {
	r.addTool(ToolDefinition{
		Name:        "xkms.listBackends",
		Description: "List available cryptographic backends",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.generateKey",
		Description: "Generate a new cryptographic key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":     map[string]interface{}{"type": "string", "description": "Unique key identifier"},
				"backend":    map[string]interface{}{"type": "string", "description": "Backend to use (e.g., software, pkcs11, tpm2)"},
				"key_type":   map[string]interface{}{"type": "string", "description": "Key type (rsa, ecdsa, ed25519, symmetric)"},
				"key_size":   map[string]interface{}{"type": "integer", "description": "Key size in bits (for RSA)"},
				"curve":      map[string]interface{}{"type": "string", "description": "Elliptic curve (P-256, P-384, P-521)"},
				"algorithm":  map[string]interface{}{"type": "string", "description": "Algorithm specification"},
				"exportable": map[string]interface{}{"type": "boolean", "description": "Whether the key can be exported"},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.getKey",
		Description: "Retrieve a key by its identifier",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":  map[string]interface{}{"type": "string", "description": "Key identifier"},
				"backend": map[string]interface{}{"type": "string", "description": "Backend containing the key"},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.deleteKey",
		Description: "Delete a cryptographic key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":  map[string]interface{}{"type": "string", "description": "Key identifier"},
				"backend": map[string]interface{}{"type": "string", "description": "Backend containing the key"},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.listKeys",
		Description: "List all keys, optionally filtered by backend",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend": map[string]interface{}{"type": "string", "description": "Filter keys by backend"},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.rotateKey",
		Description: "Rotate (replace) an existing key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":  map[string]interface{}{"type": "string", "description": "Key identifier to rotate"},
				"backend": map[string]interface{}{"type": "string", "description": "Backend containing the key"},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.sign",
		Description: "Sign data using a cryptographic key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":  map[string]interface{}{"type": "string", "description": "Key identifier for signing"},
				"backend": map[string]interface{}{"type": "string", "description": "Backend containing the key"},
				"data":    map[string]interface{}{"type": "string", "description": "Base64-encoded data to sign"},
				"hash":    map[string]interface{}{"type": "string", "description": "Hash algorithm (SHA-256, SHA-384, SHA-512)"},
			},
			"required": []string{"key_id", "data"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.verify",
		Description: "Verify a signature against data",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":    map[string]interface{}{"type": "string", "description": "Key identifier for verification"},
				"backend":   map[string]interface{}{"type": "string", "description": "Backend containing the key"},
				"data":      map[string]interface{}{"type": "string", "description": "Base64-encoded original data"},
				"signature": map[string]interface{}{"type": "string", "description": "Base64-encoded signature to verify"},
				"hash":      map[string]interface{}{"type": "string", "description": "Hash algorithm used for signing"},
			},
			"required": []string{"key_id", "data", "signature"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.encrypt",
		Description: "Encrypt data using a symmetric key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":          map[string]interface{}{"type": "string", "description": "Key identifier for encryption"},
				"backend":         map[string]interface{}{"type": "string", "description": "Backend containing the key"},
				"plaintext":       map[string]interface{}{"type": "string", "description": "Base64-encoded plaintext to encrypt"},
				"additional_data": map[string]interface{}{"type": "string", "description": "Base64-encoded additional authenticated data"},
				"aad":             map[string]interface{}{"type": "string", "description": "Base64-encoded additional authenticated data (alias for additional_data)"},
			},
			"required": []string{"key_id", "plaintext"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.decrypt",
		Description: "Decrypt data using a symmetric key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":          map[string]interface{}{"type": "string", "description": "Key identifier for decryption"},
				"backend":         map[string]interface{}{"type": "string", "description": "Backend containing the key"},
				"ciphertext":      map[string]interface{}{"type": "string", "description": "Base64-encoded ciphertext"},
				"nonce":           map[string]interface{}{"type": "string", "description": "Base64-encoded nonce/IV"},
				"tag":             map[string]interface{}{"type": "string", "description": "Base64-encoded authentication tag"},
				"additional_data": map[string]interface{}{"type": "string", "description": "Base64-encoded additional authenticated data"},
				"aad":             map[string]interface{}{"type": "string", "description": "Base64-encoded additional authenticated data (alias for additional_data)"},
			},
			"required": []string{"key_id", "ciphertext"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.saveCert",
		Description: "Save a certificate for a key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":   map[string]interface{}{"type": "string", "description": "Key identifier to associate the certificate with"},
				"backend":  map[string]interface{}{"type": "string", "description": "Backend to store the certificate"},
				"cert_pem": map[string]interface{}{"type": "string", "description": "PEM-encoded certificate"},
			},
			"required": []string{"key_id", "cert_pem"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.getCert",
		Description: "Retrieve a certificate by key identifier",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":  map[string]interface{}{"type": "string", "description": "Key identifier"},
				"backend": map[string]interface{}{"type": "string", "description": "Backend containing the certificate"},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.deleteCert",
		Description: "Delete a certificate by key identifier",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":  map[string]interface{}{"type": "string", "description": "Key identifier"},
				"backend": map[string]interface{}{"type": "string", "description": "Backend containing the certificate"},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.listCerts",
		Description: "List all certificate identifiers",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend": map[string]interface{}{"type": "string", "description": "Filter certificates by backend"},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.certExists",
		Description: "Check if a certificate exists for a key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":  map[string]interface{}{"type": "string", "description": "Key identifier"},
				"backend": map[string]interface{}{"type": "string", "description": "Backend to check"},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.saveCertChain",
		Description: "Save a certificate chain for a key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":     map[string]interface{}{"type": "string", "description": "Key identifier"},
				"backend":    map[string]interface{}{"type": "string", "description": "Backend to store the chain"},
				"chain_pems": map[string]interface{}{"type": "array", "description": "PEM-encoded certificates in chain order", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"key_id", "chain_pems"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.getCertChain",
		Description: "Retrieve a certificate chain by key identifier",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id":  map[string]interface{}{"type": "string", "description": "Key identifier"},
				"backend": map[string]interface{}{"type": "string", "description": "Backend containing the chain"},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.seal",
		Description: "Seal (encrypt and protect) data using a backend's sealing mechanism",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend": map[string]interface{}{"type": "string", "description": "Backend to use for sealing"},
				"key_id":  map[string]interface{}{"type": "string", "description": "Key identifier for sealing"},
				"data":    map[string]interface{}{"type": "string", "description": "Base64-encoded data to seal"},
				"aad":     map[string]interface{}{"type": "string", "description": "Base64-encoded additional authenticated data"},
			},
			"required": []string{"data"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.unseal",
		Description: "Unseal (decrypt and verify) previously sealed data",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend":    map[string]interface{}{"type": "string", "description": "Backend used for sealing"},
				"key_id":     map[string]interface{}{"type": "string", "description": "Key identifier used for sealing"},
				"ciphertext": map[string]interface{}{"type": "string", "description": "Base64-encoded sealed ciphertext"},
				"nonce":      map[string]interface{}{"type": "string", "description": "Base64-encoded nonce/IV"},
				"tag":        map[string]interface{}{"type": "string", "description": "Base64-encoded authentication tag"},
				"aad":        map[string]interface{}{"type": "string", "description": "Base64-encoded additional authenticated data"},
			},
			"required": []string{"ciphertext"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.canSeal",
		Description: "Check if a backend supports sealing operations",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend": map[string]interface{}{"type": "string", "description": "Backend to check"},
			},
		},
	})
}

// registerXKMSPIVTools registers PIV tool definitions.
func (r *ToolRegistry) registerXKMSPIVTools() {
	r.addTool(ToolDefinition{
		Name:        "xkms.piv.listSlots",
		Description: "List PIV slots and their status",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend": map[string]interface{}{"type": "string", "description": "Backend containing PIV slots"},
			},
			"required": []string{"backend"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.piv.getCert",
		Description: "Get certificate from a PIV slot",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend": map[string]interface{}{"type": "string", "description": "Backend containing the PIV slot"},
				"slot":    map[string]interface{}{"type": "string", "description": "PIV slot identifier (e.g., 9a, 9c, 9d, 9e)"},
				"format":  map[string]interface{}{"type": "string", "description": "Certificate format: pem or der"},
			},
			"required": []string{"backend", "slot"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.piv.storeCert",
		Description: "Store a certificate in a PIV slot",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend":     map[string]interface{}{"type": "string", "description": "Backend containing the PIV slot"},
				"slot":        map[string]interface{}{"type": "string", "description": "PIV slot identifier"},
				"certificate": map[string]interface{}{"type": "string", "description": "Base64-encoded certificate data"},
				"format":      map[string]interface{}{"type": "string", "description": "Certificate format: pem or der"},
			},
			"required": []string{"backend", "slot", "certificate", "format"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.piv.deleteCert",
		Description: "Delete a certificate from a PIV slot",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend": map[string]interface{}{"type": "string", "description": "Backend containing the PIV slot"},
				"slot":    map[string]interface{}{"type": "string", "description": "PIV slot identifier"},
			},
			"required": []string{"backend", "slot"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.piv.generateKey",
		Description: "Generate a key pair in a PIV slot with a self-signed certificate",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend":   map[string]interface{}{"type": "string", "description": "Backend containing the PIV slot"},
				"slot":      map[string]interface{}{"type": "string", "description": "PIV slot identifier"},
				"algorithm": map[string]interface{}{"type": "string", "description": "Key algorithm (rsa2048, rsa4096, ecdsap256, ecdsap384, ed25519)"},
				"subject":   map[string]interface{}{"type": "string", "description": "Certificate subject common name"},
			},
			"required": []string{"backend", "slot"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.piv.generateCSR",
		Description: "Generate a certificate signing request for a PIV slot",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"backend": map[string]interface{}{"type": "string", "description": "Backend containing the PIV slot"},
				"slot":    map[string]interface{}{"type": "string", "description": "PIV slot identifier"},
				"subject": map[string]interface{}{"type": "string", "description": "CSR subject common name"},
			},
			"required": []string{"backend", "slot"},
		},
	})
}

// registerXKMSFIDO2Tools registers FIDO2/WebAuthn tool definitions.
func (r *ToolRegistry) registerXKMSFIDO2Tools() {
	r.addTool(ToolDefinition{
		Name:        "xkms.fido2.beginRegistration",
		Description: "Begin WebAuthn registration ceremony",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username":     map[string]interface{}{"type": "string", "description": "Username for registration"},
				"display_name": map[string]interface{}{"type": "string", "description": "Display name for the user"},
				"rp_id":        map[string]interface{}{"type": "string", "description": "Relying Party identifier"},
				"rp_name":      map[string]interface{}{"type": "string", "description": "Relying Party display name"},
			},
			"required": []string{"username"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.fido2.finishRegistration",
		Description: "Complete WebAuthn registration ceremony",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username":         map[string]interface{}{"type": "string", "description": "Username for registration"},
				"credential_id":    map[string]interface{}{"type": "string", "description": "Base64-encoded credential ID"},
				"public_key":       map[string]interface{}{"type": "string", "description": "Base64-encoded COSE public key"},
				"attestation_data": map[string]interface{}{"type": "string", "description": "Base64-encoded attestation data"},
				"client_data_json": map[string]interface{}{"type": "string", "description": "Base64-encoded client data JSON"},
			},
			"required": []string{"username", "credential_id", "public_key", "attestation_data", "client_data_json"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.fido2.beginAuthentication",
		Description: "Begin WebAuthn authentication ceremony",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username": map[string]interface{}{"type": "string", "description": "Username for authentication"},
				"rp_id":    map[string]interface{}{"type": "string", "description": "Relying Party identifier"},
			},
			"required": []string{"username"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.fido2.finishAuthentication",
		Description: "Complete WebAuthn authentication ceremony",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"username":           map[string]interface{}{"type": "string", "description": "Username for authentication"},
				"credential_id":      map[string]interface{}{"type": "string", "description": "Base64-encoded credential ID"},
				"signature":          map[string]interface{}{"type": "string", "description": "Base64-encoded signature"},
				"authenticator_data": map[string]interface{}{"type": "string", "description": "Base64-encoded authenticator data"},
				"client_data_json":   map[string]interface{}{"type": "string", "description": "Base64-encoded client data JSON"},
			},
			"required": []string{"username", "credential_id", "signature", "authenticator_data", "client_data_json"},
		},
	})
}

// registerTrustedCATools registers CA (Certificate Authority) tool definitions.
func (r *ToolRegistry) registerTrustedCATools() {
	r.addTool(ToolDefinition{
		Name:        "xkms.ca.getBundle",
		Description: "Get the CA certificate bundle",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"store_type": map[string]interface{}{"type": "string", "description": "Filter by store type (software, tpm2, pkcs11)"},
				"algorithm":  map[string]interface{}{"type": "string", "description": "Filter by algorithm (RSA, ECDSA, Ed25519)"},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.ca.getCert",
		Description: "Get the CA certificate",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"identity": map[string]interface{}{"type": "string", "description": "CA identity (CN), empty for default"},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.ca.signCSR",
		Description: "Sign a certificate signing request",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"csr_pem":       map[string]interface{}{"type": "string", "description": "PEM-encoded PKCS#10 CSR"},
				"profile":       map[string]interface{}{"type": "string", "description": "Certificate profile (server, client, idevid)"},
				"validity_days": map[string]interface{}{"type": "integer", "description": "Override default validity in days"},
			},
			"required": []string{"csr_pem"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.ca.issueCert",
		Description: "Issue a new certificate",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"profile":       map[string]interface{}{"type": "string", "description": "Certificate profile (server, client, idevid)"},
				"common_name":   map[string]interface{}{"type": "string", "description": "Certificate common name"},
				"organization":  map[string]interface{}{"type": "string", "description": "Organization name"},
				"sans":          map[string]interface{}{"type": "array", "description": "Subject alternative names", "items": map[string]interface{}{"type": "string"}},
				"validity_days": map[string]interface{}{"type": "integer", "description": "Validity period in days"},
				"algorithm":     map[string]interface{}{"type": "string", "description": "Key algorithm (ecdsa-p256, rsa2048, ed25519)"},
			},
			"required": []string{"profile", "common_name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.ca.revokeCert",
		Description: "Revoke a certificate by serial number",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"serial_number": map[string]interface{}{"type": "string", "description": "Certificate serial number"},
				"reason":        map[string]interface{}{"type": "integer", "description": "RFC 5280 revocation reason code"},
			},
			"required": []string{"serial_number"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.ca.generateCRL",
		Description: "Generate a certificate revocation list",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.ca.isRevoked",
		Description: "Check if a certificate is revoked",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"serial_number": map[string]interface{}{"type": "string", "description": "Certificate serial number to check"},
			},
			"required": []string{"serial_number"},
		},
	})
}

// registerXKMSPINTools registers PIN management tool definitions.
func (r *ToolRegistry) registerXKMSPINTools() {
	r.addTool(ToolDefinition{
		Name:        "xkms.pin.setSO",
		Description: "Set the Security Officer PIN",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"current_so_pin": map[string]interface{}{"type": "string", "description": "Current SO PIN (empty if not yet set)"},
				"new_so_pin":     map[string]interface{}{"type": "string", "description": "New SO PIN to set"},
			},
			"required": []string{"new_so_pin"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.pin.setUser",
		Description: "Set the user PIN using the SO PIN",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"so_pin":       map[string]interface{}{"type": "string", "description": "Security Officer PIN"},
				"new_user_pin": map[string]interface{}{"type": "string", "description": "New user PIN to set"},
			},
			"required": []string{"so_pin", "new_user_pin"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.pin.changeSO",
		Description: "Change the Security Officer PIN",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"current_so_pin": map[string]interface{}{"type": "string", "description": "Current SO PIN"},
				"new_so_pin":     map[string]interface{}{"type": "string", "description": "New SO PIN"},
			},
			"required": []string{"current_so_pin", "new_so_pin"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.pin.changeUser",
		Description: "Change the user PIN",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"current_user_pin": map[string]interface{}{"type": "string", "description": "Current user PIN"},
				"new_user_pin":     map[string]interface{}{"type": "string", "description": "New user PIN"},
			},
			"required": []string{"current_user_pin", "new_user_pin"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.pin.verifySO",
		Description: "Verify the Security Officer PIN",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"so_pin": map[string]interface{}{"type": "string", "description": "SO PIN to verify"},
			},
			"required": []string{"so_pin"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.pin.verifyUser",
		Description: "Verify the user PIN",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_pin": map[string]interface{}{"type": "string", "description": "User PIN to verify"},
			},
			"required": []string{"user_pin"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.pin.lockoutStatus",
		Description: "Get the current PIN lockout status",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "xkms.pin.resetLockout",
		Description: "Reset PIN lockout using the SO PIN",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"so_pin": map[string]interface{}{"type": "string", "description": "Security Officer PIN for authorization"},
			},
			"required": []string{"so_pin"},
		},
	})
}

// =============================================================================
// Core XKMS Tool Handlers
// =============================================================================

// xkmsKeyAttrsFromArgs creates KeyAttributes from MCP arguments, injecting
// the tenant ID from the auth context for multi-tenant isolation.
func xkmsKeyAttrsFromArgs(ctx context.Context, args map[string]interface{}) *xkmssdk.KeyAttributes {
	attrs := &xkmssdk.KeyAttributes{
		CN:         getString(args, "key_id"),
		Exportable: getBool(args, "exportable"),
	}

	if backend := getString(args, "backend"); backend != "" {
		attrs.StoreType = xkmssdk.ParseStoreType(backend)
	}

	// Inject tenant ID from auth context for multi-tenant isolation
	if tenantID := iam.TenantFromContext(ctx); tenantID != "" {
		attrs.TenantID = tenantID
	}

	return attrs
}

// parseHashAlgorithm converts a hash algorithm string to crypto.Hash.
func parseHashAlgorithm(hashStr string) crypto.Hash {
	hashMap := map[string]crypto.Hash{
		"sha-256": crypto.SHA256,
		"sha-384": crypto.SHA384,
		"sha-512": crypto.SHA512,
		"sha256":  crypto.SHA256,
		"sha384":  crypto.SHA384,
		"sha512":  crypto.SHA512,
	}

	if h, ok := hashMap[strings.ToLower(hashStr)]; ok {
		return h
	}
	return crypto.SHA256
}

// decodeBase64Arg extracts and decodes a base64-encoded argument.
// Returns nil if the argument is missing or empty.
// Returns a Base64DecodeError if decoding fails.
func decodeBase64Arg(args map[string]interface{}, key string) ([]byte, error) {
	s := getString(args, key)
	if s == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, &Base64DecodeError{Field: key, Cause: err}
	}
	return data, nil
}

// decodeAADArg extracts and decodes the additional authenticated data argument.
// Accepts both "additional_data" (canonical) and "aad" (shorthand alias) keys,
// consistent with the seal/unseal handlers that use "aad".
func decodeAADArg(args map[string]interface{}) ([]byte, error) {
	aad, err := decodeBase64Arg(args, "additional_data")
	if err != nil {
		return nil, err
	}
	if aad != nil {
		return aad, nil
	}
	return decodeBase64Arg(args, "aad")
}

// handleXKMSListBackends returns the list of available cryptographic backends.
func (r *ToolRegistry) handleXKMSListBackends(_ context.Context, _ map[string]interface{}) (interface{}, error) {
	backends := xkmssdk.ServiceBackends()
	if backends == nil {
		backends = []string{}
	}
	return marshalResult(map[string]interface{}{
		"backends": backends,
	})
}

// handleXKMSGenerateKey generates a new cryptographic key via xkms.
func (r *ToolRegistry) handleXKMSGenerateKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	attrs := xkmsKeyAttrsFromArgs(ctx, args)

	// Determine key algorithm from key_type and algorithm fields
	keyType := strings.ToLower(getString(args, "key_type"))
	algorithm := strings.ToLower(getString(args, "algorithm"))

	switch {
	case keyType == "rsa" || algorithm == "rsa":
		attrs.KeyAlgorithm = x509.RSA
		attrs.KeyType = xkmssdk.KeyTypeSigning
		keySize := getInt(args, "key_size")
		if keySize == 0 {
			keySize = 2048
		}
		attrs.RSAAttributes = &xkmssdk.RSAAttributes{KeySize: keySize}

	case keyType == "ecdsa" || algorithm == "ecdsa":
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.KeyType = xkmssdk.KeyTypeSigning
		curve := getString(args, "curve")
		if curve == "" {
			curve = "P-256"
		}
		parsedCurve, err := xkmssdk.ParseCurve(curve)
		if err != nil {
			return nil, &XKMSInvalidCurveError{Curve: curve}
		}
		attrs.ECCAttributes = &xkmssdk.ECCAttributes{Curve: parsedCurve}

	case keyType == "ed25519" || algorithm == "ed25519":
		attrs.KeyAlgorithm = x509.Ed25519
		attrs.KeyType = xkmssdk.KeyTypeSigning

	default:
		// Default to ECDSA P-256
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.KeyType = xkmssdk.KeyTypeSigning
		defaultCurve, _ := xkmssdk.ParseCurve("P-256")
		attrs.ECCAttributes = &xkmssdk.ECCAttributes{Curve: defaultCurve}
	}

	// Use the specified backend or default
	backendName := getString(args, "backend")
	var privKey crypto.PrivateKey
	var err error

	if backendName != "" {
		privKey, err = xkmssdk.ServiceGenerateKeyWithBackend(backendName, attrs)
	} else {
		privKey, err = xkmssdk.ServiceGenerateKey(attrs)
	}
	if err != nil {
		return nil, &XKMSOperationError{Operation: "generateKey", Cause: err}
	}

	// Extract public key PEM if available
	publicKeyPEM := extractPublicKeyPEM(privKey)

	return marshalResult(map[string]interface{}{
		"key_id":         keyID,
		"public_key_pem": publicKeyPEM,
	})
}

// handleXKMSGetKey retrieves key metadata by key identifier.
func (r *ToolRegistry) handleXKMSGetKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	attrs := xkmsKeyAttrsFromArgs(ctx, args)

	privKey, err := xkmssdk.ServiceKey(attrs)
	if err != nil {
		return nil, &XKMSOperationError{Operation: "getKey", Cause: err}
	}

	publicKeyPEM := extractPublicKeyPEM(privKey)
	backend := getString(args, "backend")

	return marshalResult(map[string]interface{}{
		"key_id":         keyID,
		"public_key_pem": publicKeyPEM,
		"backend":        backend,
	})
}

// handleXKMSDeleteKey deletes a key by its identifier.
func (r *ToolRegistry) handleXKMSDeleteKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	attrs := xkmsKeyAttrsFromArgs(ctx, args)

	if err := xkmssdk.ServiceDeleteKey(attrs); err != nil {
		return nil, &XKMSOperationError{Operation: "deleteKey", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"success": true,
		"message": "key deleted",
	})
}

// handleXKMSListKeys lists all keys, optionally filtered by backend.
func (r *ToolRegistry) handleXKMSListKeys(_ context.Context, args map[string]interface{}) (interface{}, error) {
	backend := getString(args, "backend")

	var keys []*xkmssdk.KeyAttributes
	var err error

	if backend != "" {
		keys, err = xkmssdk.ServiceListKeys(backend)
	} else {
		keys, err = xkmssdk.ServiceListKeys()
	}
	if err != nil {
		return nil, &XKMSOperationError{Operation: "listKeys", Cause: err}
	}

	keyInfos := make([]map[string]interface{}, 0, len(keys))
	for _, k := range keys {
		keyInfos = append(keyInfos, map[string]interface{}{
			"cn": k.CN,
		})
	}

	return marshalResult(map[string]interface{}{
		"keys": keyInfos,
	})
}

// handleXKMSRotateKey rotates an existing key.
func (r *ToolRegistry) handleXKMSRotateKey(_ context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	// Build the kid string for xkmssdk.ServiceRotateKey
	kid := keyID
	if backend := getString(args, "backend"); backend != "" {
		kid = backend + ":::" + keyID
	}

	privKey, err := xkmssdk.ServiceRotateKey(kid)
	if err != nil {
		return nil, &XKMSOperationError{Operation: "rotateKey", Cause: err}
	}

	publicKeyPEM := extractPublicKeyPEM(privKey)

	return marshalResult(map[string]interface{}{
		"key_id":         keyID,
		"public_key_pem": publicKeyPEM,
	})
}

// handleXKMSSign signs data using a cryptographic key.
func (r *ToolRegistry) handleXKMSSign(_ context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	data, err := decodeBase64Arg(args, "data")
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, api.NewRequiredFieldError("data")
	}

	hashStr := getString(args, "hash")
	hashAlg := parseHashAlgorithm(hashStr)

	// Build the kid string
	kid := keyID
	if backend := getString(args, "backend"); backend != "" {
		kid = backend + ":::" + keyID
	}

	signature, err := xkmssdk.ServiceSign(kid, data, &xkmssdk.SignOptions{Hash: hashAlg})
	if err != nil {
		return nil, &XKMSOperationError{Operation: "sign", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"signature": base64.StdEncoding.EncodeToString(signature),
	})
}

// handleXKMSVerify verifies a signature against data.
func (r *ToolRegistry) handleXKMSVerify(_ context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	data, err := decodeBase64Arg(args, "data")
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, api.NewRequiredFieldError("data")
	}

	signature, err := decodeBase64Arg(args, "signature")
	if err != nil {
		return nil, err
	}
	if signature == nil {
		return nil, api.NewRequiredFieldError("signature")
	}

	hashStr := getString(args, "hash")
	hashAlg := parseHashAlgorithm(hashStr)

	// Build the kid string
	kid := keyID
	if backend := getString(args, "backend"); backend != "" {
		kid = backend + ":::" + keyID
	}

	verifyErr := xkmssdk.ServiceVerify(kid, data, signature, &xkmssdk.VerifyOpts{Hash: hashAlg})

	return marshalResult(map[string]interface{}{
		"valid": verifyErr == nil,
	})
}

// handleXKMSEncrypt encrypts data using a symmetric key.
func (r *ToolRegistry) handleXKMSEncrypt(_ context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	plaintext, err := decodeBase64Arg(args, "plaintext")
	if err != nil {
		return nil, err
	}
	if plaintext == nil {
		return nil, api.NewRequiredFieldError("plaintext")
	}

	aad, err := decodeAADArg(args)
	if err != nil {
		return nil, err
	}

	// Build the kid string
	kid := keyID
	if backend := getString(args, "backend"); backend != "" {
		kid = backend + ":::" + keyID
	}

	opts := &xkmssdk.EncryptOptions{
		AdditionalData: aad,
	}

	encrypted, err := xkmssdk.ServiceEncrypt(kid, plaintext, opts)
	if err != nil {
		return nil, &XKMSOperationError{Operation: "encrypt", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"ciphertext": base64.StdEncoding.EncodeToString(encrypted.Ciphertext),
		"nonce":      base64.StdEncoding.EncodeToString(encrypted.Nonce),
		"tag":        base64.StdEncoding.EncodeToString(encrypted.Tag),
	})
}

// handleXKMSDecrypt decrypts data using a symmetric key.
func (r *ToolRegistry) handleXKMSDecrypt(_ context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	ciphertext, err := decodeBase64Arg(args, "ciphertext")
	if err != nil {
		return nil, err
	}
	if ciphertext == nil {
		return nil, api.NewRequiredFieldError("ciphertext")
	}

	nonce, err := decodeBase64Arg(args, "nonce")
	if err != nil {
		return nil, err
	}

	tag, err := decodeBase64Arg(args, "tag")
	if err != nil {
		return nil, err
	}

	aad, err := decodeAADArg(args)
	if err != nil {
		return nil, err
	}

	// Build the kid string
	kid := keyID
	if backend := getString(args, "backend"); backend != "" {
		kid = backend + ":::" + keyID
	}

	opts := &xkmssdk.DecryptOptions{
		AdditionalData: aad,
	}

	encrypted := &xkmssdk.EncryptedData{
		Ciphertext: ciphertext,
		Nonce:      nonce,
		Tag:        tag,
	}

	plaintext, err := xkmssdk.ServiceDecrypt(kid, encrypted, opts)
	if err != nil {
		return nil, &XKMSOperationError{Operation: "decrypt", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"plaintext": base64.StdEncoding.EncodeToString(plaintext),
	})
}

// handleXKMSSaveCert saves a certificate by key identifier.
func (r *ToolRegistry) handleXKMSSaveCert(_ context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	certPEM := getString(args, "cert_pem")
	if certPEM == "" {
		return nil, api.NewRequiredFieldError("cert_pem")
	}

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, &XKMSInvalidPEMError{Field: "cert_pem"}
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, &XKMSCertificateParseError{Cause: err}
	}

	if err := xkmssdk.ServiceSaveCertificateByID(keyID, cert); err != nil {
		return nil, &XKMSOperationError{Operation: "saveCert", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"success": true,
	})
}

// handleXKMSGetCert retrieves a certificate by key identifier.
func (r *ToolRegistry) handleXKMSGetCert(_ context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	cert, err := xkmssdk.ServiceCertificateByID(keyID)
	if err != nil {
		return nil, &XKMSOperationError{Operation: "getCert", Cause: err}
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})

	return marshalResult(map[string]interface{}{
		"key_id":   keyID,
		"cert_pem": string(certPEM),
	})
}

// handleXKMSDeleteCert deletes a certificate by key identifier.
func (r *ToolRegistry) handleXKMSDeleteCert(_ context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	if err := xkmssdk.ServiceDeleteCertificateByID(keyID); err != nil {
		return nil, &XKMSOperationError{Operation: "deleteCert", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"success": true,
	})
}

// handleXKMSListCerts lists all certificate identifiers.
func (r *ToolRegistry) handleXKMSListCerts(_ context.Context, args map[string]interface{}) (interface{}, error) {
	backend := getString(args, "backend")

	var certs []string
	var err error

	if backend != "" {
		certs, err = xkmssdk.ServiceListCertificates(backend)
	} else {
		certs, err = xkmssdk.ServiceListCertificates()
	}
	if err != nil {
		return nil, &XKMSOperationError{Operation: "listCerts", Cause: err}
	}

	if certs == nil {
		certs = []string{}
	}

	return marshalResult(map[string]interface{}{
		"key_ids": certs,
	})
}

// handleXKMSCertExists checks if a certificate exists for a key.
func (r *ToolRegistry) handleXKMSCertExists(_ context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	exists, err := xkmssdk.ServiceCertificateExistsByID(keyID)
	if err != nil {
		return nil, &XKMSOperationError{Operation: "certExists", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"exists": exists,
	})
}

// handleXKMSSaveCertChain saves a certificate chain for a key.
func (r *ToolRegistry) handleXKMSSaveCertChain(_ context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	chainPEMs := getStringSlice(args, "chain_pems")
	if len(chainPEMs) == 0 {
		return nil, api.NewRequiredFieldError("chain_pems")
	}

	chain := make([]*x509.Certificate, 0, len(chainPEMs))
	for _, certPEM := range chainPEMs {
		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			return nil, &XKMSInvalidPEMError{Field: "chain_pems"}
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, &XKMSCertificateParseError{Cause: err}
		}
		chain = append(chain, cert)
	}

	if err := xkmssdk.ServiceSaveCertificateChainByID(keyID, chain); err != nil {
		return nil, &XKMSOperationError{Operation: "saveCertChain", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"success": true,
	})
}

// handleXKMSGetCertChain retrieves a certificate chain by key identifier.
func (r *ToolRegistry) handleXKMSGetCertChain(_ context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	chain, err := xkmssdk.ServiceCertificateChainByID(keyID)
	if err != nil {
		return nil, &XKMSOperationError{Operation: "getCertChain", Cause: err}
	}

	chainPEMs := make([]string, 0, len(chain))
	for _, cert := range chain {
		certPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert.Raw,
		})
		chainPEMs = append(chainPEMs, string(certPEM))
	}

	return marshalResult(map[string]interface{}{
		"key_id":     keyID,
		"chain_pems": chainPEMs,
	})
}

// handleXKMSSeal seals data using a backend's sealing mechanism.
func (r *ToolRegistry) handleXKMSSeal(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	data, err := decodeBase64Arg(args, "data")
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, api.NewRequiredFieldError("data")
	}

	aad, err := decodeBase64Arg(args, "aad")
	if err != nil {
		return nil, err
	}

	backendName := getString(args, "backend")

	opts := &xkmssdk.SealOptions{
		AAD: aad,
	}

	var sealed *xkmssdk.SealedData
	if backendName != "" {
		sealed, err = xkmssdk.ServiceSealWithBackend(ctx, backendName, data, opts)
	} else {
		sealed, err = xkmssdk.ServiceSeal(ctx, data, opts)
	}
	if err != nil {
		return nil, &XKMSOperationError{Operation: "seal", Cause: err}
	}

	result := map[string]interface{}{
		"backend":    string(sealed.Backend),
		"ciphertext": base64.StdEncoding.EncodeToString(sealed.Ciphertext),
	}
	if len(sealed.Nonce) > 0 {
		result["nonce"] = base64.StdEncoding.EncodeToString(sealed.Nonce)
	}
	if len(sealed.Tag) > 0 {
		result["tag"] = base64.StdEncoding.EncodeToString(sealed.Tag)
	}

	return marshalResult(result)
}

// handleXKMSUnseal unseals previously sealed data.
func (r *ToolRegistry) handleXKMSUnseal(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	ciphertext, err := decodeBase64Arg(args, "ciphertext")
	if err != nil {
		return nil, err
	}
	if ciphertext == nil {
		return nil, api.NewRequiredFieldError("ciphertext")
	}

	nonce, err := decodeBase64Arg(args, "nonce")
	if err != nil {
		return nil, err
	}

	tag, err := decodeBase64Arg(args, "tag")
	if err != nil {
		return nil, err
	}

	aad, err := decodeBase64Arg(args, "aad")
	if err != nil {
		return nil, err
	}

	backendName := getString(args, "backend")
	backendType := xkmssdk.BackendType(backendName)

	sealed := &xkmssdk.SealedData{
		Backend:    backendType,
		Ciphertext: ciphertext,
		Nonce:      nonce,
		Tag:        tag,
	}

	opts := &xkmssdk.UnsealOptions{
		AAD: aad,
	}

	var plaintext []byte
	if backendName != "" {
		plaintext, err = xkmssdk.ServiceUnsealWithBackend(ctx, backendName, sealed, opts)
	} else {
		plaintext, err = xkmssdk.ServiceUnseal(ctx, sealed, opts)
	}
	if err != nil {
		return nil, &XKMSOperationError{Operation: "unseal", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"plaintext": base64.StdEncoding.EncodeToString(plaintext),
	})
}

// handleXKMSCanSeal checks if a backend supports sealing operations.
func (r *ToolRegistry) handleXKMSCanSeal(_ context.Context, args map[string]interface{}) (interface{}, error) {
	backend := getString(args, "backend")

	var canSeal bool
	if backend != "" {
		canSeal = xkmssdk.ServiceCanSeal(backend)
	} else {
		canSeal = xkmssdk.ServiceCanSeal()
	}

	return marshalResult(map[string]interface{}{
		"can_seal": canSeal,
	})
}

// =============================================================================
// PIV Tool Handlers
// =============================================================================

// handleXKMSPIVListSlots lists PIV slots and their status.
func (r *ToolRegistry) handleXKMSPIVListSlots(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	backend := getString(args, "backend")
	if backend == "" {
		return nil, api.NewRequiredFieldError("backend")
	}

	resp, err := xkmssdk.ServiceListPIVSlots(ctx, &transport.ListPIVSlotsRequest{
		Backend: backend,
	})
	if err != nil {
		return nil, &XKMSOperationError{Operation: "piv.listSlots", Cause: err}
	}

	return marshalResult(resp)
}

// handleXKMSPIVGetCert retrieves a certificate from a PIV slot.
func (r *ToolRegistry) handleXKMSPIVGetCert(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	backend := getString(args, "backend")
	if backend == "" {
		return nil, api.NewRequiredFieldError("backend")
	}

	slot := getString(args, "slot")
	if slot == "" {
		return nil, api.NewRequiredFieldError("slot")
	}

	format := getString(args, "format")
	if format == "" {
		format = "pem"
	}

	resp, err := xkmssdk.ServiceGetPIVCertificate(ctx, &transport.GetPIVCertificateRequest{
		Backend: backend,
		Slot:    slot,
		Format:  format,
	})
	if err != nil {
		return nil, &XKMSOperationError{Operation: "piv.getCert", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"slot":        resp.Slot,
		"certificate": base64.StdEncoding.EncodeToString(resp.Certificate),
		"format":      resp.Format,
	})
}

// handleXKMSPIVStoreCert stores a certificate in a PIV slot.
func (r *ToolRegistry) handleXKMSPIVStoreCert(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	backend := getString(args, "backend")
	if backend == "" {
		return nil, api.NewRequiredFieldError("backend")
	}

	slot := getString(args, "slot")
	if slot == "" {
		return nil, api.NewRequiredFieldError("slot")
	}

	certData, err := decodeBase64Arg(args, "certificate")
	if err != nil {
		return nil, err
	}
	if certData == nil {
		return nil, api.NewRequiredFieldError("certificate")
	}

	format := getString(args, "format")
	if format == "" {
		return nil, api.NewRequiredFieldError("format")
	}

	if err := xkmssdk.ServiceStorePIVCertificate(ctx, &transport.StorePIVCertificateRequest{
		Backend:     backend,
		Slot:        slot,
		Certificate: certData,
		Format:      format,
	}); err != nil {
		return nil, &XKMSOperationError{Operation: "piv.storeCert", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"success": true,
	})
}

// handleXKMSPIVDeleteCert deletes a certificate from a PIV slot.
func (r *ToolRegistry) handleXKMSPIVDeleteCert(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	backend := getString(args, "backend")
	if backend == "" {
		return nil, api.NewRequiredFieldError("backend")
	}

	slot := getString(args, "slot")
	if slot == "" {
		return nil, api.NewRequiredFieldError("slot")
	}

	if err := xkmssdk.ServiceDeletePIVCertificate(ctx, &transport.DeletePIVCertificateRequest{
		Backend: backend,
		Slot:    slot,
	}); err != nil {
		return nil, &XKMSOperationError{Operation: "piv.deleteCert", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"success": true,
	})
}

// handleXKMSPIVGenerateKey generates a key pair in a PIV slot.
func (r *ToolRegistry) handleXKMSPIVGenerateKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	backend := getString(args, "backend")
	if backend == "" {
		return nil, api.NewRequiredFieldError("backend")
	}

	slot := getString(args, "slot")
	if slot == "" {
		return nil, api.NewRequiredFieldError("slot")
	}

	resp, err := xkmssdk.ServiceGeneratePIVKey(ctx, &transport.GeneratePIVKeyRequest{
		Backend:   backend,
		Slot:      slot,
		Algorithm: getString(args, "algorithm"),
		Subject:   getString(args, "subject"),
	})
	if err != nil {
		return nil, &XKMSOperationError{Operation: "piv.generateKey", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"slot":        resp.Slot,
		"certificate": base64.StdEncoding.EncodeToString(resp.Certificate),
		"public_key":  base64.StdEncoding.EncodeToString(resp.PublicKey),
	})
}

// handleXKMSPIVGenerateCSR generates a CSR for a PIV slot key.
func (r *ToolRegistry) handleXKMSPIVGenerateCSR(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	backend := getString(args, "backend")
	if backend == "" {
		return nil, api.NewRequiredFieldError("backend")
	}

	slot := getString(args, "slot")
	if slot == "" {
		return nil, api.NewRequiredFieldError("slot")
	}

	resp, err := xkmssdk.ServiceGeneratePIVCSR(ctx, &transport.GeneratePIVCSRRequest{
		Backend: backend,
		Slot:    slot,
		Subject: getString(args, "subject"),
	})
	if err != nil {
		return nil, &XKMSOperationError{Operation: "piv.generateCSR", Cause: err}
	}

	return marshalResult(map[string]interface{}{
		"slot": resp.Slot,
		"csr":  base64.StdEncoding.EncodeToString(resp.CSR),
	})
}

// =============================================================================
// FIDO2 Tool Handlers
// =============================================================================

// handleXKMSFIDO2BeginRegistration begins a WebAuthn registration ceremony.
func (r *ToolRegistry) handleXKMSFIDO2BeginRegistration(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	username := getString(args, "username")
	if username == "" {
		return nil, api.NewRequiredFieldError("username")
	}

	// FIDO2 operations are not yet implemented as top-level xkms functions.
	// Return a structured error indicating the operation is not available.
	return nil, &XKMSOperationError{Operation: "fido2.beginRegistration", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleXKMSFIDO2FinishRegistration completes a WebAuthn registration ceremony.
func (r *ToolRegistry) handleXKMSFIDO2FinishRegistration(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	username := getString(args, "username")
	if username == "" {
		return nil, api.NewRequiredFieldError("username")
	}

	credentialID := getString(args, "credential_id")
	if credentialID == "" {
		return nil, api.NewRequiredFieldError("credential_id")
	}

	publicKey := getString(args, "public_key")
	if publicKey == "" {
		return nil, api.NewRequiredFieldError("public_key")
	}

	attestationData := getString(args, "attestation_data")
	if attestationData == "" {
		return nil, api.NewRequiredFieldError("attestation_data")
	}

	clientDataJSON := getString(args, "client_data_json")
	if clientDataJSON == "" {
		return nil, api.NewRequiredFieldError("client_data_json")
	}

	return nil, &XKMSOperationError{Operation: "fido2.finishRegistration", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleXKMSFIDO2BeginAuthentication begins a WebAuthn authentication ceremony.
func (r *ToolRegistry) handleXKMSFIDO2BeginAuthentication(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	username := getString(args, "username")
	if username == "" {
		return nil, api.NewRequiredFieldError("username")
	}

	return nil, &XKMSOperationError{Operation: "fido2.beginAuthentication", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleXKMSFIDO2FinishAuthentication completes a WebAuthn authentication ceremony.
func (r *ToolRegistry) handleXKMSFIDO2FinishAuthentication(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	username := getString(args, "username")
	if username == "" {
		return nil, api.NewRequiredFieldError("username")
	}

	credentialID := getString(args, "credential_id")
	if credentialID == "" {
		return nil, api.NewRequiredFieldError("credential_id")
	}

	sig := getString(args, "signature")
	if sig == "" {
		return nil, api.NewRequiredFieldError("signature")
	}

	authData := getString(args, "authenticator_data")
	if authData == "" {
		return nil, api.NewRequiredFieldError("authenticator_data")
	}

	clientDataJSON := getString(args, "client_data_json")
	if clientDataJSON == "" {
		return nil, api.NewRequiredFieldError("client_data_json")
	}

	return nil, &XKMSOperationError{Operation: "fido2.finishAuthentication", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// =============================================================================
// CA Tool Handlers
// =============================================================================

// handleTrustedCAGetBundle retrieves the CA certificate bundle.
func (r *ToolRegistry) handleTrustedCAGetBundle(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	// CA operations are not yet implemented as top-level xkms functions.
	return nil, &XKMSOperationError{Operation: "ca.getBundle", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleTrustedCAGetCert retrieves the CA certificate.
func (r *ToolRegistry) handleTrustedCAGetCert(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	return nil, &XKMSOperationError{Operation: "ca.getCert", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleTrustedCASignCSR signs a certificate signing request.
func (r *ToolRegistry) handleTrustedCASignCSR(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	csrPEM := getString(args, "csr_pem")
	if csrPEM == "" {
		return nil, api.NewRequiredFieldError("csr_pem")
	}

	return nil, &XKMSOperationError{Operation: "ca.signCSR", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleTrustedCAIssueCert issues a new certificate.
func (r *ToolRegistry) handleTrustedCAIssueCert(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	profile := getString(args, "profile")
	if profile == "" {
		return nil, api.NewRequiredFieldError("profile")
	}

	commonName := getString(args, "common_name")
	if commonName == "" {
		return nil, api.NewRequiredFieldError("common_name")
	}

	return nil, &XKMSOperationError{Operation: "ca.issueCert", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleTrustedCARevokeCert revokes a certificate by serial number.
func (r *ToolRegistry) handleTrustedCARevokeCert(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	serialNumber := getString(args, "serial_number")
	if serialNumber == "" {
		return nil, api.NewRequiredFieldError("serial_number")
	}

	return nil, &XKMSOperationError{Operation: "ca.revokeCert", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleTrustedCAGenerateCRL generates a certificate revocation list.
func (r *ToolRegistry) handleTrustedCAGenerateCRL(_ context.Context, _ map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	return nil, &XKMSOperationError{Operation: "ca.generateCRL", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleTrustedCAIsRevoked checks if a certificate is revoked.
func (r *ToolRegistry) handleTrustedCAIsRevoked(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	serialNumber := getString(args, "serial_number")
	if serialNumber == "" {
		return nil, api.NewRequiredFieldError("serial_number")
	}

	return nil, &XKMSOperationError{Operation: "ca.isRevoked", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// =============================================================================
// PIN Tool Handlers
// =============================================================================

// handleXKMSPINSetSO sets the Security Officer PIN.
func (r *ToolRegistry) handleXKMSPINSetSO(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	newSOPIN := getString(args, "new_so_pin")
	if newSOPIN == "" {
		return nil, api.NewRequiredFieldError("new_so_pin")
	}

	return nil, &XKMSOperationError{Operation: "pin.setSO", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleXKMSPINSetUser sets the user PIN using the SO PIN.
func (r *ToolRegistry) handleXKMSPINSetUser(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	soPIN := getString(args, "so_pin")
	if soPIN == "" {
		return nil, api.NewRequiredFieldError("so_pin")
	}

	newUserPIN := getString(args, "new_user_pin")
	if newUserPIN == "" {
		return nil, api.NewRequiredFieldError("new_user_pin")
	}

	return nil, &XKMSOperationError{Operation: "pin.setUser", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleXKMSPINChangeSO changes the Security Officer PIN.
func (r *ToolRegistry) handleXKMSPINChangeSO(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	currentSOPIN := getString(args, "current_so_pin")
	if currentSOPIN == "" {
		return nil, api.NewRequiredFieldError("current_so_pin")
	}

	newSOPIN := getString(args, "new_so_pin")
	if newSOPIN == "" {
		return nil, api.NewRequiredFieldError("new_so_pin")
	}

	return nil, &XKMSOperationError{Operation: "pin.changeSO", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleXKMSPINChangeUser changes the user PIN.
func (r *ToolRegistry) handleXKMSPINChangeUser(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	currentUserPIN := getString(args, "current_user_pin")
	if currentUserPIN == "" {
		return nil, api.NewRequiredFieldError("current_user_pin")
	}

	newUserPIN := getString(args, "new_user_pin")
	if newUserPIN == "" {
		return nil, api.NewRequiredFieldError("new_user_pin")
	}

	return nil, &XKMSOperationError{Operation: "pin.changeUser", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleXKMSPINVerifySO verifies the Security Officer PIN.
func (r *ToolRegistry) handleXKMSPINVerifySO(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	soPIN := getString(args, "so_pin")
	if soPIN == "" {
		return nil, api.NewRequiredFieldError("so_pin")
	}

	return nil, &XKMSOperationError{Operation: "pin.verifySO", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleXKMSPINVerifyUser verifies the user PIN.
func (r *ToolRegistry) handleXKMSPINVerifyUser(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	userPIN := getString(args, "user_pin")
	if userPIN == "" {
		return nil, api.NewRequiredFieldError("user_pin")
	}

	return nil, &XKMSOperationError{Operation: "pin.verifyUser", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleXKMSPINLockoutStatus gets the current PIN lockout status.
func (r *ToolRegistry) handleXKMSPINLockoutStatus(_ context.Context, _ map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	return nil, &XKMSOperationError{Operation: "pin.lockoutStatus", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// handleXKMSPINResetLockout resets PIN lockout using the SO PIN.
func (r *ToolRegistry) handleXKMSPINResetLockout(_ context.Context, args map[string]interface{}) (interface{}, error) {
	if err := ensureXKMSInitialized(); err != nil {
		return nil, err
	}

	soPIN := getString(args, "so_pin")
	if soPIN == "" {
		return nil, api.NewRequiredFieldError("so_pin")
	}

	return nil, &XKMSOperationError{Operation: "pin.resetLockout", Cause: xkmssdk.ServiceErrOperationNotSupported}
}

// =============================================================================
// Helper Functions
// =============================================================================

// extractPublicKeyPEM extracts a PEM-encoded public key from a private key.
func extractPublicKeyPEM(privKey crypto.PrivateKey) string {
	if privKey == nil {
		return ""
	}

	signer, ok := privKey.(crypto.Signer)
	if !ok {
		return ""
	}

	pubKeyBytes, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return ""
	}

	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	}))
}

// =============================================================================
// XKMS Error Types
// =============================================================================

// XKMSNotInitializedError indicates the xkms service is not initialized.
type XKMSNotInitializedError struct{}

// Error implements the error interface.
func (e *XKMSNotInitializedError) Error() string {
	return "xkms service not initialized"
}

// XKMSOperationError indicates an xkms operation failed.
type XKMSOperationError struct {
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *XKMSOperationError) Error() string {
	if e.Cause != nil {
		return "xkms " + e.Operation + " failed: " + e.Cause.Error()
	}
	return "xkms " + e.Operation + " failed"
}

// Unwrap returns the underlying cause.
func (e *XKMSOperationError) Unwrap() error {
	return e.Cause
}

// XKMSInvalidCurveError indicates an invalid elliptic curve was specified.
type XKMSInvalidCurveError struct {
	Curve string
}

// Error implements the error interface.
func (e *XKMSInvalidCurveError) Error() string {
	return "invalid elliptic curve: " + e.Curve
}

// XKMSInvalidPEMError indicates invalid PEM data was provided.
type XKMSInvalidPEMError struct {
	Field string
}

// Error implements the error interface.
func (e *XKMSInvalidPEMError) Error() string {
	return "invalid PEM data in field: " + e.Field
}

// XKMSCertificateParseError indicates a certificate parsing failure.
type XKMSCertificateParseError struct {
	Cause error
}

// Error implements the error interface.
func (e *XKMSCertificateParseError) Error() string {
	if e.Cause != nil {
		return "failed to parse certificate: " + e.Cause.Error()
	}
	return "failed to parse certificate"
}

// Unwrap returns the underlying cause.
func (e *XKMSCertificateParseError) Unwrap() error {
	return e.Cause
}

// Base64DecodeError indicates a base64 decoding failure.
type Base64DecodeError struct {
	Field string
	Cause error
}

// Error implements the error interface.
func (e *Base64DecodeError) Error() string {
	return "failed to decode base64 field " + e.Field + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *Base64DecodeError) Unwrap() error {
	return e.Cause
}
