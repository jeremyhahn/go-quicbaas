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

// kms_handler.go adapts the KMS client's key-version, key-status, metadata,
// algorithm and backend verbs to the quicbaas_kms_* MCP tools that serve them,
// and decodes their results into the client/kms types the SDK client
// type-asserts on. The tools speak the service layer's string enums
// ("enabled", "ED25519", "sign"); the client speaks integer enums. The other
// KMS verbs keep the generic path.

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// isMCPKMSService reports whether the service name targets KMS.
func isMCPKMSService(name string) bool {
	return trimServiceName(name) == "KMS"
}

// kmsVerbs are the KMS methods whose tool result must decode into a client
// type. Each tool name matches mapToToolName.
var kmsVerbs = map[string]toolVerb{
	"ListKeyVersions":        {tool: "quicbaas_kms_key_versions", args: kmsKeyIDArgs("mcp.kms.ListKeyVersions"), decode: decodeKMSListKeyVersions},
	"UpdateKeyStatus":        {tool: "quicbaas_kms_key_status_update", args: kmsUpdateKeyStatusArgs, decode: decodeKMSUpdateKeyStatus},
	"GetKeyMetadata":         {tool: "quicbaas_kms_get_metadata", args: kmsKeyIDArgs("mcp.kms.GetKeyMetadata"), decode: decodeKMSKeyMetadata},
	"GetSupportedAlgorithms": {tool: "quicbaas_kms_get_supported_algorithms", args: kmsNoArgs, decode: decodeKMSSupportedAlgorithms},
	"BackendStatus":          {tool: "quicbaas_kms_backend_status", args: kmsNoArgs, decode: decodeKMSBackendStatus},
	"GetKey":                 {tool: "quicbaas_kms_get_key", args: kmsKeyIDArgs("mcp.kms.GetKey"), decode: decodeKMSKeyInfo},
	"ListKeys":               {tool: "quicbaas_kms_list_keys", args: kmsNoArgs, decode: decodeKMSListKeys},
	"ImportKey":              {tool: "quicbaas_kms_import_key", args: kmsImportKeyArgs, check: kmsResultKeyID, decode: decodeKMSImportKey},
	"RevokeKey":              {tool: "quicbaas_kms_revoke_key", args: kmsRevokeKeyArgs, check: kmsResultKeyID, decode: decodeKMSRevokeKey},
	"UpdateKeyMetadata":      {tool: "quicbaas_kms_update_metadata", args: kmsUpdateKeyMetadataArgs, check: kmsMetadataKeyID, decode: decodeKMSKeyMetadata},
	"GenerateKey": {
		tool: "quicbaas_kms_generate_key", args: kmsGenerateKeyArgs,
		check: requireResultFields("key_id"), decode: decodeKMSGenerateKey,
	},
	"RotateKey": {
		tool: "quicbaas_kms_rotate_key", args: kmsKeyIDArgs("mcp.kms.RotateKey"),
		check: requireResultFields("key_id", "new_version"), decode: decodeKMSRotateKey,
	},
	"DeleteKey": {
		tool: "quicbaas_kms_delete_key", args: kmsKeyIDArgs("mcp.kms.DeleteKey"),
		check: requireResultFields("key_id"), decode: decodeKMSDeleteKey,
	},
	"SetKeyPolicy":            {tool: "quicbaas_kms_set_policy", args: kmsSetKeyPolicyArgs, decode: decodeKMSSetKeyPolicy},
	"GetKeyPolicy":            {tool: "quicbaas_kms_get_policy", args: kmsKeyIDArgs("mcp.kms.GetKeyPolicy"), decode: decodeKMSGetKeyPolicy},
	"SetKeyRotationPolicy":    {tool: "quicbaas_kms_set_rotation_policy", args: kmsSetRotationPolicyArgs, decode: decodeKMSSetRotationPolicy},
	"GetKeyRotationPolicy":    {tool: "quicbaas_kms_get_rotation_policy", args: kmsKeyIDArgs("mcp.kms.GetKeyRotationPolicy"), decode: decodeKMSGetRotationPolicy},
	"DeleteKeyRotationPolicy": {tool: "quicbaas_kms_delete_rotation_policy", args: kmsKeyIDArgs("mcp.kms.DeleteKeyRotationPolicy"), decode: decodeKMSDeleteRotationPolicy},
	"ExportKey": {
		tool: "quicbaas_kms_export_key", args: kmsExportKeyArgs,
		check: requireResultFields("key_id"), decode: decodeKMSExportKey,
	},
	"UnwrapKey": {
		tool: "quicbaas_kms_unwrap_key", args: kmsUnwrapKeyArgs,
		check: requireResultFields("key_id"), decode: decodeKMSUnwrapKey,
	},
}

