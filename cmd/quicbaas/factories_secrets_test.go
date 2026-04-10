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

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestCreateTransportAwareSecretsClient_GRPCTransport(t *testing.T) {
	origTransport := transport
	defer func() { transport = origTransport }()

	// gRPC client creation succeeds (lazy connection to unix socket)
	transport = "grpc"
	client, err := createTransportAwareSecretsClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.NoError(t, client.Close())
}

func TestCreateTransportAwareSecretsClient_EmptyTransport(t *testing.T) {
	origTransport := transport
	defer func() { transport = origTransport }()

	// Empty string defaults to gRPC
	transport = ""
	client, err := createTransportAwareSecretsClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.NoError(t, client.Close())
}

func TestCreateTransportAwareSecretsClient_REST(t *testing.T) {
	origTransport := transport
	origSDK := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origSDK
	}()

	transport = "rest"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientForMCP{mockTransport: &mockMCPTransport{}}, nil
	}

	client, err := createTransportAwareSecretsClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.IsType(t, &restSecretsClient{}, client)
	assert.NoError(t, client.Close())
}

func TestCreateTransportAwareSecretsClient_MCP(t *testing.T) {
	origTransport := transport
	origSDK := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origSDK
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientForMCP{mockTransport: &mockMCPTransport{}}, nil
	}

	client, err := createTransportAwareSecretsClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.IsType(t, &mcpSecretsClient{}, client)
	assert.NoError(t, client.Close())
}

func TestCreateTransportAwareSecretsClient_UnknownFallback(t *testing.T) {
	origTransport := transport
	defer func() { transport = origTransport }()

	// Unknown transport falls through to gRPC default which succeeds
	transport = "unknown-transport"
	client, err := createTransportAwareSecretsClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.NoError(t, client.Close())
}

// =============================================================================
// REST Secrets Client Tests
// =============================================================================

func newMockRESTSecretsClient(t *testing.T, respFn func(ctx context.Context, svc, method string, req interface{}) (interface{}, error)) *restSecretsClient {
	t.Helper()
	tr := &mockMCPTransport{requestFunc: respFn}
	sdk := &mockSDKClientForMCP{mockTransport: tr}
	return &restSecretsClient{sdk: sdk}
}

func marshalResp(m map[string]interface{}) interface{} {
	data, _ := json.Marshal(m)
	var result interface{}
	_ = json.Unmarshal(data, &result)
	return result
}

