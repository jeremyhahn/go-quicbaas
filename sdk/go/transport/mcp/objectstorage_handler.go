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

// objectstorage_handler.go provides typed MCP routing for the
// quicbaas.ObjectStorage service. The generic MCP dispatch returns an opaque
// map the SDK objectstorage client cannot type-assert into its concrete
// *Response structs; this handler maps each SDK method onto its quicbaasd
// MCP tool (the quicbaas_object_* surface), builds the tool arguments, and
// decodes the tool result into the precise SDK response type.
//
// Wire conventions applied here mirror the quicbaasd objectstorage tools:
//   - object bodies are base64-encoded under "value"
//   - checksums are hex-encoded strings, decoded back to []byte
//   - timestamps are numeric (Unix nanoseconds) int64 fields
//
// Streaming methods (StreamPut/StreamGet) are not routed here; the MCP
// transport does not support streaming and rejects them upstream.

package mcp

import (
	"encoding/base64"
	"encoding/hex"

	os "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isMCPObjectStorageService reports whether service resolves to the
// ObjectStorage service.
func isMCPObjectStorageService(service string) bool {
	return trimServiceName(service) == "ObjectStorage"
}

// objectStorageMethodToTool maps ObjectStorage SDK method names onto
// quicbaasd MCP tool names.
var objectStorageMethodToTool = map[string]string{
	"PutObject":          "quicbaas_object_put",
	"GetObject":          "quicbaas_object_get",
	"DeleteObject":       "quicbaas_object_delete",
	"HeadObject":         "quicbaas_object_head",
	"ListObjects":        "quicbaas_object_list",
	"ListByTag":          "quicbaas_object_list_by_tag",
	"ListByTenant":       "quicbaas_object_list_by_tenant",
	"CopyObject":         "quicbaas_object_copy",
	"ReplicateObject":    "quicbaas_object_replicate",
	"MigrateObject":      "quicbaas_object_migrate",
	"ArchiveObject":      "quicbaas_object_archive",
	"RestoreObject":      "quicbaas_object_restore",
	"VerifyObject":       "quicbaas_object_verify",
	"SetObjectPolicy":    "quicbaas_object_set_policy",
	"GetObjectPolicy":    "quicbaas_object_get_policy",
	"ListBackends":       "quicbaas_object_list_backends",
	"GetBackendHealth":   "quicbaas_object_backend_health",
	"GetTenantUsage":     "quicbaas_object_tenant_usage",
	"GetTenantQuotas":    "quicbaas_object_tenant_quotas",
	"SetTenantQuotas":    "quicbaas_object_set_tenant_quotas",
	"BatchPutObjects":    "quicbaas_object_batch_put",
	"BatchGetObjects":    "quicbaas_object_batch_get",
	"BatchDeleteObjects": "quicbaas_object_batch_delete",
}

// mapObjectStorageMethodToTool returns the MCP tool name for an
// ObjectStorage method.
func mapObjectStorageMethodToTool(method string) (string, bool) {
	tool, ok := objectStorageMethodToTool[method]
	return tool, ok
}

// objectStorageHandler transforms ObjectStorage SDK requests/responses for
// the MCP transport.
type objectStorageHandler struct {
	transport *Transport
}

// newObjectStorageHandler constructs an ObjectStorage MCP handler bound to t.
func newObjectStorageHandler(t *Transport) *objectStorageHandler {
	return &objectStorageHandler{transport: t}
}

// transformRequest builds MCP tool arguments for an ObjectStorage method.
func (h *objectStorageHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	switch method {
	case "PutObject":
		return h.transformPut(req)
	case "GetObject":
		return h.transformGet(req)
	case "DeleteObject":
		return h.transformDelete(req)
	case "HeadObject":
		return h.transformHead(req)
	case "ListObjects":
		return h.transformList(req)
	case "ListByTag":
		return h.transformListByTag(req)
	case "ListByTenant":
		return h.transformListByTenant(req)
	case "CopyObject":
		return h.transformCopy(req)
	case "ReplicateObject":
		return h.transformReplicate(req)
	case "MigrateObject":
		return h.transformMigrate(req)
	case "ArchiveObject":
		return h.transformArchive(req)
	case "RestoreObject":
		return h.transformRestore(req)
	case "VerifyObject":
		return h.transformVerify(req)
	case "SetObjectPolicy":
		return h.transformSetPolicy(req)
	case "GetObjectPolicy":
		return h.transformGetPolicy(req)
	case "ListBackends":
		return h.transformListBackends(req)
	case "GetBackendHealth":
		return h.transformBackendHealth(req)
	case "GetTenantUsage":
		return h.transformTenantUsage(req)
	case "GetTenantQuotas":
		return h.transformTenantQuotas(req)
	case "SetTenantQuotas":
		return h.transformSetTenantQuotas(req)
	case "BatchPutObjects":
		return h.transformBatchPut(req)
	case "BatchGetObjects":
		return h.transformBatchGet(req)
	case "BatchDeleteObjects":
		return h.transformBatchDelete(req)
	default:
		return nil, newObjectStorageUnsupportedError(method)
	}
}

// transformResponse decodes the MCP tool result into the concrete SDK type.
func (h *objectStorageHandler) transformResponse(method string, resp interface{}) (interface{}, error) {
	switch method {
	case "PutObject":
		return h.decodePut(resp)
	case "GetObject":
		return h.decodeGet(resp)
	case "DeleteObject":
		return h.decodeDelete(resp)
	case "HeadObject":
		return h.decodeHead(resp)
	case "ListObjects":
		return h.decodeList(resp)
	case "ListByTag":
		return h.decodeListByTag(resp)
	case "ListByTenant":
		return h.decodeListByTenant(resp)
	case "CopyObject":
		return h.decodeCopy(resp)
	case "ReplicateObject":
		return h.decodeReplicate(resp)
	case "MigrateObject":
		return h.decodeMigrate(resp)
	case "ArchiveObject":
		return h.decodeArchive(resp)
	case "RestoreObject":
		return h.decodeRestore(resp)
	case "VerifyObject":
		return h.decodeVerify(resp)
	case "SetObjectPolicy":
		return h.decodeSetPolicy(resp)
	case "GetObjectPolicy":
		return h.decodeGetPolicy(resp)
	case "ListBackends":
		return h.decodeListBackends(resp)
	case "GetBackendHealth":
		return h.decodeBackendHealth(resp)
	case "GetTenantUsage":
		return h.decodeTenantUsage(resp)
	case "GetTenantQuotas":
		return h.decodeTenantQuotas(resp)
	case "SetTenantQuotas":
		return h.decodeSetTenantQuotas(resp)
	case "BatchPutObjects":
		return h.decodeBatchPut(resp)
	case "BatchGetObjects":
		return h.decodeBatchGet(resp)
	case "BatchDeleteObjects":
		return h.decodeBatchDelete(resp)
	default:
		return nil, newObjectStorageUnsupportedError(method)
	}
}

func newObjectStorageUnsupportedError(method string) error {
	return errors.New(errors.CodeUnimplemented, "mcp.objectstorage."+method,
		"objectstorage method is not supported by the MCP transport")
}

// decodeChecksum decodes a hex-encoded checksum string into bytes. An
// undecodable value yields nil rather than an error so a malformed checksum
// does not fail the whole response.
func decodeChecksum(m map[string]interface{}, key string) []byte {
	s := getResultString(m, key)
	if s == "" {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

// decodeObjectSummaries decodes the "objects" array shared by the list tools.
func decodeObjectSummaries(m map[string]interface{}) []*os.ObjectSummary {
	out := make([]*os.ObjectSummary, 0)
	raw, ok := m["objects"].([]interface{})
	if !ok {
		return out
	}
	for _, item := range raw {
		om, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		out = append(out, &os.ObjectSummary{
			Key:         getResultString(om, "key"),
			Size:        getResultInt64(om, "size"),
			ContentType: getResultString(om, "content_type"),
			Version:     getResultUint64(om, "version"),
			Status:      getResultString(om, "status"),
			Tags:        getResultStringMap(om, "tags"),
			CreatedAt:   getResultInt64(om, "created_at"),
			ModifiedAt:  getResultInt64(om, "modified_at"),
		})
	}
	return out
}

// =============================================================================
// Core object operations
// =============================================================================

func (h *objectStorageHandler) transformPut(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.PutObjectRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("PutObject", "*objectstorage.PutObjectRequest")
	}
	args := map[string]interface{}{
		"key":   r.Key,
		"value": base64.StdEncoding.EncodeToString(r.Value),
	}
	putStringArg(args, "content_type", r.ContentType)
	putStringArg(args, "tenant_id", r.TenantID)
	putStringArg(args, "target_backend", r.TargetBackend)
	if len(r.Tags) > 0 {
		args["tags"] = stringMapToInterface(r.Tags)
	}
	return args, nil
}

func (h *objectStorageHandler) decodePut(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.PutObject")
	if err != nil {
		return nil, err
	}
	return &os.PutObjectResponse{
		Key:       getResultString(m, "key"),
		ObjectID:  getResultUint64(m, "object_id"),
		Checksum:  decodeChecksum(m, "checksum"),
		Size:      getResultInt64(m, "size"),
		Backend:   getResultString(m, "backend"),
		Version:   getResultUint64(m, "version"),
		CreatedAt: getResultInt64(m, "created_at"),
	}, nil
}

func (h *objectStorageHandler) transformGet(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.GetObjectRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("GetObject", "*objectstorage.GetObjectRequest")
	}
	args := map[string]interface{}{
		"key":              r.Key,
		"include_metadata": r.IncludeMetadata,
	}
	putStringArg(args, "tenant_id", r.TenantID)
	return args, nil
}

func (h *objectStorageHandler) decodeGet(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.GetObject")
	if err != nil {
		return nil, err
	}
	value, err := decodeBase64Value(m, "value")
	if err != nil {
		return nil, err
	}
	return &os.GetObjectResponse{
		Key:         getResultString(m, "key"),
		Value:       value,
		ContentType: getResultString(m, "content_type"),
		Size:        getResultInt64(m, "size"),
		Checksum:    decodeChecksum(m, "checksum"),
		Version:     getResultUint64(m, "version"),
		Tags:        getResultStringMap(m, "tags"),
		TenantID:    getResultString(m, "tenant_id"),
		CreatedAt:   getResultInt64(m, "created_at"),
		ModifiedAt:  getResultInt64(m, "modified_at"),
		Backend:     getResultString(m, "backend"),
	}, nil
}

func (h *objectStorageHandler) transformDelete(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.DeleteObjectRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("DeleteObject", "*objectstorage.DeleteObjectRequest")
	}
	args := map[string]interface{}{"key": r.Key}
	putStringArg(args, "tenant_id", r.TenantID)
	return args, nil
}

