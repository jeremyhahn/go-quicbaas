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
	"encoding/base64"
	"encoding/json"

	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isMCPSSHService reports whether service names the SSH service.
func isMCPSSHService(service string) bool {
	return trimServiceName(service) == "SSH"
}

// sshMethodTools maps SDK SSH methods to the MCP tool names the server
// registers (pkg/api/mcp/ssh_tools.go). The tools are named after the
// resource and operation (ssh_key_generate), not the RPC (GenerateKey), so
// the transport's generic service_method derivation would ask for tools that
// do not exist.
var sshMethodTools = map[string]string{
	sshclient.MethodGenerateKey:         "quicbaas_ssh_key_generate",
	sshclient.MethodImportKey:           "quicbaas_ssh_key_import",
	sshclient.MethodGetKey:              "quicbaas_ssh_key_get",
	sshclient.MethodListKeys:            "quicbaas_ssh_key_list",
	sshclient.MethodDeleteKey:           "quicbaas_ssh_key_delete",
	sshclient.MethodExportPublicKey:     "quicbaas_ssh_key_export",
	sshclient.MethodSignDigest:          "quicbaas_ssh_key_sign",
	sshclient.MethodAddKnownHost:        "quicbaas_ssh_known_host_add",
	sshclient.MethodListKnownHosts:      "quicbaas_ssh_known_host_list",
	sshclient.MethodRemoveKnownHost:     "quicbaas_ssh_known_host_remove",
	sshclient.MethodAddAuthorizedKey:    "quicbaas_ssh_authorized_add",
	sshclient.MethodListAuthorizedKeys:  "quicbaas_ssh_authorized_list",
	sshclient.MethodRemoveAuthorizedKey: "quicbaas_ssh_authorized_remove",
	sshclient.MethodEmitAuthorizedKey:   "quicbaas_ssh_authorized_emit",
}

// sshToolName returns the MCP tool name for an SDK SSH method.
func sshToolName(method string) (string, bool) {
	name, ok := sshMethodTools[method]
	return name, ok
}

// newSSHUnsupportedError reports an SSH method the MCP surface does not
// register. The SSH tool set is closed, so this is a typed failure naming the
// transport rather than a request for a tool the server does not have.
func newSSHUnsupportedError(method string) error {
	return errors.New(errors.CodeUnimplemented, "mcp.ssh."+method,
		"ssh method is not supported by the MCP transport")
}

// sshMCPHandler transforms SDK SSH requests into MCP tool arguments and tool
// results back into the SDK wire types.
type sshMCPHandler struct{}

// newSSHMCPHandler creates a new SSH MCP handler.
func newSSHMCPHandler() *sshMCPHandler {
	return &sshMCPHandler{}
}

// transformRequest converts an SDK SSH request into MCP tool arguments.
//
// Most SSH request types already carry the snake_case JSON tags the tools
// expect, so the generic marshal is exact. The two binary-bearing verbs are
// the exception: JSON has no byte type, so the tools take base64 under
// explicitly _b64-suffixed argument names.
func (h *sshMCPHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	switch typed := req.(type) {
	case *sshclient.ImportKeyRequest:
		return map[string]interface{}{
			"scope":          typed.Scope,
			"owner":          typed.Owner,
			"name":           typed.Name,
			"comment":        typed.Comment,
			"pem_b64":        base64.StdEncoding.EncodeToString(typed.PEM),
			"passphrase_b64": typed.PassphraseB64,
		}, nil
	case *sshclient.SignRequest:
		return map[string]interface{}{
			"scope":      typed.Scope,
			"owner":      typed.Owner,
			"name":       typed.Name,
			"digest_b64": base64.StdEncoding.EncodeToString(typed.Digest),
		}, nil
	}
	return sshArgumentsFromRequest(method, req)
}

// sshArgumentsFromRequest marshals an SDK request struct into tool arguments
// via its JSON tags.
func sshArgumentsFromRequest(method string, req interface{}) (map[string]interface{}, error) {
	if req == nil {
		return map[string]interface{}{}, nil
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "mcp.ssh."+method,
			"failed to encode request")
	}
	args := map[string]interface{}{}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "mcp.ssh."+method,
			"failed to convert request to tool arguments")
	}
	return args, nil
}

