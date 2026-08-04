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
	"strings"
	"time"

	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// kmsServicePrefix is the service name prefix for KMS operations.
const kmsServicePrefix = "quicbaas.KMS"

// isKMSService reports whether svc names the QuicBaaS KMS service.
func isKMSService(svc string) bool {
	return strings.HasPrefix(svc, kmsServicePrefix) || svc == "KMS"
}

// kmsResponseConverter decodes raw wire JSON into a typed SDK value.
type kmsResponseConverter func(data []byte) (interface{}, error)

// kmsResponseConverters maps KMS method names to their response converters for
// O(1) dispatch. The QUIC server marshals the canonical service.*Response
// structs with encoding/json (see pkg/api/quic/handler_kms.go), and the KMS SDK
// client accepts those canonical types directly (case *service.X), so decoding
// the same bytes back into the canonical type round-trips exactly — matching
// what the embedded/gRPC transports return.
//
// Only methods that the server reaches over QUIC (i.e. have a wire opcode) and
// that the KMS client converts from service.* are included.
var kmsResponseConverters = map[string]kmsResponseConverter{
	"GenerateKey":             convertResponse[service.GenerateKeyResponse],
	"GetKey":                  convertResponse[service.GetKeyResponse],
	"GetPublicKey":            convertResponse[service.GetPublicKeyResponse],
	"ListKeys":                convertResponse[service.ListKeysResponse],
	"RotateKey":               convertResponse[service.RotateKeyResponse],
	"RevokeKey":               convertResponse[service.RevokeKeyResponse],
	"DeleteKey":               convertResponse[service.DeleteKeyResponse],
	"ImportKey":               convertResponse[service.ImportKeyResponse],
	"ExportKey":               convertResponse[service.ExportKeyResponse],
	"UnwrapKey":               convertResponse[service.UnwrapKeyResponse],
	"GetKeyMetadata":          convertResponse[service.GetKeyMetadataResponse],
	"UpdateKeyStatus":         convertResponse[service.UpdateKeyStatusResponse],
	"UpdateKeyMetadata":       convertResponse[service.UpdateKeyMetadataResponse],
	"SetKeyPolicy":            convertResponse[service.SetKeyPolicyResponse],
	"GetKeyPolicy":            convertResponse[service.GetKeyPolicyResponse],
	"SetKeyRotationPolicy":    convertResponse[service.SetKeyRotationPolicyResponse],
	"GetKeyRotationPolicy":    convertResponse[service.GetKeyRotationPolicyResponse],
	"DeleteKeyRotationPolicy": convertResponse[service.DeleteKeyRotationPolicyResponse],
	"GetSupportedAlgorithms":  convertStringSliceResponse,
	"BackendStatus":           convertResponse[service.BackendStatusResponse],
	"ListKeyVersions":         convertResponse[service.ListKeyVersionsResponse],
}

// convertStringSliceResponse decodes a JSON string array (the wire shape the
// QUIC server emits for GetSupportedAlgorithms) into the []string the KMS SDK
// client accepts directly.
func convertStringSliceResponse(data []byte) (interface{}, error) {
	var algorithms []string
	if err := json.Unmarshal(data, &algorithms); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertStringSliceResponse", "failed to unmarshal response")
	}
	return algorithms, nil
}

// decodeKMSResponse decodes raw wire bytes into the canonical SDK KMS type for
// the given method. Unknown methods fall back to a generic map decode.
func decodeKMSResponse(method string, data []byte) (interface{}, error) {
	return decodeServiceResponse("quic.decodeKMSResponse", kmsResponseConverters, method, data)
}

// convertResponse decodes JSON into a *T and returns it. It is the generic
// equivalent of the per-type converters used by the backup handler, eliminating
// the repetitive single-method boilerplate for services whose client accepts
// the canonical service.* response types directly.
func convertResponse[T any](data []byte) (interface{}, error) {
	var resp T
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// decodeServiceResponse is the shared decode loop for service handlers whose
// converters map a method name to a canonical service.* response. Empty payload
// returns nil; an unknown method falls back to a generic map decode so callers
// that do not yet have a typed converter still receive structured data instead
// of an error.
func decodeServiceResponse(op string, converters map[string]kmsResponseConverter, method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}

	converter, ok := converters[method]
	if !ok {
		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to decode response")
		}
		return decoded, nil
	}

	return converter(data)
}

// =============================================================================
// KMS request transforms
// =============================================================================