func (h *objectStorageHandler) decodeDelete(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.DeleteObject")
	if err != nil {
		return nil, err
	}
	return &os.DeleteObjectResponse{
		Key:       getResultString(m, "key"),
		Success:   getResultBool(m, "success"),
		Size:      getResultInt64(m, "size"),
		Timestamp: getResultInt64(m, "timestamp"),
	}, nil
}

func (h *objectStorageHandler) transformHead(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.HeadObjectRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("HeadObject", "*objectstorage.HeadObjectRequest")
	}
	args := map[string]interface{}{"key": r.Key}
	putStringArg(args, "tenant_id", r.TenantID)
	return args, nil
}

func (h *objectStorageHandler) decodeHead(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.HeadObject")
	if err != nil {
		return nil, err
	}
	return &os.HeadObjectResponse{
		Key:          getResultString(m, "key"),
		Exists:       getResultBool(m, "exists"),
		ContentType:  getResultString(m, "content_type"),
		Size:         getResultInt64(m, "size"),
		Checksum:     decodeChecksum(m, "checksum"),
		Version:      getResultUint64(m, "version"),
		Tags:         getResultStringMap(m, "tags"),
		TenantID:     getResultString(m, "tenant_id"),
		Status:       getResultString(m, "status"),
		CreatedAt:    getResultInt64(m, "created_at"),
		ModifiedAt:   getResultInt64(m, "modified_at"),
		LastAccessAt: getResultInt64(m, "last_access_at"),
		Backend:      getResultString(m, "backend"),
		ReplicaCount: int(getResultInt64(m, "replica_count")),
	}, nil
}

