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

package grpc

import (
	"context"
	"crypto/elliptic"
	"crypto/x509"
	"reflect"
	"strings"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"google.golang.org/grpc"

	apigrpc "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// kmsServicePrefix is the gRPC-style service identifier the SDK KMS client
// routes through. The DEK SDK client routes through quicbaas.DEK, which the
// server exposes as the DEKManagement proto service; both are handled here.
const (
	kmsServicePrefix = "quicbaas.KMS"
	dekServicePrefix = "quicbaas.DEK"
)

// isKMSService reports whether svc targets the quicbaas.KMS surface. The KMS
// SDK client sends plain Go structs that do not implement proto.Message, so
// the bare conn.Invoke path cannot serialize them; they must be converted to
// the generated KeyManagement / Crypto proto types first.
func isKMSService(svc string) bool {
	return strings.HasPrefix(svc, kmsServicePrefix)
}

// isDEKService reports whether svc targets the quicbaas.DEK surface, which the
// server registers as the DEKManagement proto service.
func isDEKService(svc string) bool {
	return strings.HasPrefix(svc, dekServicePrefix)
}

// kmsHandler converts between the SDK KMS/DEK client structs and the generated
// KeyManagement / Crypto / DEKManagement proto types. It delegates the actual
// struct<->protobuf conversion and RPC to the pkg/api/grpc Client, which wraps
// the transport's existing connection via NewClientWithConn so no second
// connection is dialed and no conversion logic is forked.
type kmsHandler struct {
	conn   *grpc.ClientConn
	client *apigrpc.Client
}

// newKMSHandler creates a new KMS handler bound to conn. The wrapped
// pkg/api/grpc Client borrows conn and is used ephemerally for this request.
func newKMSHandler(conn *grpc.ClientConn) (*kmsHandler, error) {
	client, err := apigrpc.NewClientWithConn(conn)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "grpc.newKMSHandler",
			"failed to wrap connection")
	}
	return &kmsHandler{conn: conn, client: client}, nil
}

// kmsHandlerFunc handles a single KMS/DEK method.
type kmsHandlerFunc func(h *kmsHandler, ctx context.Context, req interface{}) (interface{}, error)

// kmsHandlerMap maps method names to handler functions for O(1) dispatch. Every
// method exposed by the KeyManagement / Crypto / DEKManagement proto services
// (and wrapped by the pkg/api/grpc Client) is wired here; a method missing from
// the map returns a clear unimplemented error rather than falling through to
// the untyped conn.Invoke path, matching the certificate handler precedent.
var kmsHandlerMap = map[string]kmsHandlerFunc{
	// KeyManagement service.
	"GenerateKey":             (*kmsHandler).handleGenerateKey,
	"ImportKey":               (*kmsHandler).handleImportKey,
	"GetKey":                  (*kmsHandler).handleGetKey,
	"GetPublicKey":            (*kmsHandler).handleGetPublicKey,
	"ListKeys":                (*kmsHandler).handleListKeys,
	"RotateKey":               (*kmsHandler).handleRotateKey,
	"RevokeKey":               (*kmsHandler).handleRevokeKey,
	"DeleteKey":               (*kmsHandler).handleDeleteKey,
	"UpdateKeyStatus":         (*kmsHandler).handleUpdateKeyStatus,
	"ListKeyVersions":         (*kmsHandler).handleListKeyVersions,
	"GetKeyMetadata":          (*kmsHandler).handleGetKeyMetadata,
	"UpdateKeyMetadata":       (*kmsHandler).handleUpdateKeyMetadata,
	"SetKeyPolicy":            (*kmsHandler).handleSetKeyPolicy,
	"GetKeyPolicy":            (*kmsHandler).handleGetKeyPolicy,
	"SetKeyRotationPolicy":    (*kmsHandler).handleSetKeyRotationPolicy,
	"GetKeyRotationPolicy":    (*kmsHandler).handleGetKeyRotationPolicy,
	"DeleteKeyRotationPolicy": (*kmsHandler).handleDeleteKeyRotationPolicy,
	"ExportKey":               (*kmsHandler).handleExportKey,
	"UnwrapKey":               (*kmsHandler).handleUnwrapKey,
	"GetSupportedAlgorithms":  (*kmsHandler).handleGetSupportedAlgorithms,
	"BackendStatus":           (*kmsHandler).handleBackendStatus,

	// DEKManagement service (reachable under both quicbaas.KMS and quicbaas.DEK).
	"CreateDEK":               (*kmsHandler).handleCreateDEK,
	"GetDEK":                  (*kmsHandler).handleGetDEK,
	"ListDEKs":                (*kmsHandler).handleListDEKs,
	"RotateDEK":               (*kmsHandler).handleRotateDEK,
	"DeleteDEK":               (*kmsHandler).handleDeleteDEK,
	"GetDEKStatus":            (*kmsHandler).handleGetDEKStatus,
	"SetDefaultDEK":           (*kmsHandler).handleSetDefaultDEK,
	"SetDEKSafety":            (*kmsHandler).handleSetDEKSafety,
	"ClearDEKSafety":          (*kmsHandler).handleClearDEKSafety,
	"StartReencryption":       (*kmsHandler).handleStartReencryption,
	"GetReencryptionProgress": (*kmsHandler).handleGetReencryptionProgress,
}

