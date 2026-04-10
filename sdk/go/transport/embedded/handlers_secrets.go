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
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Ensure", "*secrets.EnsureSecretRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Store", "*secrets.StoreSecretRequest", reflect.TypeOf(req).String())
	}

	// Convert SDK request Data (map[string]interface{}) to bytes
	dataBytes, marshalErr := json.Marshal(sdkReq.Data)
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
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Get", "struct{Path string, Version int}", reflect.TypeOf(req).String())
	}

	pathField := reqVal.FieldByName("Path")
	versionField := reqVal.FieldByName("Version")

	if !pathField.IsValid() || !versionField.IsValid() {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Get", "struct{Path string, Version int}", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "List", "struct{Prefix string}", reflect.TypeOf(req).String())
	}

	prefixField := reqVal.FieldByName("Prefix")
	if !prefixField.IsValid() {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "List", "struct{Prefix string}", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Delete", "struct{Path string}", reflect.TypeOf(req).String())
	}

	pathField := reqVal.FieldByName("Path")
	if !pathField.IsValid() {
		return nil, NewInvalidRequestTypeError("quicbaas.Secrets", "Delete", "struct{Path string}", reflect.TypeOf(req).String())
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