// =============================================================================
// Listing operations
// =============================================================================

func (h *objectStorageHandler) transformList(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.ListObjectsRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("ListObjects", "*objectstorage.ListObjectsRequest")
	}
	args := map[string]interface{}{}
	putStringArg(args, "prefix", r.Prefix)
	putStringArg(args, "tenant_id", r.TenantID)
	putStringArg(args, "cursor", r.Cursor)
	if r.Limit > 0 {
		args["limit"] = r.Limit
	}
	if r.IncludeTags {
		args["include_tags"] = true
	}
	return args, nil
}

func (h *objectStorageHandler) decodeList(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.ListObjects")
	if err != nil {
		return nil, err
	}
	return &os.ListObjectsResponse{
		Objects:    decodeObjectSummaries(m),
		NextCursor: getResultString(m, "next_cursor"),
		HasMore:    getResultBool(m, "has_more"),
		TotalCount: getResultInt64(m, "total_count"),
		Timestamp:  getResultInt64(m, "timestamp"),
	}, nil
}

func (h *objectStorageHandler) transformListByTag(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.ListByTagRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("ListByTag", "*objectstorage.ListByTagRequest")
	}
	args := map[string]interface{}{"tag_name": r.TagName}
	putStringArg(args, "tag_value", r.TagValue)
	putStringArg(args, "tenant_id", r.TenantID)
	putStringArg(args, "cursor", r.Cursor)
	if r.Limit > 0 {
		args["limit"] = r.Limit
	}
	return args, nil
}