// handleRequest routes a KMS/DEK request to the appropriate handler based on
// the method name.
func (h *kmsHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	handler, ok := kmsHandlerMap[method]
	if !ok {
		return nil, errors.NewUnimplemented("kmsHandler.handleRequest",
			"method not exposed by KeyManagement/Crypto/DEKManagement gRPC proto: "+method+
				" (use the REST transport for this verb)")
	}
	return handler(h, ctx, req)
}

// =============================================================================
// KeyManagement handlers
// =============================================================================

func (h *kmsHandler) handleGenerateKey(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.GenerateKeyRequest); ok {
		return h.client.KMSGenerateKey(ctx, r)
	}
	sdkReq, ok := req.(*kmsclient.GenerateKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleGenerateKey",
			"invalid request type, expected *kms.GenerateKeyRequest")
	}
	return h.client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{
		Name:               sdkReq.Name,
		Usage:              sdkUsagesToService(sdkReq.Usage),
		KeyAttributes:      sdkAlgToKeyAttributes(sdkReq.Algorithm),
		Exportable:         sdkReq.Exportable,
		DeletionProtection: sdkReq.DeletionProtection,
	})
}

func (h *kmsHandler) handleImportKey(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.ImportKeyRequest); ok {
		return h.client.KMSImportKey(ctx, r)
	}
	sdkReq, ok := req.(*kmsclient.ImportKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleImportKey",
			"invalid request type, expected *kms.ImportKeyRequest")
	}
	return h.client.KMSImportKey(ctx, &service.ImportKeyRequest{
		KeyMaterial:   sdkReq.KeyMaterial,
		WrappingKeyID: sdkReq.WrappingKeyID,
		Name:          sdkReq.Name,
		Description:   sdkReq.Description,
		Usage:         sdkUsagesToService(sdkReq.Usage),
		KeyAttributes: sdkAlgToKeyAttributes(sdkReq.Algorithm),
		Tags:          sdkReq.Tags,
	})
}

func (h *kmsHandler) handleGetKey(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.GetKeyRequest); ok {
		return h.client.KMSGetKey(ctx, r)
	}
	keyID, ok := stringFieldByName(req, "KeyID")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleGetKey",
			"invalid request type, expected struct with KeyID field")
	}
	return h.client.KMSGetKey(ctx, &service.GetKeyRequest{KeyID: keyID})
}

func (h *kmsHandler) handleListKeys(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.ListKeysRequest); ok {
		return h.client.KMSListKeys(ctx, r)
	}
	// The SDK list request carries no fields.
	return h.client.KMSListKeys(ctx, &service.ListKeysRequest{})
}