// kmsPublicKeyTool serves GetPublicKey. It is outside kmsVerbs because its
// decode can fail on a missing or malformed public key.
const kmsPublicKeyTool = "quicbaas_kms_get_public_key"

// kmsWireStatus is the closed set of statuses the key-status tool accepts.
var kmsWireStatus = map[kmsclient.KeyStatus]string{
	kmsclient.KeyStatusEnabled:  string(service.KeyStatusEnabled),
	kmsclient.KeyStatusDisabled: string(service.KeyStatusDisabled),
}

// kmsStatuses, kmsAlgorithms and kmsUsages map the tools' string enums to the
// client's. An unknown value decodes to the enum's zero value, as the KMS
// client does for service responses.
var kmsStatuses = map[service.KeyStatus]kmsclient.KeyStatus{
	service.KeyStatusEnabled:         kmsclient.KeyStatusEnabled,
	service.KeyStatusDisabled:        kmsclient.KeyStatusDisabled,
	service.KeyStatusPendingDeletion: kmsclient.KeyStatusPendingDeletion,
	service.KeyStatusDeleted:         kmsclient.KeyStatusDeleted,
	service.KeyStatusRotating:        kmsclient.KeyStatusRotating,
	service.KeyStatusCompromised:     kmsclient.KeyStatusCompromised,
}

var kmsAlgorithms = map[service.KeyAlgorithm]kmsclient.KeyAlgorithm{
	service.KeyAlgAES256GCM:        kmsclient.AlgorithmAES256GCM,
	service.KeyAlgChaCha20Poly1305: kmsclient.AlgorithmChaCha20Poly1305,
	service.KeyAlgRSA2048:          kmsclient.AlgorithmRSA2048,
	service.KeyAlgRSA4096:          kmsclient.AlgorithmRSA4096,
	service.KeyAlgECDSAP256:        kmsclient.AlgorithmECDSAP256,
	service.KeyAlgECDSAP384:        kmsclient.AlgorithmECDSAP384,
	service.KeyAlgEd25519:          kmsclient.AlgorithmED25519,
}

// kmsWireWrappingAlgorithms maps the client wrapping algorithms to the names
// the export and unwrap tools accept.
var kmsWireWrappingAlgorithms = map[kmsclient.WrappingAlgorithm]string{
	kmsclient.WrapAlgAESKW:         "AES-KW",
	kmsclient.WrapAlgAESKWP:        "AES-KWP",
	kmsclient.WrapAlgAESGCM:        "AES-GCM",
	kmsclient.WrapAlgRSAOAEPSHA256: "RSA-OAEP-SHA256",
	kmsclient.WrapAlgRSAOAEPSHA384: "RSA-OAEP-SHA384",
	kmsclient.WrapAlgRSAOAEPSHA512: "RSA-OAEP-SHA512",
	kmsclient.WrapAlgECDHESA256KW:  "ECDH-ES+A256KW",
}

// kmsWrappingAlgorithms maps the tools' wrapping algorithm names back to the
// client wrapping algorithms.
var kmsWrappingAlgorithms = invertKMSMap(kmsWireWrappingAlgorithms)

// kmsWireExportFormats maps the client export formats to the names the
// export tool accepts.
var kmsWireExportFormats = map[kmsclient.ExportFormat]string{
	kmsclient.ExportFormatRaw: "raw",
	kmsclient.ExportFormatJWK: "jwk",
	kmsclient.ExportFormatPEM: "pem",
}

// kmsExportFormats maps the export tool's format names back to the client
// export formats.
var kmsExportFormats = invertKMSMap(kmsWireExportFormats)

var kmsUsages = map[service.KeyUsage]kmsclient.KeyUsage{
	service.KeyUsageEncrypt: kmsclient.KeyUsageEncrypt,
	service.KeyUsageDecrypt: kmsclient.KeyUsageDecrypt,
	service.KeyUsageSign:    kmsclient.KeyUsageSign,
	service.KeyUsageVerify:  kmsclient.KeyUsageVerify,
	service.KeyUsageWrapKey: kmsclient.KeyUsageWrap,
}

// kmsWireAlgorithms are the tool algorithm names of the client algorithms.
var kmsWireAlgorithms = invertKMSMap(kmsAlgorithms)

// kmsWireUsages are the tool usage names of the client usages. The tool has
// no unwrap usage: wrap_key grants both directions, as in the client.
var kmsWireUsages = map[kmsclient.KeyUsage]service.KeyUsage{
	kmsclient.KeyUsageEncrypt: service.KeyUsageEncrypt,
	kmsclient.KeyUsageDecrypt: service.KeyUsageDecrypt,
	kmsclient.KeyUsageSign:    service.KeyUsageSign,
	kmsclient.KeyUsageVerify:  service.KeyUsageVerify,
	kmsclient.KeyUsageWrap:    service.KeyUsageWrapKey,
	kmsclient.KeyUsageUnwrap:  service.KeyUsageWrapKey,
}