func TestRESTSecretsClient_StoreSecret(t *testing.T) {
	now := time.Now().Unix()
	client := newMockRESTSecretsClient(t, func(_ context.Context, svc, method string, _ interface{}) (interface{}, error) {
		assert.Equal(t, "REST", svc)
		assert.Equal(t, "POST", method)
		return marshalResp(map[string]interface{}{
			"secret_id": "db/prod",
			"version":   float64(1),
			"timestamp": float64(now),
		}), nil
	})

	resp, err := client.StoreSecret(context.Background(), &service.StoreSecretRequest{
		Name: "db/prod",
		Data: []byte("secret-data"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "db/prod", resp.SecretID)
	assert.Equal(t, uint64(1), resp.Version)
}

func TestRESTSecretsClient_StoreSecret_Error(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, assert.AnError
		},
	}
	sdk := &mockSDKClientForMCP{mockTransport: tr}
	client := &restSecretsClient{sdk: sdk}

	resp, err := client.StoreSecret(context.Background(), &service.StoreSecretRequest{Name: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTSecretsClient_GetSecret(t *testing.T) {
	client := newMockRESTSecretsClient(t, func(_ context.Context, svc, method string, _ interface{}) (interface{}, error) {
		assert.Equal(t, "REST", svc)
		assert.Equal(t, "GET", method)
		return marshalResp(map[string]interface{}{
			"secret_id": "db/prod",
			"version":   float64(1),
			"data":      "c2VjcmV0LWRhdGE=", // base64("secret-data")
		}), nil
	})

	resp, err := client.GetSecret(context.Background(), &service.GetSecretRequest{SecretID: "db/prod"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "db/prod", resp.SecretID)
	assert.Equal(t, []byte("secret-data"), resp.Data)
}

func TestRESTSecretsClient_GetSecret_Error(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, assert.AnError
		},
	}
	client := &restSecretsClient{sdk: &mockSDKClientForMCP{mockTransport: tr}}

	resp, err := client.GetSecret(context.Background(), &service.GetSecretRequest{SecretID: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTSecretsClient_DeleteSecret(t *testing.T) {
	client := newMockRESTSecretsClient(t, func(_ context.Context, svc, method string, _ interface{}) (interface{}, error) {
		assert.Equal(t, "REST", svc)
		assert.Equal(t, "DELETE", method)
		return marshalResp(map[string]interface{}{
			"secret_id": "db/old",
			"deleted":   true,
		}), nil
	})

	resp, err := client.DeleteSecret(context.Background(), &service.DeleteSecretRequest{SecretID: "db/old"})
	require.NoError(t, err)
	assert.True(t, resp.Deleted)
}

func TestRESTSecretsClient_DeleteSecret_Error(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, assert.AnError
		},
	}
	client := &restSecretsClient{sdk: &mockSDKClientForMCP{mockTransport: tr}}

	resp, err := client.DeleteSecret(context.Background(), &service.DeleteSecretRequest{SecretID: "db/old"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTSecretsClient_ListSecrets(t *testing.T) {
	client := newMockRESTSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return marshalResp(map[string]interface{}{
			"secrets": []interface{}{
				map[string]interface{}{
					"secret_id":       "db/prod",
					"name":            "prod-db",
					"current_version": float64(3),
				},
			},
			"total_count": float64(1),
		}), nil
	})

	resp, err := client.ListSecrets(context.Background(), &service.ListSecretsRequest{Limit: 10})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Secrets, 1)
}

func TestRESTSecretsClient_ListSecrets_Error(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, assert.AnError
		},
	}
	client := &restSecretsClient{sdk: &mockSDKClientForMCP{mockTransport: tr}}

	resp, err := client.ListSecrets(context.Background(), &service.ListSecretsRequest{})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTSecretsClient_GetSecretsByPrefix(t *testing.T) {
	client := newMockRESTSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return marshalResp(map[string]interface{}{
			"secrets": map[string]interface{}{
				"db/prod": map[string]interface{}{
					"password": "c2VjcmV0", // base64("secret")
				},
			},
			"total_count": float64(1),
		}), nil
	})

	resp, err := client.GetSecretsByPrefix(context.Background(), &service.GetSecretsByPrefixRequest{
		Prefix:      "db/",
		IncludeData: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestRESTSecretsClient_GetSecretsByPrefix_Error(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, assert.AnError
		},
	}
	client := &restSecretsClient{sdk: &mockSDKClientForMCP{mockTransport: tr}}

	resp, err := client.GetSecretsByPrefix(context.Background(), &service.GetSecretsByPrefixRequest{Prefix: "db/"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTSecretsClient_RotateSecret(t *testing.T) {
	client := newMockRESTSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return marshalResp(map[string]interface{}{
			"secret_id":   "db/prod",
			"old_version": float64(1),
			"new_version": float64(2),
		}), nil
	})

	resp, err := client.RotateSecret(context.Background(), &service.RotateSecretRequest{SecretID: "db/prod"})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestRESTSecretsClient_RotateSecret_Error(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, assert.AnError
		},
	}
	client := &restSecretsClient{sdk: &mockSDKClientForMCP{mockTransport: tr}}

	resp, err := client.RotateSecret(context.Background(), &service.RotateSecretRequest{SecretID: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTSecretsClient_ListSecretVersions(t *testing.T) {
	client := newMockRESTSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return marshalResp(map[string]interface{}{
			"versions": []interface{}{
				map[string]interface{}{
					"secret_id":  "db/prod",
					"version":    float64(1),
					"created_by": "admin",
				},
			},
		}), nil
	})

	resp, err := client.ListSecretVersions(context.Background(), &service.ListSecretVersionsRequest{SecretID: "db/prod"})
	require.NoError(t, err)
	assert.Len(t, resp.Versions, 1)
}

func TestRESTSecretsClient_ListSecretVersions_Error(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, assert.AnError
		},
	}
	client := &restSecretsClient{sdk: &mockSDKClientForMCP{mockTransport: tr}}

	resp, err := client.ListSecretVersions(context.Background(), &service.ListSecretVersionsRequest{SecretID: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTSecretsClient_RollbackSecret(t *testing.T) {
	client := newMockRESTSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return marshalResp(map[string]interface{}{
			"secret_id":   "db/prod",
			"new_version": float64(3),
		}), nil
	})

	resp, err := client.RollbackSecret(context.Background(), &service.RollbackSecretRequest{
		SecretID:      "db/prod",
		TargetVersion: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(3), resp.NewVersion)
}

func TestRESTSecretsClient_RollbackSecret_Error(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, assert.AnError
		},
	}
	client := &restSecretsClient{sdk: &mockSDKClientForMCP{mockTransport: tr}}

	resp, err := client.RollbackSecret(context.Background(), &service.RollbackSecretRequest{SecretID: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTSecretsClient_UpdateSecretStatus(t *testing.T) {
	client := newMockRESTSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return marshalResp(map[string]interface{}{
			"secret_id":  "db/prod",
			"old_status": "enabled",
			"new_status": "disabled",
		}), nil
	})

	resp, err := client.UpdateSecretStatus(context.Background(), &service.UpdateSecretStatusRequest{
		SecretID: "db/prod",
		Status:   service.SecretStatusDisabled,
	})
	require.NoError(t, err)
	assert.Equal(t, service.SecretStatus("disabled"), resp.NewStatus)
}

func TestRESTSecretsClient_UpdateSecretStatus_Error(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, assert.AnError
		},
	}
	client := &restSecretsClient{sdk: &mockSDKClientForMCP{mockTransport: tr}}

	resp, err := client.UpdateSecretStatus(context.Background(), &service.UpdateSecretStatusRequest{SecretID: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestRESTSecretsClient_WatchSecretsByPrefix_Unsupported(t *testing.T) {
	client := newMockRESTSecretsClient(t, nil)
	ch, err := client.WatchSecretsByPrefix(context.Background(), &service.WatchSecretsRequest{Prefix: "db/"})
	assert.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "rest-watch")
}

func TestRESTSecretsClient_Close(t *testing.T) {
	client := newMockRESTSecretsClient(t, nil)
	assert.NoError(t, client.Close())
}

func TestCreateRESTSecretsClient_Success(t *testing.T) {
	origSDK := sdkClientFactory
	defer func() { sdkClientFactory = origSDK }()

	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientForMCP{mockTransport: &mockMCPTransport{}}, nil
	}

	client, err := createRESTSecretsClient()
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestCreateRESTSecretsClient_SDKError(t *testing.T) {
	origSDK := sdkClientFactory
	defer func() { sdkClientFactory = origSDK }()

	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, assert.AnError
	}

	client, err := createRESTSecretsClient()
	assert.Error(t, err)
	assert.Nil(t, client)
}