func (h *kmsHandler) handleRotateKey(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.RotateKeyRequest); ok {
		return h.client.KMSRotateKey(ctx, r)
	}
	keyID, ok := stringFieldByName(req, "KeyID")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleRotateKey",
			"invalid request type, expected struct with KeyID field")
	}
	return h.client.KMSRotateKey(ctx, &service.RotateKeyRequest{KeyID: keyID})
}

func (h *kmsHandler) handleDeleteKey(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.DeleteKeyRequest); ok {
		return h.client.KMSDeleteKey(ctx, r)
	}
	keyID, ok := stringFieldByName(req, "KeyID")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleDeleteKey",
			"invalid request type, expected struct with KeyID field")
	}
	return h.client.KMSDeleteKey(ctx, &service.DeleteKeyRequest{KeyID: keyID})
}

func (h *kmsHandler) handleUpdateKeyStatus(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.UpdateKeyStatusRequest); ok {
		return h.client.KMSUpdateKeyStatus(ctx, r)
	}
	sdkReq, ok := req.(*kmsclient.UpdateKeyStatusRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleUpdateKeyStatus",
			"invalid request type, expected *kms.UpdateKeyStatusRequest")
	}
	return h.client.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{
		KeyID:   sdkReq.KeyID,
		Version: uint64(sdkReq.Version),
		Status:  sdkStatusToService(sdkReq.Status),
	})
}

func (h *kmsHandler) handleListKeyVersions(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.ListKeyVersionsRequest); ok {
		return h.client.KMSListKeyVersions(ctx, r)
	}
	keyID, ok := stringFieldByName(req, "KeyID")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleListKeyVersions",
			"invalid request type, expected struct with KeyID field")
	}
	return h.client.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{KeyID: keyID})
}

func (h *kmsHandler) handleGetPublicKey(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.GetPublicKeyRequest); ok {
		return h.client.KMSGetPublicKey(ctx, r)
	}
	keyID, ok := stringFieldByName(req, "KeyID")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleGetPublicKey",
			"invalid request type, expected struct with KeyID field")
	}
	return h.client.KMSGetPublicKey(ctx, &service.GetPublicKeyRequest{KeyID: keyID})
}

func (h *kmsHandler) handleRevokeKey(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.RevokeKeyRequest); ok {
		return h.client.KMSRevokeKey(ctx, r)
	}
	keyID, ok := stringFieldByName(req, "KeyID")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleRevokeKey",
			"invalid request type, expected struct with KeyID field")
	}
	// Reason is optional on the SDK wire request; absent field maps to "".
	reason, _ := stringFieldByName(req, "Reason")
	return h.client.KMSRevokeKey(ctx, &service.RevokeKeyRequest{KeyID: keyID, Reason: reason})
}

func (h *kmsHandler) handleGetKeyMetadata(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.GetKeyMetadataRequest); ok {
		return h.client.KMSGetKeyMetadata(ctx, r)
	}
	keyID, ok := stringFieldByName(req, "KeyID")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleGetKeyMetadata",
			"invalid request type, expected struct with KeyID field")
	}
	return h.client.KMSGetKeyMetadata(ctx, &service.GetKeyMetadataRequest{KeyID: keyID})
}

func (h *kmsHandler) handleUpdateKeyMetadata(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.UpdateKeyMetadataRequest); ok {
		return h.client.KMSUpdateKeyMetadata(ctx, r)
	}
	sdkReq, ok := req.(*kmsclient.UpdateKeyMetadataRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleUpdateKeyMetadata",
			"invalid request type, expected *kms.UpdateKeyMetadataRequest")
	}
	return h.client.KMSUpdateKeyMetadata(ctx, sdkUpdateKeyMetadataToService(sdkReq))
}

func (h *kmsHandler) handleSetKeyPolicy(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.SetKeyPolicyRequest); ok {
		return h.client.KMSSetKeyPolicy(ctx, r)
	}
	sdkReq, ok := req.(*kmsclient.SetKeyPolicyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleSetKeyPolicy",
			"invalid request type, expected *kms.SetKeyPolicyRequest")
	}
	policy, err := sdkKeyPolicyToService(sdkReq.Policy)
	if err != nil {
		return nil, err
	}
	return h.client.KMSSetKeyPolicy(ctx, &service.SetKeyPolicyRequest{
		KeyID:  sdkReq.KeyID,
		Policy: policy,
	})
}

