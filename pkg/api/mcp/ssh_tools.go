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

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
)

// =============================================================================
// SSH MCP tools.
//
// The SSH surface composes the go-xkms SSH SDK through service.SSHService,
// the same service the REST, gRPC, QUIC, and embedded transports use.
//
// SECURITY INVARIANT: no tool result carries private key material. Import
// accepts a base64 PEM inbound and Sign accepts a base64 digest inbound;
// neither echoes the key back. Results are the sdk/go/client/ssh wire
// envelopes, so the MCP JSON matches every other transport byte-for-byte.
// =============================================================================

// SSHInvalidBase64Error reports a tool argument that was not valid base64.
type SSHInvalidBase64Error struct {
	Field string
	Err   error
}

// Error implements the error interface.
func (e *SSHInvalidBase64Error) Error() string {
	return "mcp/ssh: " + e.Field + " is not valid base64: " + e.Err.Error()
}

// Unwrap exposes the underlying decode failure.
func (e *SSHInvalidBase64Error) Unwrap() error { return e.Err }

// registerSSHTools registers the SSH MCP tools.
func (r *ToolRegistry) registerSSHTools() {
	scopeOwner := func(extra map[string]interface{}, required ...string) map[string]interface{} {
		props := map[string]interface{}{
			"scope": map[string]interface{}{"type": "string", "description": "Asset scope: tenant, user, or service"},
			"owner": map[string]interface{}{"type": "string", "description": "Scope-specific owner identifier"},
		}
		for k, v := range extra {
			props[k] = v
		}
		return map[string]interface{}{
			"type":       "object",
			"properties": props,
			"required":   required,
		}
	}
	nameProp := map[string]interface{}{"type": "string", "description": "Key name, unique within (scope, owner)"}

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_key_generate",
		Description: "Generate a new SSH key. Returns public metadata only — the private half stays in the key store.",
		InputSchema: scopeOwner(map[string]interface{}{
			"name":      nameProp,
			"algorithm": map[string]interface{}{"type": "string", "description": "ed25519, rsa, ecdsa-p256, ecdsa-p384, or ecdsa-p521"},
			"rsa_bits":  map[string]interface{}{"type": "integer", "description": "RSA key size; defaults to 4096 for rsa"},
			"comment":   map[string]interface{}{"type": "string", "description": "Free-form key comment"},
			"backend":   map[string]interface{}{"type": "string", "description": "Optional crypto backend: software, tpm2, pkcs11, cloud"},
		}, "scope", "owner", "name"),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_key_import",
		Description: "Import a PEM-encoded SSH private key. The PEM and its passphrase are write-only and are never returned.",
		InputSchema: scopeOwner(map[string]interface{}{
			"name":           nameProp,
			"comment":        map[string]interface{}{"type": "string", "description": "Free-form key comment"},
			"pem_b64":        map[string]interface{}{"type": "string", "description": "Base64-encoded PEM private key"},
			"passphrase_b64": map[string]interface{}{"type": "string", "description": "Base64-encoded PEM passphrase; omit for an unencrypted PEM"},
		}, "scope", "owner", "name", "pem_b64"),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_key_get",
		Description: "Retrieve metadata for one SSH key.",
		InputSchema: scopeOwner(map[string]interface{}{"name": nameProp}, "scope", "owner", "name"),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_key_list",
		Description: "List SSH keys for a (scope, owner) pair.",
		InputSchema: scopeOwner(nil),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_key_delete",
		Description: "Delete an SSH key and destroy its enclave.",
		InputSchema: scopeOwner(map[string]interface{}{"name": nameProp}, "scope", "owner", "name"),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_key_export",
		Description: "Export the authorized_keys-format public half of an SSH key.",
		InputSchema: scopeOwner(map[string]interface{}{"name": nameProp}, "scope", "owner", "name"),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_key_sign",
		Description: "Sign a digest with a stored SSH key. The private half never leaves the key store.",
		InputSchema: scopeOwner(map[string]interface{}{
			"name":       nameProp,
			"digest_b64": map[string]interface{}{"type": "string", "description": "Base64-encoded digest to sign"},
		}, "scope", "owner", "name", "digest_b64"),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_known_host_add",
		Description: "Pin an SSH host-key fingerprint.",
		InputSchema: scopeOwner(map[string]interface{}{
			"host":        map[string]interface{}{"type": "string", "description": "Target host"},
			"fingerprint": map[string]interface{}{"type": "string", "description": "OpenSSH SHA256 fingerprint"},
			"algorithm":   map[string]interface{}{"type": "string", "description": "Host-key algorithm, e.g. ssh-ed25519"},
			"source":      map[string]interface{}{"type": "string", "description": "Provenance tag, e.g. manual or cert"},
		}, "scope", "owner", "host", "fingerprint"),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_known_host_list",
		Description: "List pinned SSH host-key fingerprints.",
		InputSchema: scopeOwner(nil),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_known_host_remove",
		Description: "Remove a pinned SSH host-key fingerprint.",
		InputSchema: scopeOwner(map[string]interface{}{
			"host": map[string]interface{}{"type": "string", "description": "Target host"},
		}, "scope", "owner", "host"),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_authorized_add",
		Description: "Add an authorized_keys entry.",
		InputSchema: scopeOwner(map[string]interface{}{
			"name":    map[string]interface{}{"type": "string", "description": "Entry name, unique within (scope, owner)"},
			"line":    map[string]interface{}{"type": "string", "description": "authorized_keys line"},
			"options": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "authorized_keys options"},
			"comment": map[string]interface{}{"type": "string", "description": "Free-form comment"},
		}, "scope", "owner", "name", "line"),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_authorized_list",
		Description: "List authorized_keys entries.",
		InputSchema: scopeOwner(nil),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_authorized_remove",
		Description: "Remove an authorized_keys entry by name.",
		InputSchema: scopeOwner(map[string]interface{}{
			"name": map[string]interface{}{"type": "string", "description": "Entry name"},
		}, "scope", "owner", "name"),
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_ssh_authorized_emit",
		Description: "Format a public key as an authorized_keys line. Pure formatting — nothing is stored.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"public_key": map[string]interface{}{"type": "string", "description": "authorized_keys-format public key line"},
				"options":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "authorized_keys options"},
				"comment":    map[string]interface{}{"type": "string", "description": "Free-form comment"},
			},
			"required": []string{"public_key"},
		},
	})
}

