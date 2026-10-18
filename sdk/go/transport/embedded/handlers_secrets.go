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
	"encoding/json"
	"reflect"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	secrets "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// Secrets Handlers
// =============================================================================

// handleSecretsEnsure implements the Vault-style generate-on-first-access
// primitive through the embedded transport. It routes the SDK client's
// EnsureSecretRequest down to the service-layer SecretsService.EnsureSecret,
// which delegates to the concrete Manager.EnsureSecret that handles
// distributed locking + spec-match + persistence.
//
// This replaces the old autogen:// pattern where every boot generated
// different random bytes. Now first access on a path generates-and-stores,
// and every subsequent access returns the same stable bytes.
func handleSecretsEnsure(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}

	sdkReq, ok := req.(*secrets.EnsureSecretRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Ensure", "*secrets.EnsureSecretRequest", typeNameOf(req))
	}

	svcReq := &service.EnsureSecretRequest{
		SecretID: sdkReq.Path,
		Length:   sdkReq.Length,
		Encoding: sdkReq.Encoding,
		TenantID: sdkReq.TenantID,
	}

	svcResp, svcErr := secretsSvc.EnsureSecret(ctx, svcReq)
	if svcErr != nil {
		return nil, svcErr
	}

	return &secrets.EnsureSecretResponse{
		Path:      svcResp.SecretID,
		Data:      svcResp.Data,
		Generated: svcResp.Generated,
		Version:   int(svcResp.Version),
	}, nil
}

func handleSecretsStore(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}

	// Accept SDK client type
	sdkReq, ok := req.(*secrets.StoreSecretRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Store", "*secrets.StoreSecretRequest", typeNameOf(req))
	}

	// Raw is stored verbatim; structured Data is JSON-encoded.
	dataBytes, marshalErr := sdkReq.Payload()
	if marshalErr != nil {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Store", "serializable data", "unserializable data")
	}

	// Build service-layer request
	svcReq := &service.StoreSecretRequest{
		Name:     sdkReq.Path,
		SecretID: sdkReq.Path,
		Data:     dataBytes,
		Metadata: sdkReq.Metadata,
	}

	svcResp, svcErr := secretsSvc.StoreSecret(ctx, svcReq)
	if svcErr != nil {
		return nil, svcErr
	}

	// Convert service response to SDK response
	return &secrets.StoreSecretResponse{
		Version: int(svcResp.Version),
	}, nil
}

func handleSecretsGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}

	// The SDK client sends an unexported getSecretRequest type. Since we cannot
	// type-assert unexported types from another package, we use reflection to
	// extract the fields.
	reqVal := reflect.ValueOf(req)
	if reqVal.Kind() == reflect.Ptr {
		reqVal = reqVal.Elem()
	}

	if reqVal.Kind() != reflect.Struct {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Get", "struct{Path string, Version int}", typeNameOf(req))
	}

	pathField := reqVal.FieldByName("Path")
	versionField := reqVal.FieldByName("Version")

	if !pathField.IsValid() || !versionField.IsValid() {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Get", "struct{Path string, Version int}", typeNameOf(req))
	}

	path := pathField.String()
	version := int(versionField.Int())

	// Build service-layer request
	svcReq := &service.GetSecretRequest{
		SecretID: path,
		Version:  uint64(version),
	}

	svcResp, svcErr := secretsSvc.GetSecret(ctx, svcReq)
	if svcErr != nil {
		return nil, svcErr
	}

	// Convert Data bytes back to map[string]interface{}
	dataMap := make(map[string]interface{})
	if len(svcResp.Data) > 0 {
		if unmarshalErr := json.Unmarshal(svcResp.Data, &dataMap); unmarshalErr != nil {
			// If not JSON, store raw bytes under "data" key
			dataMap["data"] = string(svcResp.Data)
		}
	}

	// Convert service response to SDK response
	return &secrets.Secret{
		Path:      svcResp.SecretID,
		Data:      dataMap,
		Metadata:  svcResp.Metadata,
		Version:   int(svcResp.Version),
		CreatedAt: svcResp.CreatedAt,
	}, nil
}