func (h *kmsHandler) handleGetKeyPolicy(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.GetKeyPolicyRequest); ok {
		return h.client.KMSGetKeyPolicy(ctx, r)
	}
	keyID, ok := stringFieldByName(req, "KeyID")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleGetKeyPolicy",
			"invalid request type, expected struct with KeyID field")
	}
	return h.client.KMSGetKeyPolicy(ctx, &service.GetKeyPolicyRequest{KeyID: keyID})
}

func (h *kmsHandler) handleSetKeyRotationPolicy(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.SetKeyRotationPolicyRequest); ok {
		return h.client.KMSSetKeyRotationPolicy(ctx, r)
	}
	// The SDK client sends the unexported flat setKeyRotationPolicyRequest
	// (KeyID, AutoRotate, IntervalSeconds, RetainVersions, Enabled); match it
	// structurally, exactly like the other unexported request types.
	keyID, ok := stringFieldByName(req, "KeyID")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleSetKeyRotationPolicy",
			"invalid request type, expected struct with KeyID field")
	}
	autoRotate, _ := boolFieldByName(req, "AutoRotate")
	enabled, _ := boolFieldByName(req, "Enabled")
	intervalSeconds, _ := int64FieldByName(req, "IntervalSeconds")
	retainVersions, _ := int64FieldByName(req, "RetainVersions")
	return h.client.KMSSetKeyRotationPolicy(ctx, &service.SetKeyRotationPolicyRequest{
		KeyID: keyID,
		Policy: &service.RotationPolicy{
			Enabled:        enabled,
			AutoRotate:     autoRotate,
			Interval:       time.Duration(intervalSeconds) * time.Second,
			RetainVersions: int(retainVersions),
		},
	})
}

func (h *kmsHandler) handleGetKeyRotationPolicy(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.GetKeyRotationPolicyRequest); ok {
		return h.client.KMSGetKeyRotationPolicy(ctx, r)
	}
	keyID, ok := stringFieldByName(req, "KeyID")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleGetKeyRotationPolicy",
			"invalid request type, expected struct with KeyID field")
	}
	return h.client.KMSGetKeyRotationPolicy(ctx, &service.GetKeyRotationPolicyRequest{KeyID: keyID})
}

func (h *kmsHandler) handleDeleteKeyRotationPolicy(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.DeleteKeyRotationPolicyRequest); ok {
		return h.client.KMSDeleteKeyRotationPolicy(ctx, r)
	}
	keyID, ok := stringFieldByName(req, "KeyID")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleDeleteKeyRotationPolicy",
			"invalid request type, expected struct with KeyID field")
	}
	return h.client.KMSDeleteKeyRotationPolicy(ctx, &service.DeleteKeyRotationPolicyRequest{KeyID: keyID})
}

func (h *kmsHandler) handleExportKey(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.ExportKeyRequest); ok {
		return h.client.KMSExportKey(ctx, r)
	}
	sdkReq, ok := req.(*kmsclient.ExportKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleExportKey",
			"invalid request type, expected *kms.ExportKeyRequest")
	}
	return h.client.KMSExportKey(ctx, &service.ExportKeyRequest{
		KeyID:             sdkReq.KeyID,
		WrappingKeyID:     sdkReq.WrappingKeyID,
		WrappingAlgorithm: sdkWrappingAlgToService(sdkReq.WrappingAlgorithm),
		Format:            sdkExportFormatToService(sdkReq.Format),
		Version:           uint64(sdkReq.Version),
	})
}