func (h *objectStorageHandler) decodeListByTag(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.ListByTag")
	if err != nil {
		return nil, err
	}
	return &os.ListByTagResponse{
		Objects:    decodeObjectSummaries(m),
		NextCursor: getResultString(m, "next_cursor"),
		HasMore:    getResultBool(m, "has_more"),
		TotalCount: getResultInt64(m, "total_count"),
		Timestamp:  getResultInt64(m, "timestamp"),
	}, nil
}

func (h *objectStorageHandler) transformListByTenant(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.ListByTenantRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("ListByTenant", "*objectstorage.ListByTenantRequest")
	}
	args := map[string]interface{}{"tenant_id": r.TenantID}
	putStringArg(args, "cursor", r.Cursor)
	if r.Limit > 0 {
		args["limit"] = r.Limit
	}
	if r.IncludeTags {
		args["include_tags"] = true
	}
	return args, nil
}

func (h *objectStorageHandler) decodeListByTenant(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.ListByTenant")
	if err != nil {
		return nil, err
	}
	return &os.ListByTenantResponse{
		Objects:    decodeObjectSummaries(m),
		NextCursor: getResultString(m, "next_cursor"),
		HasMore:    getResultBool(m, "has_more"),
		TotalCount: getResultInt64(m, "total_count"),
		Timestamp:  getResultInt64(m, "timestamp"),
	}, nil
}

// =============================================================================
// Copy / replicate / migrate
// =============================================================================

func (h *objectStorageHandler) transformCopy(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.CopyObjectRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("CopyObject", "*objectstorage.CopyObjectRequest")
	}
	args := map[string]interface{}{
		"source_key":      r.SourceKey,
		"destination_key": r.DestinationKey,
	}
	putStringArg(args, "tenant_id", r.TenantID)
	putStringArg(args, "target_backend", r.TargetBackend)
	if r.OverwriteExisting {
		args["overwrite_existing"] = true
	}
	return args, nil
}

func (h *objectStorageHandler) decodeCopy(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.CopyObject")
	if err != nil {
		return nil, err
	}
	return &os.CopyObjectResponse{
		Key:       getResultString(m, "key"),
		ObjectID:  getResultUint64(m, "object_id"),
		Size:      getResultInt64(m, "size"),
		Checksum:  decodeChecksum(m, "checksum"),
		Version:   getResultUint64(m, "version"),
		Backend:   getResultString(m, "backend"),
		CreatedAt: getResultInt64(m, "created_at"),
	}, nil
}

func (h *objectStorageHandler) transformReplicate(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.ReplicateObjectRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("ReplicateObject", "*objectstorage.ReplicateObjectRequest")
	}
	args := map[string]interface{}{
		"key":     r.Key,
		"targets": stringSliceToInterface(r.Targets),
	}
	putStringArg(args, "tenant_id", r.TenantID)
	if r.Synchronous {
		args["synchronous"] = true
	}
	if r.Priority > 0 {
		args["priority"] = r.Priority
	}
	return args, nil
}

func (h *objectStorageHandler) decodeReplicate(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.ReplicateObject")
	if err != nil {
		return nil, err
	}
	out := &os.ReplicateObjectResponse{
		Key:       getResultString(m, "key"),
		Status:    getResultString(m, "status"),
		Results:   map[string]*os.ReplicationResult{},
		Timestamp: getResultInt64(m, "timestamp"),
	}
	if results, ok := m["results"].(map[string]interface{}); ok {
		for target, rv := range results {
			rm, ok := rv.(map[string]interface{})
			if !ok {
				continue
			}
			out.Results[target] = &os.ReplicationResult{
				Target:   target,
				Success:  getResultBool(rm, "success"),
				Error:    getResultString(rm, "error"),
				Duration: getResultInt64(rm, "duration"),
			}
		}
	}
	return out, nil
}

func (h *objectStorageHandler) transformMigrate(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.MigrateObjectRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("MigrateObject", "*objectstorage.MigrateObjectRequest")
	}
	args := map[string]interface{}{
		"key":                r.Key,
		"target_backend":     r.TargetBackend,
		"delete_from_source": r.DeleteFromSource,
	}
	putStringArg(args, "tenant_id", r.TenantID)
	return args, nil
}

