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

package embedded

import (
	"context"
	"crypto/elliptic"
	"crypto/x509"
	"reflect"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// KMS Handlers
// =============================================================================

func handleKMSGenerateKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kmsSvc := svc.KMS()
	if kmsSvc == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}

	// Accept service type directly (future direct service callers)
	if r, ok := req.(*service.GenerateKeyRequest); ok {
		return kmsSvc.GenerateKey(ctx, r)
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*kmsclient.GenerateKeyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.KMS", "GenerateKey",
			"*kms.GenerateKeyRequest",
			typeNameOf(req),
		)
	}

	svcReq := &service.GenerateKeyRequest{
		Name:               sdkReq.Name,
		Usage:              convertSDKUsagesToService(sdkReq.Usage),
		KeyAttributes:      sdkAlgToKeyAttributes(sdkReq.Algorithm),
		Exportable:         sdkReq.Exportable,
		DeletionProtection: sdkReq.DeletionProtection,
	}
	return kmsSvc.GenerateKey(ctx, svcReq)
}

// sdkAlgToKeyAttributes converts an SDK KeyAlgorithm to xkmssdk.KeyAttributes
// for the service-layer GenerateKeyRequest. Symmetric algorithms populate the
// SymmetricAlgorithm field; asymmetric algorithms populate KeyAlgorithm (and
// RSAAttributes for RSA key sizes).
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

// convertSDKUsagesToService converts a slice of SDK KeyUsage values to
// the corresponding service-layer KeyUsage values.
func convertSDKUsagesToService(usages []kmsclient.KeyUsage) []service.KeyUsage {
	if len(usages) == 0 {
		return nil
	}
	svcUsages := make([]service.KeyUsage, len(usages))
	for i, u := range usages {
		svcUsages[i] = convertSDKUsageToService(u)
	}
	return svcUsages
}

// convertSDKUsageToService converts a single SDK KeyUsage value to the
// corresponding service-layer KeyUsage value.
func convertSDKUsageToService(usage kmsclient.KeyUsage) service.KeyUsage {
	switch usage {
	case kmsclient.KeyUsageEncrypt:
		return service.KeyUsageEncrypt
	case kmsclient.KeyUsageDecrypt:
		return service.KeyUsageDecrypt
	case kmsclient.KeyUsageSign:
		return service.KeyUsageSign
	case kmsclient.KeyUsageVerify:
		return service.KeyUsageVerify
	case kmsclient.KeyUsageWrap:
		return service.KeyUsageWrapKey
	case kmsclient.KeyUsageUnwrap:
		return service.KeyUsageWrapKey
	default:
		return service.KeyUsageEncrypt
	}
}

func handleKMSImportKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.ImportKeyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "ImportKey", "*service.ImportKeyRequest", typeNameOf(req))
	}
	return kms.ImportKey(ctx, r)
}

func handleKMSGetKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}

	// Accept service type directly
	if r, ok := req.(*service.GetKeyRequest); ok {
		return kms.GetKey(ctx, r)
	}

	// Accept SDK client type via reflection (unexported getKeyRequest)
	reqVal := reflect.ValueOf(req)
	if reqVal.Kind() == reflect.Ptr {
		reqVal = reqVal.Elem()
	}

	if reqVal.Kind() != reflect.Struct {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "GetKey", "*service.GetKeyRequest or struct{KeyID string}", typeNameOf(req))
	}

	keyIDField := reqVal.FieldByName("KeyID")
	if !keyIDField.IsValid() {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "GetKey", "struct{KeyID string}", typeNameOf(req))
	}

	return kms.GetKey(ctx, &service.GetKeyRequest{KeyID: keyIDField.String()})
}

func handleKMSGetPublicKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}

	// Accept service type directly
	if r, ok := req.(*service.GetPublicKeyRequest); ok {
		return kms.GetPublicKey(ctx, r)
	}

	// Accept SDK client type via reflection (unexported getPublicKeyRequest)
	reqVal := reflect.ValueOf(req)
	if reqVal.Kind() == reflect.Ptr {
		reqVal = reqVal.Elem()
	}

	if reqVal.Kind() != reflect.Struct {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "GetPublicKey", "*service.GetPublicKeyRequest or struct{KeyID string}", typeNameOf(req))
	}

	keyIDField := reqVal.FieldByName("KeyID")
	if !keyIDField.IsValid() {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "GetPublicKey", "struct{KeyID string}", typeNameOf(req))
	}

	return kms.GetPublicKey(ctx, &service.GetPublicKeyRequest{KeyID: keyIDField.String()})
}