func (h *kmsHandler) handleUnwrapKey(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.UnwrapKeyRequest); ok {
		return h.client.KMSUnwrapKey(ctx, r)
	}
	sdkReq, ok := req.(*kmsclient.UnwrapKeyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleUnwrapKey",
			"invalid request type, expected *kms.UnwrapKeyRequest")
	}
	return h.client.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{
		KeyID:             sdkReq.KeyID,
		WrappingKeyID:     sdkReq.WrappingKeyID,
		WrappingAlgorithm: sdkWrappingAlgToService(sdkReq.WrappingAlgorithm),
		WrappedKey:        sdkReq.WrappedMaterial,
		Algorithm:         sdkAlgToServiceString(sdkReq.Algorithm),
		Name:              sdkReq.Name,
		SourceKeyID:       sdkReq.SourceKeyID,
		SourceVersion:     uint64(sdkReq.SourceVersion),
	})
}

func (h *kmsHandler) handleGetSupportedAlgorithms(ctx context.Context, _ interface{}) (interface{}, error) {
	algs, err := h.client.KMSGetSupportedAlgorithms(ctx)
	if err != nil {
		return nil, err
	}
	// The KMS SDK client accepts a []string response directly.
	return algs, nil
}

func (h *kmsHandler) handleBackendStatus(ctx context.Context, _ interface{}) (interface{}, error) {
	return h.client.KMSBackendStatus(ctx)
}

// =============================================================================
// DEKManagement handlers
// =============================================================================

func (h *kmsHandler) handleCreateDEK(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.CreateDEKRequest); ok {
		return h.client.KMSCreateDEK(ctx, r)
	}
	sdkReq, ok := req.(*dekclient.CreateDEKRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleCreateDEK",
			"invalid request type, expected *dek.CreateDEKRequest")
	}
	return h.client.KMSCreateDEK(ctx, &service.CreateDEKRequest{
		Path:           sdkReq.Path,
		Algorithm:      stringToServiceKeyAlgorithm(sdkReq.Algorithm),
		Description:    sdkReq.Description,
		MaxOperations:  sdkReq.MaxOperations,
		MaxBytes:       sdkReq.MaxBytes,
		WarningPercent: sdkReq.WarningPercent,
	})
}

func (h *kmsHandler) handleGetDEK(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.GetDEKRequest); ok {
		return h.client.KMSGetDEK(ctx, r)
	}
	path, ok := stringFieldByName(req, "Path")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleGetDEK",
			"invalid request type, expected struct with Path field")
	}
	return h.client.KMSGetDEK(ctx, &service.GetDEKRequest{Path: path})
}

func (h *kmsHandler) handleListDEKs(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.ListDEKsRequest); ok {
		return h.client.KMSListDEKs(ctx, r)
	}
	sdkReq, ok := req.(*dekclient.ListDEKsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleListDEKs",
			"invalid request type, expected *dek.ListDEKsRequest")
	}
	return h.client.KMSListDEKs(ctx, &service.ListDEKsRequest{
		Prefix:    sdkReq.Prefix,
		Recursive: sdkReq.Recursive,
		Limit:     sdkReq.Limit,
	})
}

func (h *kmsHandler) handleRotateDEK(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.RotateDEKRequest); ok {
		return h.client.KMSRotateDEK(ctx, r)
	}
	sdkReq, ok := req.(*dekclient.RotateDEKRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleRotateDEK",
			"invalid request type, expected *dek.RotateDEKRequest")
	}
	return h.client.KMSRotateDEK(ctx, &service.RotateDEKRequest{
		Path:       sdkReq.Path,
		Background: sdkReq.Background,
	})
}

func (h *kmsHandler) handleDeleteDEK(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.DeleteDEKRequest); ok {
		return h.client.KMSDeleteDEK(ctx, r)
	}
	sdkReq, ok := req.(*dekclient.DeleteDEKRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleDeleteDEK",
			"invalid request type, expected *dek.DeleteDEKRequest")
	}
	return h.client.KMSDeleteDEK(ctx, &service.DeleteDEKRequest{
		Path:      sdkReq.Path,
		Force:     sdkReq.Force,
		MigrateTo: sdkReq.MigrateTo,
	})
}