// invertKMSMap returns the inverse of a one-to-one lookup map.
func invertKMSMap[K, V comparable](m map[K]V) map[V]K {
	inverted := make(map[V]K, len(m))
	for k, v := range m {
		inverted[v] = k
	}
	return inverted
}

func kmsNoArgs(interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// kmsKeyIDRequest reads the key_id of the client's unexported key-scoped
// requests, whose JSON tags name the tool argument.
type kmsKeyIDRequest struct {
	KeyID string `json:"key_id"`
}

// kmsKeyIDArgs returns the argument builder for a verb whose only argument is
// key_id. A request that carries no key_id is refused before any call.
func kmsKeyIDArgs(op string) func(req interface{}) (map[string]interface{}, error) {
	return func(req interface{}) (map[string]interface{}, error) {
		var r kmsKeyIDRequest
		if err := readKMSRequest(req, &r); err != nil || r.KeyID == "" {
			return nil, errors.NewInvalidInput(op, "invalid request type, key_id is required")
		}
		return map[string]interface{}{"key_id": r.KeyID}, nil
	}
}

// readKMSRequest reads a request's fields through its JSON form, which
// also reaches the client's unexported request types.
func readKMSRequest(req, into interface{}) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

// kmsImportKeyArgs sends the key material base64-encoded, as the import
// tool's schema names it.
func kmsImportKeyArgs(req interface{}) (map[string]interface{}, error) {
	const op = "mcp.kms.ImportKey"
	r, ok := req.(*kmsclient.ImportKeyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type for ImportKey")
	}
	if r.Name == "" || len(r.KeyMaterial) == 0 {
		return nil, errors.NewInvalidInput(op, "name and key material are required")
	}
	algorithm, ok := kmsWireAlgorithms[r.Algorithm]
	if !ok {
		return nil, errors.NewInvalidInput(op, "unsupported algorithm "+r.Algorithm.String())
	}
	args := map[string]interface{}{
		"name":         r.Name,
		"key_material": base64.StdEncoding.EncodeToString(r.KeyMaterial),
		"algorithm":    string(algorithm),
	}
	if r.WrappingKeyID != "" {
		args["wrapping_key_id"] = r.WrappingKeyID
	}
	if r.Description != "" {
		args["description"] = r.Description
	}
	if r.Exportable {
		args["exportable"] = true
	}
	if len(r.Usage) > 0 {
		usage := make([]string, 0, len(r.Usage))
		for _, u := range r.Usage {
			wire, ok := kmsWireUsages[u]
			if !ok {
				return nil, errors.NewInvalidInput(op, "unsupported usage "+u.String())
			}
			usage = append(usage, string(wire))
		}
		args["usage"] = usage
	}
	if len(r.Tags) > 0 {
		args["tags"] = r.Tags
	}
	return args, nil
}

// kmsRevokeKeyRequest carries the fields of the client's unexported
// revoke request.
type kmsRevokeKeyRequest struct {
	KeyID  string `json:"key_id"`
	Reason string `json:"reason"`
}

func kmsRevokeKeyArgs(req interface{}) (map[string]interface{}, error) {
	var r kmsRevokeKeyRequest
	if err := readKMSRequest(req, &r); err != nil || r.KeyID == "" {
		return nil, errors.NewInvalidInput("mcp.kms.RevokeKey", "invalid request type, key_id is required")
	}
	args := map[string]interface{}{"key_id": r.KeyID}
	if r.Reason != "" {
		args["reason"] = r.Reason
	}
	return args, nil
}

func kmsUpdateKeyMetadataArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*kmsclient.UpdateKeyMetadataRequest)
	if !ok || r == nil || r.KeyID == "" {
		return nil, errors.NewInvalidInput("mcp.kms.UpdateKeyMetadata",
			"invalid request type, key_id is required")
	}
	args := map[string]interface{}{"key_id": r.KeyID}
	if r.Name != "" {
		args["name"] = r.Name
	}
	if r.Description != "" {
		args["description"] = r.Description
	}
	if len(r.Tags) > 0 {
		args["tags"] = r.Tags
	}
	if len(r.RemoveTags) > 0 {
		args["remove_tags"] = r.RemoveTags
	}
	return args, nil
}

// kmsResultKeyID refuses a tool result that names no key.
func kmsResultKeyID(op string, m map[string]interface{}) error {
	if getResultString(m, "key_id") == "" {
		return errors.New(errors.CodeInternal, op, "tool result has no key_id")
	}
	return nil
}

// kmsMetadataKeyID refuses a metadata result that names no key.
func kmsMetadataKeyID(op string, m map[string]interface{}) error {
	return kmsResultKeyID(op, nestedResultMap(m, "metadata"))
}