func handleKMSListKeys(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}

	// Accept service type directly
	if r, ok := req.(*service.ListKeysRequest); ok {
		return kms.ListKeys(ctx, r)
	}

	// Accept SDK client type (unexported listKeysRequest has no fields)
	// Validate that the request is at least a struct type
	reqVal := reflect.ValueOf(req)
	if reqVal.Kind() == reflect.Ptr {
		reqVal = reqVal.Elem()
	}

	if reqVal.Kind() != reflect.Struct {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "ListKeys", "*service.ListKeysRequest or struct{}", typeNameOf(req))
	}

	return kms.ListKeys(ctx, &service.ListKeysRequest{})
}

func handleKMSRotateKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}

	// Accept service type directly
	if r, ok := req.(*service.RotateKeyRequest); ok {
		return kms.RotateKey(ctx, r)
	}

	// Accept SDK client type via reflection (unexported rotateKeyRequest)
	reqVal := reflect.ValueOf(req)
	if reqVal.Kind() == reflect.Ptr {
		reqVal = reqVal.Elem()
	}

	if reqVal.Kind() != reflect.Struct {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "RotateKey", "*service.RotateKeyRequest or struct{KeyID string}", typeNameOf(req))
	}

	keyIDField := reqVal.FieldByName("KeyID")
	if !keyIDField.IsValid() {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "RotateKey", "struct{KeyID string}", typeNameOf(req))
	}

	return kms.RotateKey(ctx, &service.RotateKeyRequest{KeyID: keyIDField.String()})
}

func handleKMSRevokeKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}

	// Accept service type directly
	if r, ok := req.(*service.RevokeKeyRequest); ok {
		return kms.RevokeKey(ctx, r)
	}

	// Accept SDK client type via reflection (unexported revokeKeyRequest with
	// KeyID and optional Reason fields)
	keyID, ok := kmsStringField(req, "KeyID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "RevokeKey", "*service.RevokeKeyRequest or struct{KeyID, Reason string}", typeNameOf(req))
	}
	reason, _ := kmsStringField(req, "Reason")
	return kms.RevokeKey(ctx, &service.RevokeKeyRequest{KeyID: keyID, Reason: reason})
}

func handleKMSDeleteKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}

	// Accept service type directly
	if r, ok := req.(*service.DeleteKeyRequest); ok {
		return kms.DeleteKey(ctx, r)
	}

	// Accept SDK client type via reflection (unexported deleteKeyRequest)
	reqVal := reflect.ValueOf(req)
	if reqVal.Kind() == reflect.Ptr {
		reqVal = reqVal.Elem()
	}

	if reqVal.Kind() != reflect.Struct {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "DeleteKey", "*service.DeleteKeyRequest or struct{KeyID string}", typeNameOf(req))
	}

	keyIDField := reqVal.FieldByName("KeyID")
	if !keyIDField.IsValid() {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "DeleteKey", "struct{KeyID string}", typeNameOf(req))
	}

	return kms.DeleteKey(ctx, &service.DeleteKeyRequest{KeyID: keyIDField.String()})
}

func handleKMSGetKeyMetadata(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.GetKeyMetadataRequest); ok {
		return kms.GetKeyMetadata(ctx, r)
	}
	keyID, ok := kmsStringField(req, "KeyID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "GetKeyMetadata", "*service.GetKeyMetadataRequest or struct{KeyID string}", typeNameOf(req))
	}
	return kms.GetKeyMetadata(ctx, &service.GetKeyMetadataRequest{KeyID: keyID})
}

func handleKMSUpdateKeyMetadata(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.UpdateKeyMetadataRequest); ok {
		return kms.UpdateKeyMetadata(ctx, r)
	}
	sdkReq, ok := req.(*kmsclient.UpdateKeyMetadataRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "UpdateKeyMetadata", "*service.UpdateKeyMetadataRequest or *kms.UpdateKeyMetadataRequest", typeNameOf(req))
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
	return kms.UpdateKeyMetadata(ctx, svcReq)
}

func handleKMSUpdateKeyStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.UpdateKeyStatusRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "UpdateKeyStatus", "*service.UpdateKeyStatusRequest", typeNameOf(req))
	}
	return kms.UpdateKeyStatus(ctx, r)
}

func handleKMSListKeyVersions(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.ListKeyVersionsRequest); ok {
		return kms.ListKeyVersions(ctx, r)
	}
	keyID, ok := kmsStringField(req, "KeyID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "ListKeyVersions", "*service.ListKeyVersionsRequest or struct{KeyID string}", typeNameOf(req))
	}
	return kms.ListKeyVersions(ctx, &service.ListKeyVersionsRequest{KeyID: keyID})
}

func handleKMSSetKeyPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.SetKeyPolicyRequest); ok {
		return kms.SetKeyPolicy(ctx, r)
	}
	sdkReq, ok := req.(*kmsclient.SetKeyPolicyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "SetKeyPolicy", "*service.SetKeyPolicyRequest or *kms.SetKeyPolicyRequest", typeNameOf(req))
	}
	policy, err := sdkKeyPolicyToService(sdkReq.KeyID, sdkReq.Policy)
	if err != nil {
		return nil, err
	}
	return kms.SetKeyPolicy(ctx, &service.SetKeyPolicyRequest{
		KeyID:  sdkReq.KeyID,
		Policy: policy,
	})
}

func handleKMSGetKeyPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.GetKeyPolicyRequest); ok {
		return kms.GetKeyPolicy(ctx, r)
	}
	keyID, ok := kmsStringField(req, "KeyID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "GetKeyPolicy", "*service.GetKeyPolicyRequest or struct{KeyID string}", typeNameOf(req))
	}
	return kms.GetKeyPolicy(ctx, &service.GetKeyPolicyRequest{KeyID: keyID})
}

func handleKMSGetSupportedAlgorithms(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	return kms.GetSupportedAlgorithms(ctx)
}

func handleKMSBackendStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	return kms.BackendStatus(ctx)
}

func handleKMSBackendTest(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	return kms.BackendTest(ctx)
}

func handleKMSExportKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.ExportKeyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "ExportKey", "*service.ExportKeyRequest", typeNameOf(req))
	}
	return kms.ExportKey(ctx, r)
}

func handleKMSUnwrapKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.UnwrapKeyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "UnwrapKey", "*service.UnwrapKeyRequest", typeNameOf(req))
	}
	return kms.UnwrapKey(ctx, r)
}

func handleKMSSetKeySafety(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.SetKeySafetyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "SetKeySafety", "*service.SetKeySafetyRequest", typeNameOf(req))
	}
	return kms.SetKeySafety(ctx, r)
}

func handleKMSClearKeySafety(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.ClearKeySafetyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "ClearKeySafety", "*service.ClearKeySafetyRequest", typeNameOf(req))
	}
	return kms.ClearKeySafety(ctx, r)
}

func handleKMSSafetyCheck(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.SafetyCheckRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "SafetyCheck", "*service.SafetyCheckRequest", typeNameOf(req))
	}
	return kms.SafetyCheck(ctx, r)
}

func handleKMSSafetyReport(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.SafetyReportRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "SafetyReport", "*service.SafetyReportRequest", typeNameOf(req))
	}
	return kms.SafetyReport(ctx, r)
}

// =============================================================================
// DEK Handlers
// =============================================================================

func handleKMSCreateDEK(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.CreateDEKRequest); ok {
		return kms.CreateDEK(ctx, r)
	}
	if r, ok := req.(*dekclient.CreateDEKRequest); ok {
		svcReq := &service.CreateDEKRequest{
			Path:           r.Path,
			Algorithm:      stringToKeyAlgorithm(r.Algorithm),
			Description:    r.Description,
			MaxOperations:  r.MaxOperations,
			MaxBytes:       r.MaxBytes,
			WarningPercent: r.WarningPercent,
		}
		return kms.CreateDEK(ctx, svcReq)
	}
	return nil, NewInvalidRequestTypeError("quicbaas.DEK", "CreateDEK",
		"*service.CreateDEKRequest or *dek.CreateDEKRequest",
		typeNameOf(req))
}