func (h *kmsHandler) handleGetDEKStatus(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.GetDEKStatusRequest); ok {
		return h.client.KMSGetDEKStatus(ctx, r)
	}
	path, ok := stringFieldByName(req, "Path")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleGetDEKStatus",
			"invalid request type, expected struct with Path field")
	}
	return h.client.KMSGetDEKStatus(ctx, &service.GetDEKStatusRequest{Path: path})
}

func (h *kmsHandler) handleSetDefaultDEK(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.SetDefaultDEKRequest); ok {
		return h.client.KMSSetDefaultDEK(ctx, r)
	}
	path, ok := stringFieldByName(req, "Path")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleSetDefaultDEK",
			"invalid request type, expected struct with Path field")
	}
	return h.client.KMSSetDefaultDEK(ctx, &service.SetDefaultDEKRequest{Path: path})
}

func (h *kmsHandler) handleSetDEKSafety(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.SetDEKSafetyRequest); ok {
		return h.client.KMSSetDEKSafety(ctx, r)
	}
	sdkReq, ok := req.(*dekclient.SetDEKSafetyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleSetDEKSafety",
			"invalid request type, expected *dek.SetDEKSafetyRequest")
	}
	return h.client.KMSSetDEKSafety(ctx, &service.SetDEKSafetyRequest{
		Path:           sdkReq.Path,
		MaxOperations:  sdkReq.MaxOperations,
		MaxBytes:       sdkReq.MaxBytes,
		WarningPercent: sdkReq.WarningPercent,
	})
}

func (h *kmsHandler) handleClearDEKSafety(ctx context.Context, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.ClearDEKSafetyRequest); ok {
		return h.client.KMSClearDEKSafety(ctx, r)
	}
	path, ok := stringFieldByName(req, "Path")
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleClearDEKSafety",
			"invalid request type, expected struct with Path field")
	}
	return h.client.KMSClearDEKSafety(ctx, &service.ClearDEKSafetyRequest{Path: path})
}

// handleStartReencryption accepts the service request the SDK DEK client
// already builds for this verb.
func (h *kmsHandler) handleStartReencryption(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.StartReencryptionRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleStartReencryption",
			"invalid request type, expected *service.StartReencryptionRequest")
	}
	return h.client.KMSStartReencryption(ctx, r)
}

// handleGetReencryptionProgress accepts the service request the SDK DEK
// client already builds for this verb.
func (h *kmsHandler) handleGetReencryptionProgress(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GetReencryptionProgressRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kmsHandler.handleGetReencryptionProgress",
			"invalid request type, expected *service.GetReencryptionProgressRequest")
	}
	resp, err := h.client.KMSGetReencryptionProgress(ctx, r)
	if err != nil {
		return nil, err
	}
	// The proto ReencryptionProgress has no status field: the server sends
	// the progress status as the response's top-level status.
	if resp != nil && resp.Progress != nil && resp.Progress.Status == "" {
		resp.Progress.Status = resp.Status
	}
	return resp, nil
}

// =============================================================================
// SDK -> service request conversion helpers
// =============================================================================

// stringFieldByName extracts a string field from any struct (or pointer to a
// struct) via reflection. The KMS/DEK SDK clients send unexported request
// types (getKeyRequest, deleteKeyRequest, getDEKRequest, ...) carrying a single
// identifier, so a typed assertion is impossible from outside their package; we
// match structurally instead.
func stringFieldByName(req interface{}, name string) (string, bool) {
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", false
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.String {
		return "", false
	}
	return f.String(), true
}

// boolFieldByName extracts a bool field from any struct (or pointer to a
// struct) via reflection, mirroring stringFieldByName for the unexported SDK
// request types.
func boolFieldByName(req interface{}, name string) (bool, bool) {
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return false, false
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.Bool {
		return false, false
	}
	return f.Bool(), true
}

// int64FieldByName extracts a signed integer field from any struct (or pointer
// to a struct) via reflection, mirroring stringFieldByName for the unexported
// SDK request types.
func int64FieldByName(req interface{}, name string) (int64, bool) {
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, false
	}
	f := v.FieldByName(name)
	if !f.IsValid() {
		return 0, false
	}
	switch f.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return f.Int(), true
	default:
		return 0, false
	}
}