func (h *objectStorageHandler) decodeMigrate(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.MigrateObject")
	if err != nil {
		return nil, err
	}
	return &os.MigrateObjectResponse{
		Key:               getResultString(m, "key"),
		SourceBackend:     getResultString(m, "source_backend"),
		TargetBackend:     getResultString(m, "target_backend"),
		Success:           getResultBool(m, "success"),
		DeletedFromSource: getResultBool(m, "deleted_from_source"),
		Timestamp:         getResultInt64(m, "timestamp"),
	}, nil
}

// =============================================================================
// Archive / restore / verify
// =============================================================================

func (h *objectStorageHandler) transformArchive(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.ArchiveObjectRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("ArchiveObject", "*objectstorage.ArchiveObjectRequest")
	}
	args := map[string]interface{}{
		"key":                 r.Key,
		"delete_from_primary": r.DeleteFromPrimary,
	}
	putStringArg(args, "target_store", r.TargetStore)
	putStringArg(args, "tenant_id", r.TenantID)
	return args, nil
}

func (h *objectStorageHandler) decodeArchive(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.ArchiveObject")
	if err != nil {
		return nil, err
	}
	return &os.ArchiveObjectResponse{
		Key:                getResultString(m, "key"),
		ArchiveStore:       getResultString(m, "archive_store"),
		Success:            getResultBool(m, "success"),
		DeletedFromPrimary: getResultBool(m, "deleted_from_primary"),
		ArchivedAt:         getResultInt64(m, "archived_at"),
	}, nil
}

func (h *objectStorageHandler) transformRestore(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.RestoreObjectRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("RestoreObject", "*objectstorage.RestoreObjectRequest")
	}
	args := map[string]interface{}{"key": r.Key}
	putStringArg(args, "target_backend", r.TargetBackend)
	putStringArg(args, "tenant_id", r.TenantID)
	return args, nil
}

func (h *objectStorageHandler) decodeRestore(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.RestoreObject")
	if err != nil {
		return nil, err
	}
	return &os.RestoreObjectResponse{
		Key:        getResultString(m, "key"),
		Backend:    getResultString(m, "backend"),
		Success:    getResultBool(m, "success"),
		RestoredAt: getResultInt64(m, "restored_at"),
	}, nil
}

func (h *objectStorageHandler) transformVerify(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.VerifyObjectRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("VerifyObject", "*objectstorage.VerifyObjectRequest")
	}
	args := map[string]interface{}{"key": r.Key}
	if len(r.Stores) > 0 {
		args["stores"] = stringSliceToInterface(r.Stores)
	}
	putStringArg(args, "tenant_id", r.TenantID)
	if r.RepairMismatches {
		args["repair_mismatches"] = true
	}
	return args, nil
}

func (h *objectStorageHandler) decodeVerify(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.VerifyObject")
	if err != nil {
		return nil, err
	}
	out := &os.VerifyObjectResponse{
		Key:       getResultString(m, "key"),
		Valid:     getResultBool(m, "valid"),
		Results:   map[string]*os.VerificationResult{},
		Repaired:  getResultBool(m, "repaired"),
		Timestamp: getResultInt64(m, "timestamp"),
	}
	if results, ok := m["results"].(map[string]interface{}); ok {
		for store, rv := range results {
			rm, ok := rv.(map[string]interface{})
			if !ok {
				continue
			}
			out.Results[store] = &os.VerificationResult{
				Store:         store,
				Valid:         getResultBool(rm, "valid"),
				ChecksumMatch: getResultBool(rm, "checksum_match"),
				SizeMatch:     getResultBool(rm, "size_match"),
				Error:         getResultString(rm, "error"),
			}
		}
	}
	return out, nil
}

// =============================================================================
// Policy operations
// =============================================================================

func (h *objectStorageHandler) transformSetPolicy(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.SetObjectPolicyRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("SetObjectPolicy", "*objectstorage.SetObjectPolicyRequest")
	}
	args := map[string]interface{}{"key": r.Key}
	putStringArg(args, "tenant_id", r.TenantID)
	if r.ReplicationPolicy != nil {
		args["replication"] = map[string]interface{}{
			"enabled":       r.ReplicationPolicy.Enabled,
			"min_replicas":  r.ReplicationPolicy.MinReplicas,
			"mode":          string(r.ReplicationPolicy.Mode),
			"priority":      r.ReplicationPolicy.Priority,
			"target_stores": stringSliceToInterface(r.ReplicationPolicy.TargetStores),
		}
	}
	if r.ArchivePolicy != nil {
		args["archive"] = map[string]interface{}{
			"enabled":             r.ArchivePolicy.Enabled,
			"after_days":          r.ArchivePolicy.ArchiveAfterDays,
			"target_store":        r.ArchivePolicy.TargetStore,
			"delete_from_primary": r.ArchivePolicy.DeleteFromPrimary,
		}
	}
	if r.RetentionPolicy != nil {
		args["retention"] = map[string]interface{}{
			"enabled": r.RetentionPolicy.Enabled,
			"days":    r.RetentionPolicy.RetentionDays,
		}
	}
	return args, nil
}

