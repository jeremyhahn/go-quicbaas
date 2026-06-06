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

package quic

import (
	"encoding/json"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// objectstorageServicePrefix is the service name prefix for ObjectStorage ops.
const objectstorageServicePrefix = "quicbaas.ObjectStorage"

// isObjectStorageService reports whether svc names the QuicBaaS ObjectStorage
// service. The check is exact-suffix to avoid matching the distinct
// quicbaas.ObjectStorageAdmin service, which has its own opcodes and decoder.
func isObjectStorageService(svc string) bool {
	if svc == "ObjectStorage" {
		return true
	}
	// Match "quicbaas.ObjectStorage" but NOT "quicbaas.ObjectStorageAdmin".
	return svc == objectstorageServicePrefix
}

// =============================================================================
// Response decoding (server service.*Response wire shape -> SDK objectstorage types)
// =============================================================================
//
// The ObjectStorage SDK client (sdk/go/client/objectstorage) asserts on bespoke
// objectstorage.*Response types. The QUIC server's handler_objectstorage.go
// encodes the canonical pkg/service.*Response structs. Both sides declare their
// exported fields without JSON tags (PascalCase) and with identical field names,
// so a direct unmarshal of the server bytes into the SDK response round-trips
// every scalar, byte-slice (checksum / value) and map field — including the
// []byte fields, which JSON encodes as base64 on both sides.
//
// Caveat (request and policy responses): the nested replication / archive /
// retention policy structs are NOT tag-compatible. The server's catalog policy
// types use snake_case JSON tags (min_replicas, target_stores, archive_after_days,
// ...) while the SDK policy structs are PascalCase with no tags. Object CRUD,
// listing, copy, replicate, migrate, archive, restore and verify responses carry
// no nested policy structs and round-trip exactly. SetObjectPolicy / GetObjectPolicy
// responses decode their top-level fields (Key, Success, Version, Timestamp)
// correctly, but their nested policy structs only recover single-word fields. See
// the package report for the precise uncertain field set.

// objectStorageResponseDecoder decodes raw wire JSON for one method into the SDK
// objectstorage type its caller expects.
type objectStorageResponseDecoder func(data []byte) (interface{}, error)

// objectStorageResponseDecoders maps method names to their decoders for O(1)
// dispatch.
var objectStorageResponseDecoders = map[string]objectStorageResponseDecoder{
	"PutObject":       decodePutObjectResponse,
	"GetObject":       decodeGetObjectResponse,
	"DeleteObject":    decodeDeleteObjectResponse,
	"HeadObject":      decodeHeadObjectResponse,
	"ListObjects":     decodeListObjectsResponse,
	"ListByTag":       decodeListByTagResponse,
	"ListByTenant":    decodeListByTenantResponse,
	"CopyObject":      decodeCopyObjectResponse,
	"ReplicateObject": decodeReplicateObjectResponse,
	"MigrateObject":   decodeMigrateObjectResponse,
	"ArchiveObject":   decodeArchiveObjectResponse,
	"RestoreObject":   decodeRestoreObjectResponse,
	"VerifyObject":    decodeVerifyObjectResponse,
	"SetObjectPolicy": decodeSetObjectPolicyResponse,
	"GetObjectPolicy": decodeGetObjectPolicyResponse,
}

// decodeObjectStorageResponse decodes raw wire bytes into the SDK objectstorage
// type for the given method. An empty payload returns nil. Unknown methods are
// rejected to prevent silent mis-routing.
func decodeObjectStorageResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	decoder, ok := objectStorageResponseDecoders[method]
	if !ok {
		return nil, errors.NewInvalidInput("quic.decodeObjectStorageResponse",
			"unknown objectstorage method: "+method)
	}
	return decoder(data)
}

func decodePutObjectResponse(data []byte) (interface{}, error) {
	var resp objectstorage.PutObjectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("PutObject", err)
	}
	return &resp, nil
}

func decodeGetObjectResponse(data []byte) (interface{}, error) {
	var resp objectstorage.GetObjectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("GetObject", err)
	}
	return &resp, nil
}

func decodeDeleteObjectResponse(data []byte) (interface{}, error) {
	var resp objectstorage.DeleteObjectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("DeleteObject", err)
	}
	return &resp, nil
}

func decodeHeadObjectResponse(data []byte) (interface{}, error) {
	var resp objectstorage.HeadObjectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("HeadObject", err)
	}
	return &resp, nil
}

func decodeListObjectsResponse(data []byte) (interface{}, error) {
	var resp objectstorage.ListObjectsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("ListObjects", err)
	}
	if resp.Objects == nil {
		resp.Objects = []*objectstorage.ObjectSummary{}
	}
	return &resp, nil
}

func decodeListByTagResponse(data []byte) (interface{}, error) {
	var resp objectstorage.ListByTagResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("ListByTag", err)
	}
	if resp.Objects == nil {
		resp.Objects = []*objectstorage.ObjectSummary{}
	}
	return &resp, nil
}

func decodeListByTenantResponse(data []byte) (interface{}, error) {
	var resp objectstorage.ListByTenantResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("ListByTenant", err)
	}
	if resp.Objects == nil {
		resp.Objects = []*objectstorage.ObjectSummary{}
	}
	return &resp, nil
}

func decodeCopyObjectResponse(data []byte) (interface{}, error) {
	var resp objectstorage.CopyObjectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("CopyObject", err)
	}
	return &resp, nil
}

func decodeReplicateObjectResponse(data []byte) (interface{}, error) {
	var resp objectstorage.ReplicateObjectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("ReplicateObject", err)
	}
	return &resp, nil
}

func decodeMigrateObjectResponse(data []byte) (interface{}, error) {
	var resp objectstorage.MigrateObjectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("MigrateObject", err)
	}
	return &resp, nil
}

func decodeArchiveObjectResponse(data []byte) (interface{}, error) {
	var resp objectstorage.ArchiveObjectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("ArchiveObject", err)
	}
	return &resp, nil
}

func decodeRestoreObjectResponse(data []byte) (interface{}, error) {
	var resp objectstorage.RestoreObjectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("RestoreObject", err)
	}
	return &resp, nil
}

func decodeVerifyObjectResponse(data []byte) (interface{}, error) {
	var resp objectstorage.VerifyObjectResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("VerifyObject", err)
	}
	return &resp, nil
}

func decodeSetObjectPolicyResponse(data []byte) (interface{}, error) {
	var resp objectstorage.SetObjectPolicyResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("SetObjectPolicy", err)
	}
	return &resp, nil
}

func decodeGetObjectPolicyResponse(data []byte) (interface{}, error) {
	var resp objectstorage.GetObjectPolicyResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapObjectStorageDecode("GetObjectPolicy", err)
	}
	return &resp, nil
}

func wrapObjectStorageDecode(method string, err error) error {
	return errors.Wrap(err, errors.CodeInternal,
		"quic.decodeObjectStorageResponse", "failed to unmarshal "+method+" response")
}