// sdkUpdateKeyMetadataToService converts the exported SDK metadata-update
// request into the canonical service request. Empty Name/Description mean
// "no change" in the SDK shape and map to nil pointers.
func sdkUpdateKeyMetadataToService(sdkReq *kmsclient.UpdateKeyMetadataRequest) *service.UpdateKeyMetadataRequest {
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
	return svcReq
}

// sdkKeyPolicyToService converts the exported SDK KeyPolicy into the canonical
// service KeyPolicy. Zero ExpiresAt/MaxUsageCount map to nil (no expiry /
// no usage limit).
func sdkKeyPolicyToService(p *kmsclient.KeyPolicy) (*service.KeyPolicy, error) {
	if p == nil {
		return nil, nil
	}
	if p.Version < 0 {
		return nil, errors.NewInvalidInput("kmsHandler.handleSetKeyPolicy", "policy version must not be negative")
	}
	svcPolicy := &service.KeyPolicy{
		KeyID:             p.KeyID,
		Version:           uint64(p.Version),
		AllowedOperations: sdkUsagesToService(p.AllowedOperations),
		AllowedPrincipals: p.AllowedPrincipals,
		DeniedPrincipals:  p.DeniedPrincipals,
		RotationRequired:  p.RotationRequired,
		RequireMFA:        p.RequireMFA,
		UpdatedAt:         p.UpdatedAt,
	}
	if !p.ExpiresAt.IsZero() {
		expiresAt := p.ExpiresAt
		svcPolicy.ExpiresAt = &expiresAt
	}
	if p.MaxUsageCount > 0 {
		maxUsage := p.MaxUsageCount
		svcPolicy.MaxUsageCount = &maxUsage
	}
	return svcPolicy, nil
}

// sdkAlgToKeyAttributes converts an SDK KeyAlgorithm to xkmssdk.KeyAttributes
// for the service-layer key requests. Symmetric algorithms populate the
// SymmetricAlgorithm field; asymmetric algorithms populate KeyAlgorithm (and
// RSAAttributes/ECCAttributes for size/curve).
func sdkAlgToKeyAttributes(alg kmsclient.KeyAlgorithm) *xkmssdk.KeyAttributes {
	attrs := &xkmssdk.KeyAttributes{}
	switch alg {
	case kmsclient.AlgorithmAES256GCM:
		attrs.SymmetricAlgorithm = xkmssdk.SymmetricAES256GCM
	case kmsclient.AlgorithmChaCha20Poly1305:
		attrs.SymmetricAlgorithm = xkmssdk.SymmetricChaCha20Poly1305
	case kmsclient.AlgorithmRSA2048:
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &xkmssdk.RSAAttributes{KeySize: 2048}
	case kmsclient.AlgorithmRSA4096:
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &xkmssdk.RSAAttributes{KeySize: 4096}
	case kmsclient.AlgorithmECDSAP256:
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.ECCAttributes = &xkmssdk.ECCAttributes{Curve: elliptic.P256()}
	case kmsclient.AlgorithmECDSAP384:
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.ECCAttributes = &xkmssdk.ECCAttributes{Curve: elliptic.P384()}
	case kmsclient.AlgorithmED25519:
		attrs.KeyAlgorithm = x509.Ed25519
	default:
		attrs.SymmetricAlgorithm = xkmssdk.SymmetricAES256GCM
	}
	return attrs
}

// sdkUsagesToService converts a slice of SDK KeyUsage values to service-layer
// KeyUsage values.
func sdkUsagesToService(usages []kmsclient.KeyUsage) []service.KeyUsage {
	if len(usages) == 0 {
		return nil
	}
	out := make([]service.KeyUsage, len(usages))
	for i, u := range usages {
		out[i] = sdkUsageToService(u)
	}
	return out
}