// =============================================================================
// MCP Secrets Client Tests
// =============================================================================

func newMockMCPSecretsClient(t *testing.T, respFn func(ctx context.Context, svc, method string, req interface{}) (interface{}, error)) *mcpSecretsClient {
	t.Helper()
	tr := &mockMCPTransport{requestFunc: respFn}
	sdk := &mockSDKClientForMCP{mockTransport: tr}
	return &mcpSecretsClient{sdk: sdk}
}

func TestMCPSecretsClient_StoreSecret(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, svc, method string, req interface{}) (interface{}, error) {
		assert.Equal(t, "MCP", svc)
		assert.Equal(t, "quicbaas_secrets_store", method)
		args, ok := req.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "db/prod", args["name"])
		return marshalResp(map[string]interface{}{
			"secret_id": "db/prod",
			"version":   float64(1),
		}), nil
	})

	resp, err := client.StoreSecret(context.Background(), &service.StoreSecretRequest{
		Name:        "db/prod",
		Data:        []byte("secret"),
		Description: "production database",
		Metadata:    map[string]string{"team": "backend"},
	})
	require.NoError(t, err)
	assert.Equal(t, "db/prod", resp.SecretID)
}

func TestMCPSecretsClient_StoreSecret_Error(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return nil, assert.AnError
	})

	resp, err := client.StoreSecret(context.Background(), &service.StoreSecretRequest{Name: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestMCPSecretsClient_GetSecret(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
		assert.Equal(t, "quicbaas_secrets_get", method)
		args := req.(map[string]interface{})
		assert.Equal(t, "db/prod", args["secret_id"])
		return marshalResp(map[string]interface{}{
			"secret_id": "db/prod",
			"version":   float64(2),
			"data":      "c2VjcmV0", // base64("secret")
		}), nil
	})

	resp, err := client.GetSecret(context.Background(), &service.GetSecretRequest{
		SecretID: "db/prod",
		Version:  2,
	})
	require.NoError(t, err)
	assert.Equal(t, "db/prod", resp.SecretID)
}