// handleSecretsGetData backs Client.GetWithBytes. It returns the service
// response untouched, so the stored payload reaches the client as the []byte
// the secrets service produced — never decoded into a map of strings — and the
// client, which takes ownership of it, wipes it. The service returns a fresh
// copy per call (the backends deep-copy), so wiping it cannot disturb stored
// state.
func handleSecretsGetData(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}

	// The SDK client sends the same unexported getSecretRequest as Get.
	reqVal := reflect.ValueOf(req)
	if reqVal.Kind() == reflect.Ptr {
		reqVal = reqVal.Elem()
	}
	if reqVal.Kind() != reflect.Struct {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "GetData", "struct{Path string, Version int}", typeNameOf(req))
	}
	pathField := reqVal.FieldByName("Path")
	versionField := reqVal.FieldByName("Version")
	if !pathField.IsValid() || pathField.Kind() != reflect.String ||
		!versionField.IsValid() || versionField.Kind() != reflect.Int {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "GetData", "struct{Path string, Version int}", typeNameOf(req))
	}
	version := versionField.Int()
	if version < 0 {
		version = 0
	}

	svcResp, err := secretsSvc.GetSecret(ctx, &service.GetSecretRequest{
		SecretID: pathField.String(),
		Version:  uint64(version),
	})
	if err != nil {
		return nil, err
	}
	if svcResp == nil {
		// A typed nil would reach the client as a non-nil interface.
		return nil, nil
	}
	return svcResp, nil
}

func handleSecretsList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}

	// The SDK client sends an unexported listSecretsRequest type. Use reflection.
	reqVal := reflect.ValueOf(req)
	if reqVal.Kind() == reflect.Ptr {
		reqVal = reqVal.Elem()
	}

	if reqVal.Kind() != reflect.Struct {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "List", "struct{Prefix string}", typeNameOf(req))
	}

	prefixField := reqVal.FieldByName("Prefix")
	if !prefixField.IsValid() {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "List", "struct{Prefix string}", typeNameOf(req))
	}

	prefix := prefixField.String()

	// Build service-layer request
	svcReq := &service.ListSecretsRequest{
		Prefix: prefix,
	}

	svcResp, svcErr := secretsSvc.ListSecrets(ctx, svcReq)
	if svcErr != nil {
		return nil, svcErr
	}

	// Convert service response to SDK response
	paths := make([]string, 0, len(svcResp.Secrets))
	for _, s := range svcResp.Secrets {
		paths = append(paths, s.SecretID)
	}

	return &secrets.ListSecretsResponse{
		Paths: paths,
	}, nil
}

func handleSecretsDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}

	// The SDK client sends an unexported deleteSecretRequest type. Use reflection.
	reqVal := reflect.ValueOf(req)
	if reqVal.Kind() == reflect.Ptr {
		reqVal = reqVal.Elem()
	}

	if reqVal.Kind() != reflect.Struct {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Delete", "struct{Path string}", typeNameOf(req))
	}

	pathField := reqVal.FieldByName("Path")
	if !pathField.IsValid() {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Delete", "struct{Path string}", typeNameOf(req))
	}

	path := pathField.String()

	// Build service-layer request
	svcReq := &service.DeleteSecretRequest{
		SecretID: path,
	}

	_, svcErr := secretsSvc.DeleteSecret(ctx, svcReq)
	if svcErr != nil {
		return nil, svcErr
	}

	// The SDK client accepts nil response for delete
	return nil, nil
}

// handleSecretsRotate routes Secrets/Rotate. The SDK client decodes the
// service response directly, as it does from every other transport.
func handleSecretsRotate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}
	if r, ok := req.(*service.RotateSecretRequest); ok {
		return secretsSvc.RotateSecret(ctx, r)
	}
	sdkReq, ok := req.(*secrets.RotateSecretRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Rotate", "*service.RotateSecretRequest or *secrets.RotateSecretRequest", typeNameOf(req))
	}
	return secretsSvc.RotateSecret(ctx, &service.RotateSecretRequest{
		SecretID: sdkReq.SecretID,
		NewData:  sdkReq.NewData,
		Metadata: sdkReq.Metadata,
	})
}

// handleSecretsUpdateStatus routes Secrets/UpdateStatus. A status the service
// model does not define is refused here rather than forwarded.
func handleSecretsUpdateStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}
	var svcReq *service.UpdateSecretStatusRequest
	switch r := req.(type) {
	case *service.UpdateSecretStatusRequest:
		svcReq = r
	case *secrets.UpdateSecretStatusRequest:
		svcReq = &service.UpdateSecretStatusRequest{
			SecretID: r.SecretID,
			Status:   service.SecretStatus(r.Status),
		}
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "UpdateStatus", "*service.UpdateSecretStatusRequest or *secrets.UpdateSecretStatusRequest", typeNameOf(req))
	}
	if !svcReq.Status.IsValid() {
		return nil, errors.NewInvalidInput("embedded.secrets.UpdateStatus", "unknown secret status: "+string(svcReq.Status))
	}
	return secretsSvc.UpdateSecretStatus(ctx, svcReq)
}