func handleKMSGetDEK(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.GetDEKRequest); ok {
		return kms.GetDEK(ctx, r)
	}
	// SDK uses unexported getDEKRequest{Path}; accept anything with a Path field.
	if path, ok := dekRequestPath(req); ok {
		return kms.GetDEK(ctx, &service.GetDEKRequest{Path: path})
	}
	return nil, NewInvalidRequestTypeError("quicbaas.DEK", "GetDEK",
		"*service.GetDEKRequest or *dek.getDEKRequest",
		typeNameOf(req))
}

func handleKMSListDEKs(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.ListDEKsRequest); ok {
		return kms.ListDEKs(ctx, r)
	}
	if r, ok := req.(*dekclient.ListDEKsRequest); ok {
		return kms.ListDEKs(ctx, &service.ListDEKsRequest{
			Prefix:    r.Prefix,
			Recursive: r.Recursive,
			Limit:     r.Limit,
		})
	}
	return nil, NewInvalidRequestTypeError("quicbaas.DEK", "ListDEKs",
		"*service.ListDEKsRequest or *dek.ListDEKsRequest",
		typeNameOf(req))
}

func handleKMSRotateDEK(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.RotateDEKRequest); ok {
		return kms.RotateDEK(ctx, r)
	}
	if r, ok := req.(*dekclient.RotateDEKRequest); ok {
		return kms.RotateDEK(ctx, &service.RotateDEKRequest{
			Path:       r.Path,
			Background: r.Background,
		})
	}
	return nil, NewInvalidRequestTypeError("quicbaas.DEK", "RotateDEK",
		"*service.RotateDEKRequest or *dek.RotateDEKRequest",
		typeNameOf(req))
}

func handleKMSDeleteDEK(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.DeleteDEKRequest); ok {
		return kms.DeleteDEK(ctx, r)
	}
	if r, ok := req.(*dekclient.DeleteDEKRequest); ok {
		return kms.DeleteDEK(ctx, &service.DeleteDEKRequest{
			Path:      r.Path,
			Force:     r.Force,
			MigrateTo: r.MigrateTo,
		})
	}
	return nil, NewInvalidRequestTypeError("quicbaas.DEK", "DeleteDEK",
		"*service.DeleteDEKRequest or *dek.DeleteDEKRequest",
		typeNameOf(req))
}

func handleKMSGetDEKStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.GetDEKStatusRequest); ok {
		return kms.GetDEKStatus(ctx, r)
	}
	if path, ok := dekRequestPath(req); ok {
		return kms.GetDEKStatus(ctx, &service.GetDEKStatusRequest{Path: path})
	}
	return nil, NewInvalidRequestTypeError("quicbaas.DEK", "GetDEKStatus",
		"*service.GetDEKStatusRequest or *dek.getDEKStatusRequest",
		typeNameOf(req))
}

// dekRequestPath extracts a Path string from any struct that has it via
// reflection. The DEK SDK uses unexported getDEKRequest / getDEKStatusRequest
// types, so a typed assertion is impossible from outside the package; we
// match structurally instead.
func dekRequestPath(req interface{}) (string, bool) {
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", false
	}
	f := v.FieldByName("Path")
	if !f.IsValid() || f.Kind() != reflect.String {
		return "", false
	}
	return f.String(), true
}

// stringToKeyAlgorithm converts the dek SDK's string algorithm name into
// the service-layer KeyAlgorithm enum used by QuicBaaS internals.
func stringToKeyAlgorithm(s string) service.KeyAlgorithm {
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
	case "":
		return service.KeyAlgAES256GCM
	}
	return service.KeyAlgAES256GCM
}

func handleKMSSetDefaultDEK(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.SetDefaultDEKRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "SetDefaultDEK", "*service.SetDefaultDEKRequest", typeNameOf(req))
	}
	return kms.SetDefaultDEK(ctx, r)
}

func handleKMSSetDEKSafety(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.SetDEKSafetyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "SetDEKSafety", "*service.SetDEKSafetyRequest", typeNameOf(req))
	}
	return kms.SetDEKSafety(ctx, r)
}

func handleKMSClearDEKSafety(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.ClearDEKSafetyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "ClearDEKSafety", "*service.ClearDEKSafetyRequest", typeNameOf(req))
	}
	return kms.ClearDEKSafety(ctx, r)
}

func handleKMSStartReencryption(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.StartReencryptionRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "StartReencryption", "*service.StartReencryptionRequest", typeNameOf(req))
	}
	return kms.StartReencryption(ctx, r)
}