func (h *objectStorageHandler) decodeSetPolicy(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.SetObjectPolicy")
	if err != nil {
		return nil, err
	}
	return &os.SetObjectPolicyResponse{
		Key:       getResultString(m, "key"),
		Success:   getResultBool(m, "success"),
		Version:   getResultUint64(m, "version"),
		Timestamp: getResultInt64(m, "timestamp"),
	}, nil
}

func (h *objectStorageHandler) transformGetPolicy(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.GetObjectPolicyRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("GetObjectPolicy", "*objectstorage.GetObjectPolicyRequest")
	}
	args := map[string]interface{}{"key": r.Key}
	putStringArg(args, "tenant_id", r.TenantID)
	return args, nil
}

func (h *objectStorageHandler) decodeGetPolicy(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.GetObjectPolicy")
	if err != nil {
		return nil, err
	}
	out := &os.GetObjectPolicyResponse{
		Key:       getResultString(m, "key"),
		Timestamp: getResultInt64(m, "timestamp"),
	}
	if rep, ok := m["replication"].(map[string]interface{}); ok {
		out.ReplicationPolicy = &os.ReplicationPolicy{
			Enabled:      getResultBool(rep, "enabled"),
			MinReplicas:  int(getResultInt64(rep, "min_replicas")),
			TargetStores: decodeStringSlice(rep, "target_stores"),
			Mode:         os.ReplicationMode(getResultString(rep, "mode")),
			Priority:     int(getResultInt64(rep, "priority")),
		}
	}
	if arch, ok := m["archive"].(map[string]interface{}); ok {
		out.ArchivePolicy = &os.ArchivePolicy{
			Enabled:           getResultBool(arch, "enabled"),
			ArchiveAfterDays:  int(getResultInt64(arch, "after_days")),
			TargetStore:       getResultString(arch, "target_store"),
			DeleteFromPrimary: getResultBool(arch, "delete_from_primary"),
		}
	}
	if ret, ok := m["retention"].(map[string]interface{}); ok {
		out.RetentionPolicy = &os.RetentionPolicy{
			Enabled:       getResultBool(ret, "enabled"),
			RetentionDays: int(getResultInt64(ret, "days")),
		}
	}
	return out, nil
}

// =============================================================================
// Backend management
// =============================================================================

func (h *objectStorageHandler) transformListBackends(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.ListBackendsRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("ListBackends", "*objectstorage.ListBackendsRequest")
	}
	args := map[string]interface{}{}
	if r.IncludeHealth {
		args["include_health"] = true
	}
	putStringArg(args, "filter_by_type", r.FilterByType)
	return args, nil
}

func (h *objectStorageHandler) decodeListBackends(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.ListBackends")
	if err != nil {
		return nil, err
	}
	out := &os.ListBackendsResponse{
		Backends:  []*os.BackendInfo{},
		Timestamp: getResultInt64(m, "timestamp"),
	}
	if raw, ok := m["backends"].([]interface{}); ok {
		for _, item := range raw {
			bm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			b := &os.BackendInfo{
				Name:        getResultString(bm, "name"),
				Type:        getResultString(bm, "type"),
				Enabled:     getResultBool(bm, "enabled"),
				Default:     getResultBool(bm, "default"),
				Tier:        getResultString(bm, "tier"),
				Region:      getResultString(bm, "region"),
				ObjectCount: getResultInt64(bm, "object_count"),
				TotalSize:   getResultInt64(bm, "total_size"),
			}
			if hm, ok := bm["health"].(map[string]interface{}); ok {
				b.Health = &os.BackendHealthInfo{
					Healthy:   getResultBool(hm, "healthy"),
					Latency:   getResultInt64(hm, "latency"),
					LastCheck: getResultInt64(hm, "last_check"),
					Error:     getResultString(hm, "error"),
				}
			}
			out.Backends = append(out.Backends, b)
		}
	}
	return out, nil
}

