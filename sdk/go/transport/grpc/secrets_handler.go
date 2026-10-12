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
	"encoding/json"
	"strings"

	"google.golang.org/grpc"

	apigrpc "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// secretsServicePrefix is the gRPC-style service identifier the SDK secrets
// client routes through. The server exposes it as the SecretsManagement proto
// service.
const secretsServicePrefix = "quicbaas.Secrets"

// isSecretsService reports whether svc targets the quicbaas.Secrets surface.
// The secrets SDK client sends plain Go structs that do not implement
// proto.Message, so they must be converted to the generated SecretsManagement
// proto types before calling gRPC.
func isSecretsService(svc string) bool {
	return strings.HasPrefix(svc, secretsServicePrefix)
}

// secretsHandler converts between the SDK secrets client structs and the
// generated SecretsManagement proto types. It delegates the struct<->protobuf
// conversion and RPC to the pkg/api/grpc Client, which wraps the transport's
// existing connection via NewClientWithConn so no second connection is dialed
// and no conversion logic is forked.
type secretsHandler struct {
	conn   *grpc.ClientConn
	client *apigrpc.Client
}

// newSecretsHandler creates a new secrets handler bound to conn.
func newSecretsHandler(conn *grpc.ClientConn) (*secretsHandler, error) {
	client, err := apigrpc.NewClientWithConn(conn)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "grpc.newSecretsHandler",
			"failed to wrap connection")
	}
	return &secretsHandler{conn: conn, client: client}, nil
}

// secretsHandlerFunc handles a single secrets method.
type secretsHandlerFunc func(h *secretsHandler, ctx context.Context, req interface{}) (interface{}, error)

// secretsHandlerMap maps method names to handler functions for O(1) dispatch.
// Verbs without a SecretsManagement proto / pkg/api/grpc Client analogue
// (GetVersion, SetRotationPolicy, GetRotationPolicy) return a clear
// unimplemented error rather than falling through to the untyped conn.Invoke
// path.
var secretsHandlerMap = map[string]secretsHandlerFunc{
	"Store":        (*secretsHandler).handleStore,
	"Ensure":       (*secretsHandler).handleEnsure,
	"Get":          (*secretsHandler).handleGet,
	"GetData":      (*secretsHandler).handleGetData,
	"List":         (*secretsHandler).handleList,
	"Delete":       (*secretsHandler).handleDelete,
	"ListVersions": (*secretsHandler).handleListVersions,
	"Rotate":       (*secretsHandler).handleRotate,
	"Rollback":     (*secretsHandler).handleRollback,
	"UpdateStatus": (*secretsHandler).handleUpdateStatus,
}

// handleRequest routes a secrets request to the appropriate handler based on
// the method name.
func (h *secretsHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	handler, ok := secretsHandlerMap[method]
	if !ok {
		return nil, errors.NewUnimplemented("secretsHandler.handleRequest",
			"method not exposed by SecretsManagement gRPC proto: "+method+
				" (use the REST transport for this verb)")
	}
	return handler(h, ctx, req)
}

// handleStore stores a secret. The SDK request carries Data as a
// map[string]interface{}; the SecretsManagement proto stores raw bytes, so the
// map is JSON-encoded before transport — matching the embedded handler.
func (h *secretsHandler) handleStore(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*secretsclient.StoreSecretRequest)
	if !ok {
		return nil, errors.NewInvalidInput("secretsHandler.handleStore",
			"invalid request type, expected *secrets.StoreSecretRequest")
	}

	dataBytes, err := sdkReq.Payload()
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "secretsHandler.handleStore",
			"failed to encode secret data")
	}

	svcResp, err := h.client.StoreSecret(ctx, &service.StoreSecretRequest{
		Name:     sdkReq.Path,
		SecretID: sdkReq.Path,
		Data:     dataBytes,
		Metadata: sdkReq.Metadata,
	})
	if err != nil {
		return nil, err
	}

	// The Store SDK client only accepts the SDK response type.
	return &secretsclient.StoreSecretResponse{Version: int(svcResp.Version)}, nil
}

// handleEnsure implements the Vault-style generate-on-first-access primitive.
func (h *secretsHandler) handleEnsure(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*secretsclient.EnsureSecretRequest)
	if !ok {
		return nil, errors.NewInvalidInput("secretsHandler.handleEnsure",
			"invalid request type, expected *secrets.EnsureSecretRequest")
	}

	svcResp, err := h.client.EnsureSecret(ctx, &service.EnsureSecretRequest{
		SecretID: sdkReq.Path,
		Length:   sdkReq.Length,
		Encoding: sdkReq.Encoding,
		TenantID: sdkReq.TenantID,
	})
	if err != nil {
		return nil, err
	}

	return &secretsclient.EnsureSecretResponse{
		Path:      svcResp.SecretID,
		Data:      svcResp.Data,
		Generated: svcResp.Generated,
		Version:   int(svcResp.Version),
	}, nil
}