func kmsUpdateKeyStatusArgs(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*kmsclient.UpdateKeyStatusRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.kms.UpdateKeyStatus",
			"invalid request type for UpdateKeyStatus")
	}
	status, ok := kmsWireStatus[r.Status]
	if !ok {
		return nil, errors.NewInvalidInput("mcp.kms.UpdateKeyStatus",
			"status must be enabled or disabled, got "+r.Status.String())
	}
	return map[string]interface{}{
		"key_id":  r.KeyID,
		"status":  status,
		"version": r.Version,
	}, nil
}

// kmsGenerateKeyArgs sends the algorithm the tool requires and each optional
// field only when the request sets it.
func kmsGenerateKeyArgs(req interface{}) (map[string]interface{}, error) {
	const op = "mcp.kms.GenerateKey"
	r, ok := req.(*kmsclient.GenerateKeyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type for GenerateKey")
	}
	algorithm, ok := kmsWireAlgorithms[r.Algorithm]
	if !ok {
		return nil, errors.NewInvalidInput(op, "algorithm "+r.Algorithm.String()+" has no tool name")
	}
	args := map[string]interface{}{"algorithm": string(algorithm)}
	if r.Name != "" {
		args["name"] = r.Name
	}
	if len(r.Usage) > 0 {
		usage := make([]string, 0, len(r.Usage))
		for _, u := range r.Usage {
			wire, ok := kmsWireUsages[u]
			if !ok {
				return nil, errors.NewInvalidInput(op, "usage "+u.String()+" has no tool name")
			}
			usage = append(usage, string(wire))
		}
		args["usage"] = usage
	}
	if r.Exportable {
		args["exportable"] = true
	}
	if r.DeletionProtection {
		args["deletion_protection"] = true
	}
	return args, nil
}

// kmsSetKeyPolicyArgs sends the key ID and the policy document. The allowed
// operations are always sent; every other policy field only when set.
func kmsSetKeyPolicyArgs(req interface{}) (map[string]interface{}, error) {
	const op = "mcp.kms.SetKeyPolicy"
	r, ok := req.(*kmsclient.SetKeyPolicyRequest)
	if !ok || r == nil || r.KeyID == "" || r.Policy == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type, key_id and policy are required")
	}
	p := r.Policy
	operations := make([]string, 0, len(p.AllowedOperations))
	for _, usage := range p.AllowedOperations {
		wire, ok := kmsWireUsages[usage]
		if !ok {
			return nil, errors.NewInvalidInput(op, "unsupported key usage "+usage.String())
		}
		operations = append(operations, string(wire))
	}
	policy := map[string]interface{}{"allowed_operations": operations}
	if p.KeyID != "" {
		policy["key_id"] = p.KeyID
	}
	if p.Version > 0 {
		policy["version"] = p.Version
	}
	if len(p.AllowedPrincipals) > 0 {
		policy["allowed_principals"] = p.AllowedPrincipals
	}
	if len(p.DeniedPrincipals) > 0 {
		policy["denied_principals"] = p.DeniedPrincipals
	}
	if !p.ExpiresAt.IsZero() {
		policy["expires_at"] = p.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if p.RotationRequired {
		policy["rotation_required"] = true
	}
	if p.MaxUsageCount > 0 {
		policy["max_usage_count"] = p.MaxUsageCount
	}
	if p.RequireMFA {
		policy["require_mfa"] = true
	}
	return map[string]interface{}{"key_id": r.KeyID, "policy": policy}, nil
}

// kmsRotationPolicyRequest reads the KMS client's rotation policy request
// from its JSON form. The pointers tell an absent required field from a
// zero one.
type kmsRotationPolicyRequest struct {
	KeyID           string `json:"key_id"`
	AutoRotate      *bool  `json:"auto_rotate"`
	IntervalSeconds *int64 `json:"interval_seconds"`
	RetainVersions  int    `json:"retain_versions"`
	Enabled         bool   `json:"enabled"`
}

// kmsSetRotationPolicyArgs sends the fields the rotation policy tool
// requires, and retain_versions and enabled only when set.
func kmsSetRotationPolicyArgs(req interface{}) (map[string]interface{}, error) {
	var r kmsRotationPolicyRequest
	data, err := json.Marshal(req)
	if err == nil {
		err = json.Unmarshal(data, &r)
	}
	if err != nil || r.KeyID == "" || r.AutoRotate == nil || r.IntervalSeconds == nil {
		return nil, errors.NewInvalidInput("mcp.kms.SetKeyRotationPolicy",
			"invalid request type, key_id, auto_rotate and interval_seconds are required")
	}
	args := map[string]interface{}{
		"key_id":           r.KeyID,
		"auto_rotate":      *r.AutoRotate,
		"interval_seconds": *r.IntervalSeconds,
	}
	if r.RetainVersions > 0 {
		args["retain_versions"] = r.RetainVersions
	}
	if r.Enabled {
		args["enabled"] = true
	}
	return args, nil
}