// handleSecretsSetRotationPolicy routes Secrets/SetRotationPolicy.
func handleSecretsSetRotationPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}
	if r, ok := req.(*service.SetRotationPolicyRequest); ok {
		return secretsSvc.SetRotationPolicy(ctx, r)
	}
	sdkReq, ok := req.(*secrets.SetRotationPolicyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "SetRotationPolicy", "*service.SetRotationPolicyRequest or *secrets.SetRotationPolicyRequest", typeNameOf(req))
	}
	svcReq := &service.SetRotationPolicyRequest{SecretID: sdkReq.SecretID}
	if p := sdkReq.Policy; p != nil {
		svcReq.Policy = &service.SecretRotationPolicy{
			Enabled:        p.Enabled,
			Interval:       p.Interval,
			NextRotation:   p.NextRotation,
			RetainVersions: p.RetainVersions,
			RotationLambda: p.RotationLambda,
		}
	}
	return secretsSvc.SetRotationPolicy(ctx, svcReq)
}

// handleSecretsGetRotationPolicy routes Secrets/GetRotationPolicy. The SDK
// client sends an unexported request type, so its SecretID field is read by
// reflection, as handleSecretsGet does.
func handleSecretsGetRotationPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}
	if r, ok := req.(*service.GetRotationPolicyRequest); ok {
		return secretsSvc.GetRotationPolicy(ctx, r)
	}
	secretID, ok := secretIDFromRequest(req)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "GetRotationPolicy", "struct{SecretID string}", typeNameOf(req))
	}
	return secretsSvc.GetRotationPolicy(ctx, &service.GetRotationPolicyRequest{SecretID: secretID})
}

// secretIDFromRequest reads the SecretID string field of a struct (or pointer
// to struct) request. The SDK secrets client sends unexported request types
// for verbs that take only a secret ID, so the field is read by reflection.
func secretIDFromRequest(req interface{}) (string, bool) {
	reqVal := reflect.ValueOf(req)
	if reqVal.Kind() == reflect.Ptr {
		reqVal = reqVal.Elem()
	}
	if reqVal.Kind() != reflect.Struct {
		return "", false
	}
	idField := reqVal.FieldByName("SecretID")
	if !idField.IsValid() || idField.Kind() != reflect.String {
		return "", false
	}
	return idField.String(), true
}

func handleSecretsGetVersion(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}
	if r, ok := req.(*service.GetSecretVersionRequest); ok {
		return secretsSvc.GetSecretVersion(ctx, r)
	}
	sdkReq, ok := req.(*secrets.GetSecretVersionRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "GetVersion", "*service.GetSecretVersionRequest or *secrets.GetSecretVersionRequest", typeNameOf(req))
	}
	if sdkReq.Version <= 0 {
		return nil, errors.NewInvalidInput("embedded.secrets.GetVersion", "version must be positive")
	}
	return secretsSvc.GetSecretVersion(ctx, &service.GetSecretVersionRequest{
		SecretID: sdkReq.SecretID,
		Version:  uint64(sdkReq.Version),
	})
}

func handleSecretsListVersions(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}
	if r, ok := req.(*service.ListSecretVersionsRequest); ok {
		return secretsSvc.ListSecretVersions(ctx, r)
	}
	secretID, ok := secretIDFromRequest(req)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "ListVersions", "struct{SecretID string}", typeNameOf(req))
	}
	return secretsSvc.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{SecretID: secretID})
}

func handleSecretsRollback(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	secretsSvc := svc.Secrets()
	if secretsSvc == nil {
		return nil, NewServiceNotConfiguredError("Secrets")
	}
	if r, ok := req.(*service.RollbackSecretRequest); ok {
		return secretsSvc.RollbackSecret(ctx, r)
	}
	sdkReq, ok := req.(*secrets.RollbackSecretRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Rollback", "*service.RollbackSecretRequest or *secrets.RollbackSecretRequest", typeNameOf(req))
	}
	if sdkReq.TargetVersion <= 0 {
		return nil, errors.NewInvalidInput("embedded.secrets.Rollback", "target version must be positive")
	}
	return secretsSvc.RollbackSecret(ctx, &service.RollbackSecretRequest{
		SecretID:      sdkReq.SecretID,
		TargetVersion: uint64(sdkReq.TargetVersion),
	})
}

// =============================================================================
// Health Ping Handler
// =============================================================================

func handlePing(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	// Ping is a lightweight connectivity check. Delegate to Liveness which is
	// already the simplest health check available on the service.
	_, err := svc.Liveness(ctx, &service.LivenessRequest{})
	if err != nil {
		return nil, err
	}

	// The SDK health client's Ping method only returns error, not data.
	// Returning nil is the correct response since the client accepts nil
	// at health/client.go line 85-87.
	return nil, nil
}