func (h *objectStorageHandler) transformBackendHealth(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.GetBackendHealthRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("GetBackendHealth", "*objectstorage.GetBackendHealthRequest")
	}
	return map[string]interface{}{"name": r.Name}, nil
}

func (h *objectStorageHandler) decodeBackendHealth(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.GetBackendHealth")
	if err != nil {
		return nil, err
	}
	return &os.GetBackendHealthResponse{
		Name:      getResultString(m, "name"),
		Healthy:   getResultBool(m, "healthy"),
		Latency:   getResultInt64(m, "latency"),
		LastCheck: getResultInt64(m, "last_check"),
		Status:    getResultString(m, "status"),
		Error:     getResultString(m, "error"),
	}, nil
}

// =============================================================================
// Tenant management
// =============================================================================

func (h *objectStorageHandler) transformTenantUsage(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.GetTenantUsageRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("GetTenantUsage", "*objectstorage.GetTenantUsageRequest")
	}
	return map[string]interface{}{"tenant_id": r.TenantID}, nil
}

func (h *objectStorageHandler) decodeTenantUsage(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.GetTenantUsage")
	if err != nil {
		return nil, err
	}
	return &os.GetTenantUsageResponse{
		TenantID:      getResultString(m, "tenant_id"),
		ObjectCount:   getResultInt64(m, "object_count"),
		TotalSize:     getResultInt64(m, "total_size"),
		ActiveCount:   getResultInt64(m, "active_count"),
		ArchivedCount: getResultInt64(m, "archived_count"),
		Timestamp:     getResultInt64(m, "timestamp"),
	}, nil
}

func (h *objectStorageHandler) transformTenantQuotas(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.GetTenantQuotasRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("GetTenantQuotas", "*objectstorage.GetTenantQuotasRequest")
	}
	return map[string]interface{}{"tenant_id": r.TenantID}, nil
}

func (h *objectStorageHandler) decodeTenantQuotas(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.GetTenantQuotas")
	if err != nil {
		return nil, err
	}
	return &os.GetTenantQuotasResponse{
		TenantID:         getResultString(m, "tenant_id"),
		MaxStorageBytes:  getResultInt64(m, "max_storage_bytes"),
		MaxObjectSize:    getResultInt64(m, "max_object_size"),
		MaxObjectCount:   getResultInt64(m, "max_object_count"),
		UsedStorageBytes: getResultInt64(m, "used_storage_bytes"),
		UsedObjectCount:  getResultInt64(m, "used_object_count"),
		Timestamp:        getResultInt64(m, "timestamp"),
	}, nil
}

func (h *objectStorageHandler) transformSetTenantQuotas(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.SetTenantQuotasRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("SetTenantQuotas", "*objectstorage.SetTenantQuotasRequest")
	}
	return map[string]interface{}{
		"tenant_id":         r.TenantID,
		"max_storage_bytes": r.MaxStorageBytes,
		"max_object_size":   r.MaxObjectSize,
		"max_object_count":  r.MaxObjectCount,
	}, nil
}

func (h *objectStorageHandler) decodeSetTenantQuotas(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.SetTenantQuotas")
	if err != nil {
		return nil, err
	}
	return &os.SetTenantQuotasResponse{
		TenantID:  getResultString(m, "tenant_id"),
		UpdatedAt: getResultInt64(m, "updated_at"),
	}, nil
}

// =============================================================================
// Batch operations
// =============================================================================

func (h *objectStorageHandler) transformBatchPut(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.BatchPutObjectsRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("BatchPutObjects", "*objectstorage.BatchPutObjectsRequest")
	}
	objects := make([]interface{}, 0, len(r.Objects))
	var tenantID, targetBackend string
	for _, obj := range r.Objects {
		if obj == nil {
			continue
		}
		if tenantID == "" {
			tenantID = obj.TenantID
		}
		if targetBackend == "" {
			targetBackend = obj.TargetBackend
		}
		item := map[string]interface{}{
			"key":   obj.Key,
			"value": base64.StdEncoding.EncodeToString(obj.Value),
		}
		putStringArg(item, "content_type", obj.ContentType)
		if len(obj.Tags) > 0 {
			item["tags"] = stringMapToInterface(obj.Tags)
		}
		objects = append(objects, item)
	}
	args := map[string]interface{}{"objects": objects}
	putStringArg(args, "tenant_id", tenantID)
	putStringArg(args, "target_backend", targetBackend)
	return args, nil
}