// kmsExportKeyArgs builds the quicbaas_kms_export_key arguments. The
// wrapping algorithm and format are always sent: their zero values are real
// choices (AES-KW, raw), not "unset", so omitting them would let the
// server's AES-GCM default replace an AES-KW request.
func kmsExportKeyArgs(req interface{}) (map[string]interface{}, error) {
	const op = "mcp.kms.ExportKey"
	r, ok := req.(*kmsclient.ExportKeyRequest)
	if !ok || r == nil || r.KeyID == "" || r.WrappingKeyID == "" {
		return nil, errors.NewInvalidInput(op, "invalid request type, key_id and wrapping_key_id are required")
	}
	if r.Version < 0 {
		return nil, errors.NewInvalidInput(op, "version must not be negative")
	}
	wrapping, ok := kmsWireWrappingAlgorithms[r.WrappingAlgorithm]
	if !ok {
		return nil, errors.NewInvalidInput(op, "unsupported wrapping algorithm")
	}
	format, ok := kmsWireExportFormats[r.Format]
	if !ok {
		return nil, errors.NewInvalidInput(op, "unsupported export format "+r.Format.String())
	}
	args := map[string]interface{}{
		"key_id":             r.KeyID,
		"wrapping_key_id":    r.WrappingKeyID,
		"wrapping_algorithm": wrapping,
		"format":             format,
	}
	if r.Version > 0 {
		args["version"] = r.Version
	}
	return args, nil
}

// kmsUnwrapKeyArgs builds the quicbaas_kms_unwrap_key arguments. The tool has
// no description field, so a request that sets one is refused rather than
// silently losing it.
func kmsUnwrapKeyArgs(req interface{}) (map[string]interface{}, error) {
	const op = "mcp.kms.UnwrapKey"
	r, ok := req.(*kmsclient.UnwrapKeyRequest)
	if !ok || r == nil || r.KeyID == "" || r.WrappingKeyID == "" || len(r.WrappedMaterial) == 0 {
		return nil, errors.NewInvalidInput(op,
			"invalid request type, key_id, wrapping_key_id and wrapped material are required")
	}
	if r.Description != "" {
		return nil, errors.NewInvalidInput(op, "the unwrap tool cannot carry a description")
	}
	if r.SourceVersion < 0 {
		return nil, errors.NewInvalidInput(op, "source version must not be negative")
	}
	algorithm, ok := kmsWireAlgorithms[r.Algorithm]
	if !ok {
		return nil, errors.NewInvalidInput(op, "unsupported key algorithm "+r.Algorithm.String())
	}
	wrapping, ok := kmsWireWrappingAlgorithms[r.WrappingAlgorithm]
	if !ok {
		return nil, errors.NewInvalidInput(op, "unsupported wrapping algorithm")
	}
	args := map[string]interface{}{
		"key_id":             r.KeyID,
		"wrapping_key_id":    r.WrappingKeyID,
		"wrapping_algorithm": wrapping,
		"wrapped_key":        base64.StdEncoding.EncodeToString(r.WrappedMaterial),
		"algorithm":          string(algorithm),
	}
	if r.Name != "" {
		args["name"] = r.Name
	}
	if r.SourceKeyID != "" {
		args["source_key_id"] = r.SourceKeyID
	}
	if r.Exportable {
		args["exportable"] = true
	}
	if r.SourceVersion > 0 {
		args["source_version"] = r.SourceVersion
	}
	return args, nil
}

func decodeKMSListKeyVersions(m map[string]interface{}) interface{} {
	entries := resultMapSlice(m, "versions")
	versions := make([]kmsclient.KeyVersionInfo, 0, len(entries))
	for _, entry := range entries {
		versions = append(versions, kmsclient.KeyVersionInfo{
			Version:   int(getResultInt64(entry, "version")),
			Status:    kmsStatuses[service.KeyStatus(getResultString(entry, "status"))],
			Algorithm: kmsAlgorithms[service.KeyAlgorithm(getResultString(entry, "algorithm"))],
			CreatedAt: getResultTime(entry, "created_at"),
		})
	}
	return &kmsclient.ListKeyVersionsResponse{
		KeyID:    getResultString(m, "key_id"),
		Versions: versions,
	}
}

func decodeKMSUpdateKeyStatus(m map[string]interface{}) interface{} {
	return &kmsclient.UpdateKeyStatusResponse{
		KeyID:   getResultString(m, "key_id"),
		Version: int(getResultInt64(m, "version")),
		Status:  kmsStatuses[service.KeyStatus(getResultString(m, "status"))],
	}
}

func decodeKMSImportKey(m map[string]interface{}) interface{} {
	return &kmsclient.ImportKeyResponse{
		KeyID:     getResultString(m, "key_id"),
		Version:   int(getResultInt64(m, "version")),
		Algorithm: kmsAlgorithms[service.KeyAlgorithm(getResultString(m, "algorithm"))],
	}
}