// handleGet retrieves a secret. The SDK client sends an unexported
// getSecretRequest{Path, Version}; we read both fields structurally.
func (h *secretsHandler) handleGet(ctx context.Context, req interface{}) (interface{}, error) {
	path, ok := stringFieldByName(req, "Path")
	if !ok {
		return nil, errors.NewInvalidInput("secretsHandler.handleGet",
			"invalid request type, expected struct with Path field")
	}
	version, _ := intFieldByName(req, "Version")

	svcResp, err := h.client.GetSecret(ctx, &service.GetSecretRequest{
		SecretID: path,
		Version:  uint64(version),
	})
	if err != nil {
		return nil, err
	}

	// Decode the stored bytes back into the SDK's map representation. When the
	// payload is not JSON, surface it under a "data" key — matching the
	// embedded handler's contract.
	dataMap := make(map[string]interface{})
	if len(svcResp.Data) > 0 {
		if unmarshalErr := json.Unmarshal(svcResp.Data, &dataMap); unmarshalErr != nil {
			dataMap["data"] = string(svcResp.Data)
		}
	}

	return &secretsclient.Secret{
		Path:      svcResp.SecretID,
		Data:      dataMap,
		Metadata:  svcResp.Metadata,
		Version:   int(svcResp.Version),
		CreatedAt: svcResp.CreatedAt,
	}, nil
}

// handleGetData backs Client.GetWithBytes. It returns the service response
// untouched: its Data is the proto's bytes field, so the stored payload
// reaches the client as a []byte — never decoded into a map of strings — and
// the client, which takes ownership of it, wipes it.
func (h *secretsHandler) handleGetData(ctx context.Context, req interface{}) (interface{}, error) {
	path, ok := stringFieldByName(req, "Path")
	if !ok {
		return nil, errors.NewInvalidInput("secretsHandler.handleGetData",
			"invalid request type, expected struct with Path field")
	}
	version, _ := intFieldByName(req, "Version")
	if version < 0 {
		version = 0
	}

	svcResp, err := h.client.GetSecret(ctx, &service.GetSecretRequest{
		SecretID: path,
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

// handleList lists secrets under a prefix. The SDK client sends an unexported
// listSecretsRequest{Prefix}.
func (h *secretsHandler) handleList(ctx context.Context, req interface{}) (interface{}, error) {
	prefix, ok := stringFieldByName(req, "Prefix")
	if !ok {
		return nil, errors.NewInvalidInput("secretsHandler.handleList",
			"invalid request type, expected struct with Prefix field")
	}

	svcResp, err := h.client.ListSecrets(ctx, &service.ListSecretsRequest{Prefix: prefix})
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(svcResp.Secrets))
	for _, s := range svcResp.Secrets {
		paths = append(paths, s.SecretID)
	}
	return &secretsclient.ListSecretsResponse{Paths: paths}, nil
}

// handleDelete deletes a secret. The SDK client sends an unexported
// deleteSecretRequest{Path} and accepts a nil response.
func (h *secretsHandler) handleDelete(ctx context.Context, req interface{}) (interface{}, error) {
	path, ok := stringFieldByName(req, "Path")
	if !ok {
		return nil, errors.NewInvalidInput("secretsHandler.handleDelete",
			"invalid request type, expected struct with Path field")
	}

	if _, err := h.client.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: path}); err != nil {
		return nil, err
	}
	// The Delete SDK client accepts a nil response.
	return nil, nil
}

// handleListVersions lists all versions of a secret. The SDK client accepts the
// *service.ListSecretVersionsResponse type directly.
func (h *secretsHandler) handleListVersions(ctx context.Context, req interface{}) (interface{}, error) {
	secretID, ok := stringFieldByName(req, "SecretID")
	if !ok {
		return nil, errors.NewInvalidInput("secretsHandler.handleListVersions",
			"invalid request type, expected struct with SecretID field")
	}
	return h.client.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{SecretID: secretID})
}

// handleRotate rotates a secret to a new value. The SDK client accepts the
// *service.RotateSecretResponse type directly.
func (h *secretsHandler) handleRotate(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*secretsclient.RotateSecretRequest)
	if !ok {
		return nil, errors.NewInvalidInput("secretsHandler.handleRotate",
			"invalid request type, expected *secrets.RotateSecretRequest")
	}
	return h.client.RotateSecret(ctx, &service.RotateSecretRequest{
		SecretID: sdkReq.SecretID,
		NewData:  sdkReq.NewData,
		Metadata: sdkReq.Metadata,
	})
}

// handleRollback rolls a secret back to a previous version. The SDK client
// accepts the *service.RollbackSecretResponse type directly.
func (h *secretsHandler) handleRollback(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*secretsclient.RollbackSecretRequest)
	if !ok {
		return nil, errors.NewInvalidInput("secretsHandler.handleRollback",
			"invalid request type, expected *secrets.RollbackSecretRequest")
	}
	return h.client.RollbackSecret(ctx, &service.RollbackSecretRequest{
		SecretID:      sdkReq.SecretID,
		TargetVersion: uint64(sdkReq.TargetVersion),
	})
}

// handleUpdateStatus enables or disables a secret. The SDK client accepts the
// *service.UpdateSecretStatusResponse type directly (or a nil response).
func (h *secretsHandler) handleUpdateStatus(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*secretsclient.UpdateSecretStatusRequest)
	if !ok {
		return nil, errors.NewInvalidInput("secretsHandler.handleUpdateStatus",
			"invalid request type, expected *secrets.UpdateSecretStatusRequest")
	}
	return h.client.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{
		SecretID: sdkReq.SecretID,
		Status:   service.SecretStatus(sdkReq.Status),
	})
}