func handleKMSGetReencryptionProgress(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	r, ok := req.(*service.GetReencryptionProgressRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "GetReencryptionProgress", "*service.GetReencryptionProgressRequest", typeNameOf(req))
	}
	return kms.GetReencryptionProgress(ctx, r)
}

// =============================================================================
// KMS Rotation Policy Handlers
// =============================================================================

func handleKMSSetKeyRotationPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}

	// Accept service type directly
	if r, ok := req.(*service.SetKeyRotationPolicyRequest); ok {
		return kms.SetKeyRotationPolicy(ctx, r)
	}

	// Accept the SDK client's unexported flat setKeyRotationPolicyRequest
	// (KeyID, AutoRotate, IntervalSeconds, RetainVersions, Enabled)
	keyID, ok := kmsStringField(req, "KeyID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "SetKeyRotationPolicy", "*service.SetKeyRotationPolicyRequest or flat rotation-policy struct", typeNameOf(req))
	}
	autoRotate, _ := kmsBoolField(req, "AutoRotate")
	enabled, _ := kmsBoolField(req, "Enabled")
	intervalSeconds, _ := kmsInt64Field(req, "IntervalSeconds")
	retainVersions, _ := kmsInt64Field(req, "RetainVersions")
	return kms.SetKeyRotationPolicy(ctx, &service.SetKeyRotationPolicyRequest{
		KeyID: keyID,
		Policy: &service.RotationPolicy{
			Enabled:        enabled,
			AutoRotate:     autoRotate,
			Interval:       time.Duration(intervalSeconds) * time.Second,
			RetainVersions: int(retainVersions),
		},
	})
}

func handleKMSGetKeyRotationPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.GetKeyRotationPolicyRequest); ok {
		return kms.GetKeyRotationPolicy(ctx, r)
	}
	keyID, ok := kmsStringField(req, "KeyID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "GetKeyRotationPolicy", "*service.GetKeyRotationPolicyRequest or struct{KeyID string}", typeNameOf(req))
	}
	return kms.GetKeyRotationPolicy(ctx, &service.GetKeyRotationPolicyRequest{KeyID: keyID})
}

func handleKMSDeleteKeyRotationPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	kms := svc.KMS()
	if kms == nil {
		return nil, NewServiceNotConfiguredError("KMS")
	}
	if r, ok := req.(*service.DeleteKeyRotationPolicyRequest); ok {
		return kms.DeleteKeyRotationPolicy(ctx, r)
	}
	keyID, ok := kmsStringField(req, "KeyID")
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KMS", "DeleteKeyRotationPolicy", "*service.DeleteKeyRotationPolicyRequest or struct{KeyID string}", typeNameOf(req))
	}
	return kms.DeleteKeyRotationPolicy(ctx, &service.DeleteKeyRotationPolicyRequest{KeyID: keyID})
}

// =============================================================================
// KMS reflection + conversion helpers
// =============================================================================

// kmsStringField extracts a string field from any struct (or pointer to a
// struct) via reflection. The KMS SDK client sends unexported request types,
// so a typed assertion is impossible from outside its package; we match
// structurally instead.
func kmsStringField(req interface{}, name string) (string, bool) {
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "", false
		}
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

// kmsBoolField extracts a bool field from any struct (or pointer to a struct)
// via reflection, mirroring kmsStringField.
func kmsBoolField(req interface{}, name string) (bool, bool) {
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return false, false
		}
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

// kmsInt64Field extracts a signed integer field from any struct (or pointer to
// a struct) via reflection, mirroring kmsStringField.
func kmsInt64Field(req interface{}, name string) (int64, bool) {
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return 0, false
		}
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

// sdkKeyPolicyToService converts the exported SDK KeyPolicy into the canonical
// service KeyPolicy. Zero ExpiresAt/MaxUsageCount map to nil (no expiry / no
// usage limit).
func sdkKeyPolicyToService(keyID string, p *kmsclient.KeyPolicy) (*service.KeyPolicy, error) {
	if p == nil {
		return nil, nil
	}
	if p.Version < 0 {
		return nil, errors.NewInvalidInput("embedded.kms.SetKeyPolicy", "policy version must not be negative")
	}
	svcPolicy := &service.KeyPolicy{
		KeyID:             keyID,
		Version:           uint64(p.Version),
		AllowedOperations: convertSDKUsagesToService(p.AllowedOperations),
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