// decodeKMSRevokeKey returns no value: RevokeKey reports only success, and
// the client accepts a nil response as one.
func decodeKMSRevokeKey(map[string]interface{}) interface{} {
	return nil
}

func decodeKMSKeyMetadata(m map[string]interface{}) interface{} {
	meta := nestedResultMap(m, "metadata")
	var usage []kmsclient.KeyUsage
	for _, u := range decodeStringSlice(meta, "usage") {
		usage = append(usage, kmsUsages[service.KeyUsage(u)])
	}
	return &kmsclient.KeyMetadataResponse{
		KeyID:              getResultString(meta, "key_id"),
		Name:               getResultString(meta, "name"),
		Description:        getResultString(meta, "description"),
		Algorithm:          kmsAlgorithms[service.KeyAlgorithm(getResultString(meta, "algorithm"))],
		Status:             kmsStatuses[service.KeyStatus(getResultString(meta, "status"))],
		Usage:              usage,
		Version:            int(getResultInt64(meta, "version")),
		Exportable:         getResultBool(meta, "exportable"),
		DeletionProtection: getResultBool(meta, "deletion_protection"),
		CreatedAt:          getResultTime(meta, "created_at"),
		UpdatedAt:          getResultTime(meta, "updated_at"),
		LastUsedAt:         getResultTime(meta, "last_used_at"),
		Tags:               getResultStringMap(meta, "tags"),
		Owner:              getResultString(meta, "owner"),
	}
}

func decodeKMSSupportedAlgorithms(m map[string]interface{}) interface{} {
	algorithms := decodeStringSlice(m, "algorithms")
	if algorithms == nil {
		algorithms = []string{}
	}
	return &kmsclient.SupportedAlgorithmsResponse{Algorithms: algorithms}
}

func decodeKMSBackendStatus(m map[string]interface{}) interface{} {
	var capabilities map[string]bool
	if raw, ok := m["capabilities"].(map[string]interface{}); ok {
		capabilities = make(map[string]bool, len(raw))
		for name, v := range raw {
			if enabled, ok := v.(bool); ok {
				capabilities[name] = enabled
			}
		}
	}
	return &kmsclient.BackendStatusResponse{
		BackendType:  getResultString(m, "backend_type"),
		Status:       getResultString(m, "status"),
		Capabilities: capabilities,
	}
}

// kmsKeyInfo maps one flat KMSKeyResponse onto the client KeyInfo. The
// server's updated_at is the client's RotatedAt, as in the client's service
// branch.
func kmsKeyInfo(m map[string]interface{}) kmsclient.KeyInfo {
	var usage []kmsclient.KeyUsage
	for _, u := range decodeStringSlice(m, "usage") {
		usage = append(usage, kmsUsages[service.KeyUsage(u)])
	}
	return kmsclient.KeyInfo{
		KeyID:     getResultString(m, "key_id"),
		Name:      getResultString(m, "name"),
		Algorithm: kmsAlgorithms[service.KeyAlgorithm(getResultString(m, "algorithm"))],
		Version:   int(getResultInt64(m, "version")),
		Usage:     usage,
		CreatedAt: getResultTime(m, "created_at"),
		RotatedAt: getResultTime(m, "updated_at"),
	}
}

func decodeKMSKeyInfo(m map[string]interface{}) interface{} {
	info := kmsKeyInfo(m)
	return &info
}

func decodeKMSListKeys(m map[string]interface{}) interface{} {
	entries := resultMapSlice(m, "keys")
	keys := make([]kmsclient.KeyInfo, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, kmsKeyInfo(entry))
	}
	return &kmsclient.ListKeysResponse{Keys: keys}
}

func decodeKMSGenerateKey(m map[string]interface{}) interface{} {
	return &kmsclient.GenerateKeyResponse{
		KeyID:              getResultString(m, "key_id"),
		Version:            int(getResultInt64(m, "version")),
		Algorithm:          kmsAlgorithms[service.KeyAlgorithm(getResultString(m, "algorithm"))],
		Exportable:         getResultBool(m, "exportable"),
		DeletionProtection: getResultBool(m, "deletion_protection"),
	}
}

func decodeKMSRotateKey(m map[string]interface{}) interface{} {
	return &kmsclient.RotateKeyResponse{
		KeyID:      getResultString(m, "key_id"),
		NewVersion: int(getResultInt64(m, "new_version")),
	}
}

func decodeKMSDeleteKey(m map[string]interface{}) interface{} {
	return &service.DeleteKeyResponse{
		KeyID:        getResultString(m, "key_id"),
		Status:       service.KeyStatus(getResultString(m, "status")),
		DeletionDate: getResultTime(m, "deletion_date"),
		Timestamp:    getResultTime(m, "timestamp"),
	}
}