func (h *objectStorageHandler) decodeBatchPut(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.BatchPutObjects")
	if err != nil {
		return nil, err
	}
	out := &os.BatchPutObjectsResponse{
		Results: []*os.PutObjectResponse{},
		Errors:  map[string]string{},
	}
	if raw, ok := m["results"].([]interface{}); ok {
		for _, item := range raw {
			rm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if getResultBool(rm, "success") {
				out.Succeeded++
				out.Results = append(out.Results, &os.PutObjectResponse{
					Key:      getResultString(rm, "key"),
					ObjectID: getResultUint64(rm, "object_id"),
					Checksum: decodeChecksum(rm, "checksum"),
					Size:     getResultInt64(rm, "size"),
					Backend:  getResultString(rm, "backend"),
				})
			} else {
				out.Failed++
				if key := getResultString(rm, "key"); key != "" {
					out.Errors[key] = getResultString(rm, "error")
				}
			}
		}
	}
	return out, nil
}

func (h *objectStorageHandler) transformBatchGet(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.BatchGetObjectsRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("BatchGetObjects", "*objectstorage.BatchGetObjectsRequest")
	}
	args := map[string]interface{}{
		"keys":             stringSliceToInterface(r.Keys),
		"include_metadata": r.IncludeMetadata,
	}
	putStringArg(args, "tenant_id", r.TenantID)
	return args, nil
}

func (h *objectStorageHandler) decodeBatchGet(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.BatchGetObjects")
	if err != nil {
		return nil, err
	}
	out := &os.BatchGetObjectsResponse{
		Objects:  []*os.GetObjectResponse{},
		NotFound: []string{},
	}
	if raw, ok := m["results"].([]interface{}); ok {
		for _, item := range raw {
			rm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if !getResultBool(rm, "success") {
				if key := getResultString(rm, "key"); key != "" {
					out.NotFound = append(out.NotFound, key)
				}
				continue
			}
			value, decErr := decodeBase64Value(rm, "value")
			if decErr != nil {
				return nil, decErr
			}
			out.Objects = append(out.Objects, &os.GetObjectResponse{
				Key:         getResultString(rm, "key"),
				Value:       value,
				ContentType: getResultString(rm, "content_type"),
				Size:        getResultInt64(rm, "size"),
				Checksum:    decodeChecksum(rm, "checksum"),
				Version:     getResultUint64(rm, "version"),
				Tags:        getResultStringMap(rm, "tags"),
				Backend:     getResultString(rm, "backend"),
			})
		}
	}
	return out, nil
}

func (h *objectStorageHandler) transformBatchDelete(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*os.BatchDeleteObjectsRequest)
	if !ok || r == nil {
		return nil, newObjectStorageInvalidInput("BatchDeleteObjects", "*objectstorage.BatchDeleteObjectsRequest")
	}
	args := map[string]interface{}{
		"keys": stringSliceToInterface(r.Keys),
	}
	putStringArg(args, "tenant_id", r.TenantID)
	return args, nil
}

func (h *objectStorageHandler) decodeBatchDelete(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.objectstorage.BatchDeleteObjects")
	if err != nil {
		return nil, err
	}
	out := &os.BatchDeleteObjectsResponse{
		DeletedKeys: []string{},
		FailedKeys:  []string{},
		Errors:      map[string]string{},
	}
	if raw, ok := m["results"].([]interface{}); ok {
		for _, item := range raw {
			rm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			key := getResultString(rm, "key")
			if getResultBool(rm, "success") {
				out.DeletedKeys = append(out.DeletedKeys, key)
			} else {
				out.FailedKeys = append(out.FailedKeys, key)
				if key != "" {
					out.Errors[key] = getResultString(rm, "error")
				}
			}
		}
	}
	return out, nil
}

// =============================================================================
// Shared helpers
// =============================================================================

func newObjectStorageInvalidInput(method, want string) error {
	return errors.NewInvalidInput("mcp.objectstorage."+method,
		"invalid request type, expected "+want)
}

// decodeBase64Value base64-decodes a string field, returning a typed error
// when the value is present but not valid base64.
func decodeBase64Value(m map[string]interface{}, key string) ([]byte, error) {
	s := getResultString(m, key)
	if s == "" {
		return nil, nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.New(errors.CodeInternal, "mcp.objectstorage.decodeValue",
			"failed to base64-decode object value: "+err.Error())
	}
	return b, nil
}

// decodeStringSlice decodes a []interface{} of strings into []string.
func decodeStringSlice(m map[string]interface{}, key string) []string {
	raw, ok := m[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// stringSliceToInterface converts a []string into []interface{} for tool args.
func stringSliceToInterface(values []string) []interface{} {
	out := make([]interface{}, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