// sdkUsageToService converts a single SDK KeyUsage to a service-layer KeyUsage.
func sdkUsageToService(usage kmsclient.KeyUsage) service.KeyUsage {
	switch usage {
	case kmsclient.KeyUsageEncrypt:
		return service.KeyUsageEncrypt
	case kmsclient.KeyUsageDecrypt:
		return service.KeyUsageDecrypt
	case kmsclient.KeyUsageSign:
		return service.KeyUsageSign
	case kmsclient.KeyUsageVerify:
		return service.KeyUsageVerify
	case kmsclient.KeyUsageWrap, kmsclient.KeyUsageUnwrap:
		return service.KeyUsageWrapKey
	default:
		return service.KeyUsageEncrypt
	}
}

// sdkStatusToService converts an SDK KeyStatus to a service-layer KeyStatus.
func sdkStatusToService(status kmsclient.KeyStatus) service.KeyStatus {
	switch status {
	case kmsclient.KeyStatusEnabled:
		return service.KeyStatusEnabled
	case kmsclient.KeyStatusDisabled:
		return service.KeyStatusDisabled
	case kmsclient.KeyStatusPendingDeletion:
		return service.KeyStatusPendingDeletion
	case kmsclient.KeyStatusDeleted:
		return service.KeyStatusDeleted
	case kmsclient.KeyStatusRotating:
		return service.KeyStatusRotating
	case kmsclient.KeyStatusCompromised:
		return service.KeyStatusCompromised
	default:
		return service.KeyStatusEnabled
	}
}

// sdkAlgToServiceString converts an SDK KeyAlgorithm to its canonical service
// algorithm string (used by UnwrapKey, which carries a string algorithm).
func sdkAlgToServiceString(alg kmsclient.KeyAlgorithm) string {
	return alg.String()
}

// sdkWrappingAlgToService converts an SDK WrappingAlgorithm to the service
// wrapping-algorithm string understood by the pkg/api/grpc converters.
func sdkWrappingAlgToService(alg kmsclient.WrappingAlgorithm) string {
	switch alg {
	case kmsclient.WrapAlgAESKW:
		return "AES-KW"
	case kmsclient.WrapAlgAESKWP:
		return "AES-KWP"
	case kmsclient.WrapAlgAESGCM:
		return "AES-GCM"
	case kmsclient.WrapAlgRSAOAEPSHA256:
		return "RSA-OAEP-SHA256"
	case kmsclient.WrapAlgRSAOAEPSHA384:
		return "RSA-OAEP-SHA384"
	case kmsclient.WrapAlgRSAOAEPSHA512:
		return "RSA-OAEP-SHA512"
	case kmsclient.WrapAlgECDHESA256KW:
		return "ECDH-ES+A256KW"
	default:
		return ""
	}
}

// sdkExportFormatToService converts an SDK ExportFormat to the service
// export-format string understood by the pkg/api/grpc converters.
func sdkExportFormatToService(format kmsclient.ExportFormat) string {
	switch format {
	case kmsclient.ExportFormatRaw:
		return "raw"
	case kmsclient.ExportFormatJWK:
		return "jwk"
	case kmsclient.ExportFormatPEM:
		return "pem"
	default:
		return ""
	}
}

// stringToServiceKeyAlgorithm converts the DEK SDK's string algorithm name into
// the service-layer KeyAlgorithm enum.
func stringToServiceKeyAlgorithm(s string) service.KeyAlgorithm {
	switch s {
	case "AES-256-GCM", "AES256GCM":
		return service.KeyAlgAES256GCM
	case "CHACHA20-POLY1305", "CHACHA20POLY1305":
		return service.KeyAlgChaCha20Poly1305
	case "RSA-2048", "RSA2048":
		return service.KeyAlgRSA2048
	case "RSA-4096", "RSA4096":
		return service.KeyAlgRSA4096
	case "ECDSA-P256", "ECDSAP256":
		return service.KeyAlgECDSAP256
	case "ECDSA-P384", "ECDSAP384":
		return service.KeyAlgECDSAP384
	case "ED25519":
		return service.KeyAlgEd25519
	default:
		return service.KeyAlgAES256GCM
	}
}