// decodeKMSSetKeyPolicy requires the key ID that identifies the update.
func decodeKMSSetKeyPolicy(m map[string]interface{}) interface{} {
	keyID := getResultString(m, "key_id")
	if keyID == "" {
		return errors.NewInternal("mcp.kms.SetKeyPolicy", "tool result has no key_id")
	}
	return &service.SetKeyPolicyResponse{
		KeyID:         keyID,
		PolicyVersion: uint64(getResultInt64(m, "policy_version")),
	}
}

// decodeKMSGetKeyPolicy requires the policy, its key ID and its allowed
// operations: an absent list would read as a policy that allows nothing,
// and an unknown operation is refused rather than read as another.
func decodeKMSGetKeyPolicy(m map[string]interface{}) interface{} {
	const op = "mcp.kms.GetKeyPolicy"
	policy := nestedResultMap(m, "policy")
	if policy == nil {
		return errors.NewInternal(op, "tool result has no policy")
	}
	keyID := getResultString(policy, "key_id")
	if keyID == "" {
		return errors.NewInternal(op, "policy has no key_id")
	}
	if _, ok := policy["allowed_operations"]; !ok {
		return errors.NewInternal(op, "policy has no allowed_operations")
	}
	var operations []kmsclient.KeyUsage
	for _, wire := range decodeStringSlice(policy, "allowed_operations") {
		usage, ok := kmsUsages[service.KeyUsage(wire)]
		if !ok {
			return errors.NewInternal(op, "policy has an unknown operation "+wire)
		}
		operations = append(operations, usage)
	}
	// A restriction the result omits or garbles is refused, never read as
	// "unrestricted": a read-modify-write caller would otherwise erase it.
	allowed, err := kmsPolicyPrincipals(op, policy, "allowed_principals")
	if err != nil {
		return err
	}
	denied, err := kmsPolicyPrincipals(op, policy, "denied_principals")
	if err != nil {
		return err
	}
	expiresAt, err := kmsPolicyExpiry(op, policy)
	if err != nil {
		return err
	}
	if _, ok := policy["max_usage_count"]; !ok {
		return errors.NewInternal(op, "policy has no max_usage_count")
	}
	return &kmsclient.KeyPolicy{
		KeyID:             keyID,
		Version:           int(getResultInt64(policy, "version")),
		AllowedOperations: operations,
		AllowedPrincipals: allowed,
		DeniedPrincipals:  denied,
		ExpiresAt:         expiresAt,
		RotationRequired:  getResultBool(policy, "rotation_required"),
		MaxUsageCount:     uint64(getResultInt64(policy, "max_usage_count")),
		RequireMFA:        getResultBool(policy, "require_mfa"),
		UpdatedAt:         getResultTime(policy, "updated_at"),
	}
}

// kmsPolicyPrincipals reads a principal list the policy result must carry as
// an array of strings; an empty array means no restriction.
func kmsPolicyPrincipals(op string, policy map[string]interface{}, key string) ([]string, error) {
	items, ok := policy[key].([]interface{})
	if !ok {
		return nil, errors.NewInternal(op, "policy has no "+key+" list")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, errors.NewInternal(op, "policy "+key+" holds a non-string")
		}
		out = append(out, s)
	}
	return out, nil
}

// kmsPolicyExpiry reads the policy expiry, which the result must carry as an
// RFC 3339 string or as null for a policy that never expires.
func kmsPolicyExpiry(op string, policy map[string]interface{}) (time.Time, error) {
	raw, ok := policy["expires_at"]
	if !ok {
		return time.Time{}, errors.NewInternal(op, "policy has no expires_at")
	}
	if raw == nil {
		return time.Time{}, nil
	}
	s, ok := raw.(string)
	if !ok {
		return time.Time{}, errors.NewInternal(op, "policy expires_at is not an RFC 3339 string")
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, errors.NewInternal(op, "policy expires_at is not RFC 3339: "+err.Error())
	}
	return t, nil
}

// kmsRotationPolicyResult decodes a rotation policy tool result. A null
// policy is no policy and decodes to nil; an absent one, or one missing the
// fields that carry its meaning, is an error.
func kmsRotationPolicyResult(op string, m map[string]interface{}) interface{} {
	if getResultString(m, "key_id") == "" {
		return errors.NewInternal(op, "tool result has no key_id")
	}
	raw, ok := m["policy"]
	if !ok {
		return errors.NewInternal(op, "tool result has no policy")
	}
	if raw == nil {
		return nil
	}
	policy, ok := raw.(map[string]interface{})
	if !ok {
		return errors.NewInternal(op, "policy is not an object")
	}
	for _, field := range []string{"enabled", "auto_rotate", "interval_seconds"} {
		if _, ok := policy[field]; !ok {
			return errors.NewInternal(op, "policy has no "+field)
		}
	}
	return &kmsclient.KeyRotationPolicy{
		Enabled:         getResultBool(policy, "enabled"),
		AutoRotate:      getResultBool(policy, "auto_rotate"),
		IntervalSeconds: getResultInt64(policy, "interval_seconds"),
		NextRotationAt:  getResultTime(policy, "next_rotation_unix"),
		RetainVersions:  int(getResultInt64(policy, "retain_versions")),
	}
}