// =============================================================================
// Dispatch
// =============================================================================

// sshToolHandlers is the O(1) dispatch table for the SSH tools.
var sshToolHandlers = map[string]func(*ToolRegistry, context.Context, map[string]interface{}) (interface{}, error){
	"quicbaas_ssh_key_generate":      (*ToolRegistry).executeSSHKeyGenerate,
	"quicbaas_ssh_key_import":        (*ToolRegistry).executeSSHKeyImport,
	"quicbaas_ssh_key_get":           (*ToolRegistry).executeSSHKeyGet,
	"quicbaas_ssh_key_list":          (*ToolRegistry).executeSSHKeyList,
	"quicbaas_ssh_key_delete":        (*ToolRegistry).executeSSHKeyDelete,
	"quicbaas_ssh_key_export":        (*ToolRegistry).executeSSHKeyExport,
	"quicbaas_ssh_key_sign":          (*ToolRegistry).executeSSHKeySign,
	"quicbaas_ssh_known_host_add":    (*ToolRegistry).executeSSHKnownHostAdd,
	"quicbaas_ssh_known_host_list":   (*ToolRegistry).executeSSHKnownHostList,
	"quicbaas_ssh_known_host_remove": (*ToolRegistry).executeSSHKnownHostRemove,
	"quicbaas_ssh_authorized_add":    (*ToolRegistry).executeSSHAuthorizedAdd,
	"quicbaas_ssh_authorized_list":   (*ToolRegistry).executeSSHAuthorizedList,
	"quicbaas_ssh_authorized_remove": (*ToolRegistry).executeSSHAuthorizedRemove,
	"quicbaas_ssh_authorized_emit":   (*ToolRegistry).executeSSHAuthorizedEmit,
}

// isSSHTool reports whether name is an SSH tool.
func isSSHTool(name string) bool {
	_, ok := sshToolHandlers[name]
	return ok
}

// executeSSHTool dispatches an SSH tool call to its handler.
func (r *ToolRegistry) executeSSHTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	handler, ok := sshToolHandlers[name]
	if !ok {
		return nil, api.NewUnknownToolError(name)
	}
	return handler(r, ctx, args)
}

// sshService builds the SSH service from the registry's composed service.
func (r *ToolRegistry) sshService() service.SSHService {
	if r.service == nil {
		return service.NewSSHService(nil)
	}
	return service.NewSSHService(r.service.SSHClient())
}

// =============================================================================
// Executors — keys
// =============================================================================

func (r *ToolRegistry) executeSSHKeyGenerate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return r.sshService().GenerateKey(ctx, xkmsssh.GenerateRequest{
		Scope:     getString(args, "scope"),
		Owner:     getString(args, "owner"),
		Name:      getString(args, "name"),
		Algorithm: getString(args, "algorithm"),
		RSABits:   getInt(args, "rsa_bits"),
		Comment:   getString(args, "comment"),
		Backend:   getString(args, "backend"),
	})
}

func (r *ToolRegistry) executeSSHKeyImport(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	pem, err := sshDecodeBase64Arg(args, "pem_b64")
	if err != nil {
		return nil, err
	}
	return r.sshService().ImportKey(ctx, service.SSHImportKeyRequest{
		Scope:         getString(args, "scope"),
		Owner:         getString(args, "owner"),
		Name:          getString(args, "name"),
		Comment:       getString(args, "comment"),
		PEM:           pem,
		PassphraseB64: getString(args, "passphrase_b64"),
	})
}

func (r *ToolRegistry) executeSSHKeyGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return r.sshService().GetKey(ctx, xkmsssh.GetKeyRequest{
		Scope: getString(args, "scope"),
		Owner: getString(args, "owner"),
		Name:  getString(args, "name"),
	})
}