// kmsRequestTransforms maps KMS method names whose SDK request wire shape
// differs from the canonical service.*Request the QUIC server decodes onto a
// transform that re-encodes them. Methods absent from the map pass through
// unchanged (their SDK request types share the service JSON wire tags).
var kmsRequestTransforms = map[string]func(req interface{}) (interface{}, error){
	"SetKeyRotationPolicy": transformKMSSetKeyRotationPolicy,
	"UpdateKeyMetadata":    transformKMSUpdateKeyMetadata,
	"UpdateKeyStatus":      transformKMSUpdateKeyStatus,
}

// transformKMSRequest re-encodes KMS requests that need a wire transform.
func transformKMSRequest(method string, req interface{}) (interface{}, error) {
	transform, ok := kmsRequestTransforms[method]
	if !ok {
		return req, nil
	}
	return transform(req)
}

// flatKeyRotationPolicyWire mirrors the KMS SDK client's unexported flat
// setKeyRotationPolicyRequest JSON shape.
type flatKeyRotationPolicyWire struct {
	KeyID           string `json:"key_id"`
	AutoRotate      bool   `json:"auto_rotate"`
	IntervalSeconds int64  `json:"interval_seconds"`
	RetainVersions  int    `json:"retain_versions,omitempty"`
	Enabled         bool   `json:"enabled"`
}

// transformKMSSetKeyRotationPolicy converts the SDK client's flat rotation
// policy request into the nested service.SetKeyRotationPolicyRequest the
// server unmarshals. Canonical service requests pass through unchanged.
func transformKMSSetKeyRotationPolicy(req interface{}) (interface{}, error) {
	if r, ok := req.(*service.SetKeyRotationPolicyRequest); ok {
		return r, nil
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.transformKMSSetKeyRotationPolicy", "failed to encode request")
	}
	var flat flatKeyRotationPolicyWire
	if err := json.Unmarshal(raw, &flat); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.transformKMSSetKeyRotationPolicy", "failed to decode request")
	}
	return &service.SetKeyRotationPolicyRequest{
		KeyID: flat.KeyID,
		Policy: &service.RotationPolicy{
			Enabled:        flat.Enabled,
			AutoRotate:     flat.AutoRotate,
			Interval:       time.Duration(flat.IntervalSeconds) * time.Second,
			RetainVersions: flat.RetainVersions,
		},
	}, nil
}

// transformKMSUpdateKeyMetadata converts the exported SDK metadata-update
// request (plain strings, PascalCase wire) into the canonical
// service.UpdateKeyMetadataRequest (snake_case, pointer semantics). Canonical
// service requests pass through unchanged.
// transformKMSUpdateKeyStatus converts the SDK client's int-enum status
// request into the service-layer string-status wire shape the QUIC server
// decodes (service.UpdateKeyStatusRequest with snake_case tags).
func transformKMSUpdateKeyStatus(req interface{}) (interface{}, error) {
	if r, ok := req.(*service.UpdateKeyStatusRequest); ok {
		return r, nil
	}
	sdkReq, ok := req.(*kmsclient.UpdateKeyStatusRequest)
	if !ok {
		return nil, errors.NewInvalidInput("quic.transformKMSUpdateKeyStatus",
			"invalid request type, expected *kms.UpdateKeyStatusRequest")
	}
	return &service.UpdateKeyStatusRequest{
		KeyID:   sdkReq.KeyID,
		Version: uint64(sdkReq.Version), //nolint:gosec // G115: version is a small non-negative counter
		Status:  quicStatusToService(sdkReq.Status),
	}, nil
}

// quicStatusToService converts an SDK KeyStatus enum to the service-layer
// string status (mirrors the gRPC transport's sdkStatusToService).
func quicStatusToService(status kmsclient.KeyStatus) service.KeyStatus {
	switch status {
	case kmsclient.KeyStatusEnabled:
		return service.KeyStatusEnabled
	case kmsclient.KeyStatusDisabled:
		return service.KeyStatusDisabled
	default:
		return service.KeyStatusEnabled
	}
}

func transformKMSUpdateKeyMetadata(req interface{}) (interface{}, error) {
	if r, ok := req.(*service.UpdateKeyMetadataRequest); ok {
		return r, nil
	}
	sdkReq, ok := req.(*kmsclient.UpdateKeyMetadataRequest)
	if !ok {
		return nil, errors.NewInvalidInput("quic.transformKMSUpdateKeyMetadata",
			"invalid request type, expected *kms.UpdateKeyMetadataRequest")
	}
	svcReq := &service.UpdateKeyMetadataRequest{
		KeyID:      sdkReq.KeyID,
		Tags:       sdkReq.Tags,
		RemoveTags: sdkReq.RemoveTags,
	}
	if sdkReq.Name != "" {
		name := sdkReq.Name
		svcReq.Name = &name
	}
	if sdkReq.Description != "" {
		description := sdkReq.Description
		svcReq.Description = &description
	}
	return svcReq, nil
}