func TestMCPSecretsClient_GetSecret_Error(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return nil, assert.AnError
	})

	resp, err := client.GetSecret(context.Background(), &service.GetSecretRequest{SecretID: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestMCPSecretsClient_DeleteSecret(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
		assert.Equal(t, "quicbaas_secrets_delete", method)
		args := req.(map[string]interface{})
		assert.Equal(t, "db/old", args["secret_id"])
		return marshalResp(map[string]interface{}{
			"secret_id": "db/old",
			"deleted":   true,
		}), nil
	})

	resp, err := client.DeleteSecret(context.Background(), &service.DeleteSecretRequest{
		SecretID:          "db/old",
		PermanentlyDelete: true,
	})
	require.NoError(t, err)
	assert.True(t, resp.Deleted)
}

func TestMCPSecretsClient_DeleteSecret_Error(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return nil, assert.AnError
	})

	resp, err := client.DeleteSecret(context.Background(), &service.DeleteSecretRequest{SecretID: "db/old"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestMCPSecretsClient_ListSecrets(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
		assert.Equal(t, "quicbaas_secrets_list", method)
		return marshalResp(map[string]interface{}{
			"secrets": []interface{}{
				map[string]interface{}{"secret_id": "db/prod"},
			},
		}), nil
	})

	resp, err := client.ListSecrets(context.Background(), &service.ListSecretsRequest{
		Limit:  10,
		Cursor: "abc",
	})
	require.NoError(t, err)
	assert.Len(t, resp.Secrets, 1)
}