// decodeKMSSetRotationPolicy requires the policy the tool stored.
func decodeKMSSetRotationPolicy(m map[string]interface{}) interface{} {
	const op = "mcp.kms.SetKeyRotationPolicy"
	decoded := kmsRotationPolicyResult(op, m)
	if decoded == nil {
		return errors.NewInternal(op, "tool result has no policy")
	}
	return decoded
}

// decodeKMSGetRotationPolicy decodes to nil when the key has no policy,
// which the KMS client reports as no policy configured.
func decodeKMSGetRotationPolicy(m map[string]interface{}) interface{} {
	return kmsRotationPolicyResult("mcp.kms.GetKeyRotationPolicy", m)
}

// decodeKMSDeleteRotationPolicy requires the key ID that identifies the
// removal.
func decodeKMSDeleteRotationPolicy(m map[string]interface{}) interface{} {
	keyID := getResultString(m, "key_id")
	if keyID == "" {
		return errors.NewInternal("mcp.kms.DeleteKeyRotationPolicy", "tool result has no key_id")
	}
	return &service.DeleteKeyRotationPolicyResponse{KeyID: keyID, Timestamp: getResultTime(m, "timestamp")}
}

// decodeKMSExportKey decodes the export tool result. The wrapped material is
// the whole point of the call, so a missing, malformed or empty wrapped_key is
// an error, as is a format or wrapping algorithm the client has no value for.
func decodeKMSExportKey(m map[string]interface{}) interface{} {
	const op = "mcp.kms.ExportKey"
	wrapped, err := base64.StdEncoding.DecodeString(getResultString(m, "wrapped_key"))
	if err != nil {
		return errors.Wrap(err, errors.CodeInternal, op, "wrapped_key is not standard base64")
	}
	if len(wrapped) == 0 {
		return errors.NewInternal(op, "tool result has no wrapped_key")
	}
	format, ok := kmsExportFormats[getResultString(m, "format")]
	if !ok {
		return errors.NewInternal(op, "unknown export format "+getResultString(m, "format"))
	}
	wrapping, ok := kmsWrappingAlgorithms[getResultString(m, "wrapping_algorithm")]
	if !ok {
		return errors.NewInternal(op, "unknown wrapping algorithm "+getResultString(m, "wrapping_algorithm"))
	}
	return &kmsclient.ExportKeyResponse{
		KeyID:             getResultString(m, "key_id"),
		Format:            format,
		WrappingKeyID:     getResultString(m, "wrapping_key_id"),
		WrappingAlgorithm: wrapping,
		WrappedMaterial:   wrapped,
		Version:           int(getResultInt64(m, "version")),
	}
}

// decodeKMSUnwrapKey decodes the unwrap tool result into the client type.
func decodeKMSUnwrapKey(m map[string]interface{}) interface{} {
	const op = "mcp.kms.UnwrapKey"
	algorithm, ok := kmsAlgorithms[service.KeyAlgorithm(getResultString(m, "algorithm"))]
	if !ok {
		return errors.NewInternal(op, "unknown key algorithm "+getResultString(m, "algorithm"))
	}
	return &kmsclient.UnwrapKeyResponse{
		KeyID:     getResultString(m, "key_id"),
		Version:   int(getResultInt64(m, "version")),
		Algorithm: algorithm,
	}
}

// requestKMSPublicKey calls the public-key tool and decodes its result. The
// public key is required: a missing, empty or malformed value is an error.
func (t *Transport) requestKMSPublicKey(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "mcp.kms.GetPublicKey"
	arguments, err := kmsKeyIDArgs(op)(req)
	if err != nil {
		return nil, err
	}
	result, err := t.callTool(ctx, kmsPublicKeyTool, arguments)
	if err != nil {
		return nil, err
	}
	m, err := asResultMap(result, op)
	if err != nil {
		return nil, err
	}
	encoded := getResultString(m, "public_key")
	if encoded == "" {
		return nil, errors.New(errors.CodeInternal, op, "tool result has no public_key")
	}
	publicKey, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "public_key is not valid base64")
	}
	return &kmsclient.GetPublicKeyResponse{
		KeyID:     getResultString(m, "key_id"),
		PublicKey: publicKey,
		Algorithm: kmsAlgorithms[service.KeyAlgorithm(getResultString(m, "algorithm"))],
		Version:   int(getResultInt64(m, "version")),
	}, nil
}