func (r *ToolRegistry) executeSSHKeyList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	items, err := r.sshService().ListKeys(ctx, xkmsssh.ListKeysRequest{
		Scope: getString(args, "scope"),
		Owner: getString(args, "owner"),
	})
	if err != nil {
		return nil, err
	}
	return &sshclient.ListKeysResponse{Items: items}, nil
}

func (r *ToolRegistry) executeSSHKeyDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := r.sshService().DeleteKey(ctx, xkmsssh.DeleteKeyRequest{
		Scope: getString(args, "scope"),
		Owner: getString(args, "owner"),
		Name:  getString(args, "name"),
	}); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func (r *ToolRegistry) executeSSHKeyExport(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return r.sshService().ExportPublicKey(ctx, xkmsssh.ExportRequest{
		Scope: getString(args, "scope"),
		Owner: getString(args, "owner"),
		Name:  getString(args, "name"),
	})
}

func (r *ToolRegistry) executeSSHKeySign(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	digest, err := sshDecodeBase64Arg(args, "digest_b64")
	if err != nil {
		return nil, err
	}
	sig, err := r.sshService().SignDigest(ctx, service.SSHSignRequest{
		Scope:  getString(args, "scope"),
		Owner:  getString(args, "owner"),
		Name:   getString(args, "name"),
		Digest: digest,
	})
	if err != nil {
		return nil, err
	}
	return &sshclient.SignResponse{Format: sig.Format, Blob: sig.Blob, Rest: sig.Rest}, nil
}

// =============================================================================
// Executors — known hosts
// =============================================================================

func (r *ToolRegistry) executeSSHKnownHostAdd(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := r.sshService().AddKnownHost(ctx, xkmsssh.AddKnownHostRequest{
		Scope:       getString(args, "scope"),
		Owner:       getString(args, "owner"),
		Host:        getString(args, "host"),
		Fingerprint: getString(args, "fingerprint"),
		Algorithm:   getString(args, "algorithm"),
		Source:      getString(args, "source"),
	}); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func (r *ToolRegistry) executeSSHKnownHostList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	items, err := r.sshService().ListKnownHosts(ctx, xkmsssh.ListKnownHostsRequest{
		Scope: getString(args, "scope"),
		Owner: getString(args, "owner"),
	})
	if err != nil {
		return nil, err
	}
	return &sshclient.ListKnownHostsResponse{Items: items}, nil
}

func (r *ToolRegistry) executeSSHKnownHostRemove(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := r.sshService().RemoveKnownHost(ctx, xkmsssh.RemoveKnownHostRequest{
		Scope: getString(args, "scope"),
		Owner: getString(args, "owner"),
		Host:  getString(args, "host"),
	}); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

// =============================================================================
// Executors — authorized keys
// =============================================================================

func (r *ToolRegistry) executeSSHAuthorizedAdd(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := r.sshService().AddAuthorizedKey(ctx, xkmsssh.AddAuthorizedKeyRequest{
		Scope:   getString(args, "scope"),
		Owner:   getString(args, "owner"),
		Name:    getString(args, "name"),
		Line:    getString(args, "line"),
		Options: getStringSlice(args, "options"),
		Comment: getString(args, "comment"),
	}); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func (r *ToolRegistry) executeSSHAuthorizedList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	items, err := r.sshService().ListAuthorizedKeys(ctx, xkmsssh.ListAuthorizedKeysRequest{
		Scope: getString(args, "scope"),
		Owner: getString(args, "owner"),
	})
	if err != nil {
		return nil, err
	}
	return &sshclient.ListAuthorizedKeysResponse{Items: items}, nil
}

func (r *ToolRegistry) executeSSHAuthorizedRemove(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := r.sshService().RemoveAuthorizedKey(ctx, service.SSHAuthorizedKeyRef{
		Scope: getString(args, "scope"),
		Owner: getString(args, "owner"),
		Name:  getString(args, "name"),
	}); err != nil {
		return nil, err
	}
	return &sshclient.AckResponse{Success: true}, nil
}

func (r *ToolRegistry) executeSSHAuthorizedEmit(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	line, err := r.sshService().EmitAuthorizedKey(ctx, xkmsssh.EmitRequest{
		PublicKey: getString(args, "public_key"),
		Options:   getStringSlice(args, "options"),
		Comment:   getString(args, "comment"),
	})
	if err != nil {
		return nil, err
	}
	return &sshclient.EmitResponse{Line: line}, nil
}

// =============================================================================
// Helpers
// =============================================================================

// sshDecodeBase64Arg decodes a base64 tool argument into raw bytes. MCP
// arguments are JSON, which has no byte type, so binary inputs travel
// base64-encoded.
func sshDecodeBase64Arg(args map[string]interface{}, key string) ([]byte, error) {
	encoded := getString(args, key)
	if encoded == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, &SSHInvalidBase64Error{Field: key, Err: err}
	}
	return raw, nil
}