// sshMCPTransform decodes one MCP tool result into its SDK type.
type sshMCPTransform func(raw []byte) (interface{}, error)

// sshMCPTransforms gives O(1) dispatch from method to result decoder. The
// tools return the sdk/go/client/ssh wire envelopes verbatim, so each decoder
// is a plain unmarshal into the matching SDK type.
var sshMCPTransforms = map[string]sshMCPTransform{
	sshclient.MethodGenerateKey:         decodeMCPSSHKey,
	sshclient.MethodImportKey:           decodeMCPSSHKey,
	sshclient.MethodGetKey:              decodeMCPSSHKey,
	sshclient.MethodListKeys:            decodeMCPSSHKeyList,
	sshclient.MethodDeleteKey:           decodeMCPSSHAck,
	sshclient.MethodExportPublicKey:     decodeMCPSSHPublicKey,
	sshclient.MethodSignDigest:          decodeMCPSSHSignature,
	sshclient.MethodAddKnownHost:        decodeMCPSSHAck,
	sshclient.MethodListKnownHosts:      decodeMCPSSHKnownHostList,
	sshclient.MethodRemoveKnownHost:     decodeMCPSSHAck,
	sshclient.MethodAddAuthorizedKey:    decodeMCPSSHAck,
	sshclient.MethodListAuthorizedKeys:  decodeMCPSSHAuthorizedList,
	sshclient.MethodRemoveAuthorizedKey: decodeMCPSSHAck,
	sshclient.MethodEmitAuthorizedKey:   decodeMCPSSHEmit,
}

// transformResponse converts a raw MCP tool result into the SDK type.
func (h *sshMCPHandler) transformResponse(method string, result interface{}) (interface{}, error) {
	transform, ok := sshMCPTransforms[method]
	if !ok {
		return nil, newSSHUnsupportedError(method)
	}
	if result == nil {
		return nil, errors.NewInternal("mcp.ssh."+method, "empty tool result")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ssh."+method,
			"failed to re-encode tool result")
	}
	return transform(raw)
}

func decodeMCPSSHKey(raw []byte) (interface{}, error) {
	var out sshclient.Key
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ssh", "failed to decode key")
	}
	return &out, nil
}

func decodeMCPSSHKeyList(raw []byte) (interface{}, error) {
	var out sshclient.ListKeysResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ssh", "failed to decode key list")
	}
	if out.Items == nil {
		out.Items = []*sshclient.Key{}
	}
	return &out, nil
}

func decodeMCPSSHKnownHostList(raw []byte) (interface{}, error) {
	var out sshclient.ListKnownHostsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ssh", "failed to decode known-host list")
	}
	if out.Items == nil {
		out.Items = []*sshclient.Pin{}
	}
	return &out, nil
}

func decodeMCPSSHAuthorizedList(raw []byte) (interface{}, error) {
	var out sshclient.ListAuthorizedKeysResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ssh", "failed to decode authorized list")
	}
	if out.Items == nil {
		out.Items = []*sshclient.AuthorizedEntry{}
	}
	return &out, nil
}

func decodeMCPSSHPublicKey(raw []byte) (interface{}, error) {
	var out sshclient.PublicKey
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ssh", "failed to decode public key")
	}
	return &out, nil
}

func decodeMCPSSHSignature(raw []byte) (interface{}, error) {
	var out sshclient.SignResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ssh", "failed to decode signature")
	}
	return &out, nil
}

func decodeMCPSSHEmit(raw []byte) (interface{}, error) {
	var out sshclient.EmitResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ssh", "failed to decode emit result")
	}
	return &out, nil
}

func decodeMCPSSHAck(raw []byte) (interface{}, error) {
	var out sshclient.AckResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ssh", "failed to decode acknowledgement")
	}
	return &out, nil
}