func TestMCPSecretsClient_ListSecrets_Error(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return nil, assert.AnError
	})

	resp, err := client.ListSecrets(context.Background(), &service.ListSecretsRequest{})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestMCPSecretsClient_GetSecretsByPrefix(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
		assert.Equal(t, "quicbaas_secrets_get_prefix", method)
		return marshalResp(map[string]interface{}{
			"secrets":     map[string]interface{}{},
			"total_count": float64(0),
		}), nil
	})

	resp, err := client.GetSecretsByPrefix(context.Background(), &service.GetSecretsByPrefixRequest{
		Prefix:      "db/",
		IncludeData: true,
		Limit:       50,
		Cursor:      "xyz",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestMCPSecretsClient_GetSecretsByPrefix_Error(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return nil, assert.AnError
	})

	resp, err := client.GetSecretsByPrefix(context.Background(), &service.GetSecretsByPrefixRequest{Prefix: "db/"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestMCPSecretsClient_RotateSecret(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
		assert.Equal(t, "quicbaas_secrets_rotate", method)
		return marshalResp(map[string]interface{}{
			"secret_id":   "db/prod",
			"new_version": float64(2),
		}), nil
	})

	resp, err := client.RotateSecret(context.Background(), &service.RotateSecretRequest{
		SecretID: "db/prod",
		NewData:  []byte("new-password"),
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestMCPSecretsClient_RotateSecret_Error(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return nil, assert.AnError
	})

	resp, err := client.RotateSecret(context.Background(), &service.RotateSecretRequest{SecretID: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestMCPSecretsClient_ListSecretVersions(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
		assert.Equal(t, "quicbaas_secrets_versions", method)
		return marshalResp(map[string]interface{}{
			"versions": []interface{}{
				map[string]interface{}{"version": float64(1)},
			},
		}), nil
	})

	resp, err := client.ListSecretVersions(context.Background(), &service.ListSecretVersionsRequest{SecretID: "db/prod"})
	require.NoError(t, err)
	assert.Len(t, resp.Versions, 1)
}

func TestMCPSecretsClient_ListSecretVersions_Error(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return nil, assert.AnError
	})

	resp, err := client.ListSecretVersions(context.Background(), &service.ListSecretVersionsRequest{SecretID: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestMCPSecretsClient_RollbackSecret(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
		assert.Equal(t, "quicbaas_secrets_rollback", method)
		return marshalResp(map[string]interface{}{
			"secret_id":   "db/prod",
			"new_version": float64(3),
		}), nil
	})

	resp, err := client.RollbackSecret(context.Background(), &service.RollbackSecretRequest{
		SecretID:      "db/prod",
		TargetVersion: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(3), resp.NewVersion)
}

func TestMCPSecretsClient_RollbackSecret_Error(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return nil, assert.AnError
	})

	resp, err := client.RollbackSecret(context.Background(), &service.RollbackSecretRequest{SecretID: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestMCPSecretsClient_UpdateSecretStatus(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
		assert.Equal(t, "quicbaas_secrets_update_status", method)
		return marshalResp(map[string]interface{}{
			"secret_id":  "db/prod",
			"old_status": "enabled",
			"new_status": "disabled",
		}), nil
	})

	resp, err := client.UpdateSecretStatus(context.Background(), &service.UpdateSecretStatusRequest{
		SecretID: "db/prod",
		Status:   service.SecretStatusDisabled,
	})
	require.NoError(t, err)
	assert.Equal(t, service.SecretStatus("disabled"), resp.NewStatus)
}

func TestMCPSecretsClient_UpdateSecretStatus_Error(t *testing.T) {
	client := newMockMCPSecretsClient(t, func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
		return nil, assert.AnError
	})

	resp, err := client.UpdateSecretStatus(context.Background(), &service.UpdateSecretStatusRequest{SecretID: "db/prod"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestMCPSecretsClient_WatchSecretsByPrefix_Unsupported(t *testing.T) {
	client := newMockMCPSecretsClient(t, nil)
	ch, err := client.WatchSecretsByPrefix(context.Background(), &service.WatchSecretsRequest{Prefix: "db/"})
	assert.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "mcp-watch")
}

func TestMCPSecretsClient_Close(t *testing.T) {
	client := newMockMCPSecretsClient(t, nil)
	assert.NoError(t, client.Close())
}

func TestCreateMCPSecretsClient_Success(t *testing.T) {
	origSDK := sdkClientFactory
	defer func() { sdkClientFactory = origSDK }()

	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientForMCP{mockTransport: &mockMCPTransport{}}, nil
	}

	client, err := createMCPSecretsClient()
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestCreateMCPSecretsClient_SDKError(t *testing.T) {
	origSDK := sdkClientFactory
	defer func() { sdkClientFactory = origSDK }()

	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, assert.AnError
	}

	client, err := createMCPSecretsClient()
	assert.Error(t, err)
	assert.Nil(t, client)
}

// Ensure both adapters satisfy the SecretsClient interface.
var _ SecretsClient = (*restSecretsClient)(nil)
