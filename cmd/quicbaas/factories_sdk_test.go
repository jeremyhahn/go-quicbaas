//go:build !integration && hashicorpmetrics && local

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// --- Mock Transport ---

type sdkMockTransport struct {
	resp interface{}
	err  error
}

func (m *sdkMockTransport) Request(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
	return m.resp, m.err
}

func (m *sdkMockTransport) RequestStream(_ context.Context, _, _ string, _ interface{}) (sdktransport.Stream, error) {
	return nil, nil
}

func (m *sdkMockTransport) Conn() interface{} { return nil }
func (m *sdkMockTransport) Close() error      { return nil }
func (m *sdkMockTransport) Healthy() bool     { return true }

// sdkTestClient wraps mockClient and adds a configurable Transport.
type sdkTestClient struct {
	mockClient
	mockTransport quicbaas.Transport
	closeErr      error
}

func (c *sdkTestClient) Transport() quicbaas.Transport { return c.mockTransport }
func (c *sdkTestClient) Close() error {
	c.closed = true
	return c.closeErr
}

// newTestSDKClient creates a test SDK client that returns the given response or error.
func newTestSDKClient(resp interface{}, err error) quicbaas.Client {
	return &sdkTestClient{
		mockTransport: &sdkMockTransport{resp: resp, err: err},
	}
}

// --- Backup Request Field Access ---

func TestBackupCreateRequest_FieldAccess(t *testing.T) {
	r := &BackupCreateRequest{
		Name:          "test-backup",
		BackupType:    "full",
		Compression:   "gzip",
		Encryption:    "aes256",
		StoragePath:   "/backups",
		StorageType:   "s3",
		StorageConfig: map[string]interface{}{"bucket": "test"},
		Tags:          map[string]string{"env": "prod"},
	}

	assert.Equal(t, "test-backup", r.Name)
	assert.Equal(t, "full", r.BackupType)
	assert.Equal(t, "gzip", r.Compression)
	assert.Equal(t, "aes256", r.Encryption)
	assert.Equal(t, "/backups", r.StoragePath)
	assert.Equal(t, "s3", r.StorageType)
	assert.Equal(t, map[string]interface{}{"bucket": "test"}, r.StorageConfig)
	assert.Equal(t, map[string]string{"env": "prod"}, r.Tags)
}

func TestBackupCreateRequest_EmptyFieldAccess(t *testing.T) {
	r := &BackupCreateRequest{}
	assert.Equal(t, "", r.Name)
	assert.Equal(t, "", r.BackupType)
	assert.Equal(t, "", r.Compression)
	assert.Equal(t, "", r.Encryption)
	assert.Equal(t, "", r.StoragePath)
	assert.Equal(t, "", r.StorageType)
	assert.Nil(t, r.StorageConfig)
	assert.Nil(t, r.Tags)
}

func TestBackupListRequest_FieldAccess(t *testing.T) {
	r := &BackupListRequest{
		Limit:         50,
		Offset:        10,
		StoragePath:   "/data",
		StorageType:   "local",
		StorageConfig: map[string]interface{}{"path": "/mnt"},
	}

	assert.Equal(t, 50, r.Limit)
	assert.Equal(t, 10, r.Offset)
	assert.Equal(t, "/data", r.StoragePath)
	assert.Equal(t, "local", r.StorageType)
	assert.Equal(t, map[string]interface{}{"path": "/mnt"}, r.StorageConfig)
}

func TestBackupListRequest_EmptyFieldAccess(t *testing.T) {
	r := &BackupListRequest{}
	assert.Equal(t, 0, r.Limit)
	assert.Equal(t, 0, r.Offset)
	assert.Equal(t, "", r.StoragePath)
	assert.Equal(t, "", r.StorageType)
	assert.Nil(t, r.StorageConfig)
}

func TestBackupRestoreRequest_FieldAccess(t *testing.T) {
	pit := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	r := &BackupRestoreRequest{
		BackupID:       "b-123",
		StoragePath:    "/restore",
		VerifyFirst:    true,
		VerifyChecksum: true,
		SkipExisting:   true,
		PointInTime:    pit,
		StorageType:    "gcs",
		StorageConfig:  map[string]interface{}{"project": "p1"},
		EncryptionKey:  "enc-key",
	}

	assert.Equal(t, "b-123", r.BackupID)
	assert.Equal(t, "/restore", r.StoragePath)
	assert.True(t, r.VerifyFirst)
	assert.True(t, r.VerifyChecksum)
	assert.True(t, r.SkipExisting)
	assert.Equal(t, pit, r.PointInTime)
	assert.Equal(t, "gcs", r.StorageType)
	assert.Equal(t, map[string]interface{}{"project": "p1"}, r.StorageConfig)
	assert.Equal(t, "enc-key", r.EncryptionKey)
}

func TestBackupRestoreRequest_EmptyFieldAccess(t *testing.T) {
	r := &BackupRestoreRequest{}
	assert.Equal(t, "", r.BackupID)
	assert.Equal(t, "", r.StoragePath)
	assert.False(t, r.VerifyFirst)
	assert.False(t, r.VerifyChecksum)
	assert.False(t, r.SkipExisting)
	assert.True(t, r.PointInTime.IsZero())
	assert.Equal(t, "", r.StorageType)
	assert.Nil(t, r.StorageConfig)
	assert.Equal(t, "", r.EncryptionKey)
}

func TestBackupDeleteRequest_FieldAccess(t *testing.T) {
	r := &BackupDeleteRequest{
		BackupID:    "b-456",
		StoragePath: "/del",
		Force:       true,
	}

	assert.Equal(t, "b-456", r.BackupID)
	assert.Equal(t, "/del", r.StoragePath)
	assert.True(t, r.Force)
}

func TestBackupDeleteRequest_EmptyFieldAccess(t *testing.T) {
	r := &BackupDeleteRequest{}
	assert.Equal(t, "", r.BackupID)
	assert.Equal(t, "", r.StoragePath)
	assert.False(t, r.Force)
}

func TestBackupVerifyRequest_FieldAccess(t *testing.T) {
	r := &BackupVerifyRequest{
		BackupID:      "b-789",
		StoragePath:   "/verify",
		StorageType:   "azure",
		EncryptionKey: "verify-key",
	}

	assert.Equal(t, "b-789", r.BackupID)
	assert.Equal(t, "/verify", r.StoragePath)
	assert.Equal(t, "azure", r.StorageType)
	assert.Equal(t, "verify-key", r.EncryptionKey)
}

func TestBackupVerifyRequest_EmptyFieldAccess(t *testing.T) {
	r := &BackupVerifyRequest{}
	assert.Equal(t, "", r.BackupID)
	assert.Equal(t, "", r.StoragePath)
	assert.Equal(t, "", r.StorageType)
	assert.Equal(t, "", r.EncryptionKey)
}

// --- Interface compliance checks ---

// --- Factory Functions ---

func TestCreateSDKCryptoClient(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newTestSDKClient(nil, nil), nil
		}
		client, err := createSDKCryptoClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("FactoryError", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, fmt.Errorf("factory error")
		}
		client, err := createSDKCryptoClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestCreateSDKKMSClient(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newTestSDKClient(nil, nil), nil
		}
		client, err := createSDKKMSClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("FactoryError", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, fmt.Errorf("factory error")
		}
		client, err := createSDKKMSClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestCreateSDKSealClient(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newTestSDKClient(nil, nil), nil
		}
		client, err := createSDKSealClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("FactoryError", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, fmt.Errorf("factory error")
		}
		client, err := createSDKSealClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestCreateSDKBackendClient(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newTestSDKClient(nil, nil), nil
		}
		client, err := createSDKBackendClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("FactoryError", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, fmt.Errorf("factory error")
		}
		client, err := createSDKBackendClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestCreateSDKSafetyClient(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newTestSDKClient(nil, nil), nil
		}
		client, err := createSDKSafetyClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("FactoryError", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, fmt.Errorf("factory error")
		}
		client, err := createSDKSafetyClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestCreateSDKBackupClient(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newTestSDKClient(nil, nil), nil
		}
		client, err := createSDKBackupClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("FactoryError", func(t *testing.T) {
		saved := sdkClientFactory
		defer func() { sdkClientFactory = saved }()
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, fmt.Errorf("factory error")
		}
		client, err := createSDKBackupClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

// --- SDK Crypto Client Methods ---

func TestSDKCryptoClient_KMSEncrypt(t *testing.T) {
	ctx := context.Background()
	expected := &service.EncryptResponse{KeyID: "k1", Ciphertext: []byte("ct")}

	t.Run("Success", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSEncrypt(ctx, &service.EncryptRequest{})
		require.NoError(t, err)
		assert.Equal(t, "k1", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("transport error"))}
		resp, err := c.KMSEncrypt(ctx, &service.EncryptRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient("wrong-type", nil)}
		resp, err := c.KMSEncrypt(ctx, &service.EncryptRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "unexpected response type")
	})
}

func TestSDKCryptoClient_KMSDecrypt(t *testing.T) {
	ctx := context.Background()
	expected := &service.DecryptResponse{KeyID: "k2", Plaintext: []byte("pt")}

	t.Run("Success", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSDecrypt(ctx, &service.DecryptRequest{})
		require.NoError(t, err)
		assert.Equal(t, "k2", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSDecrypt(ctx, &service.DecryptRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSDecrypt(ctx, &service.DecryptRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKCryptoClient_KMSSign(t *testing.T) {
	ctx := context.Background()
	expected := &service.SignResponse{KeyID: "sk"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSSign(ctx, &service.SignRequest{})
		require.NoError(t, err)
		assert.Equal(t, "sk", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSSign(ctx, &service.SignRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(42, nil)}
		resp, err := c.KMSSign(ctx, &service.SignRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKCryptoClient_KMSVerify(t *testing.T) {
	ctx := context.Background()
	expected := &service.VerifyResponse{Valid: true}

	t.Run("Success", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSVerify(ctx, &service.VerifyRequest{})
		require.NoError(t, err)
		assert.True(t, resp.Valid)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSVerify(ctx, &service.VerifyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSVerify(ctx, &service.VerifyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKCryptoClient_KMSHash(t *testing.T) {
	ctx := context.Background()
	expected := &service.HashResponse{Hash: []byte("h")}

	t.Run("Success", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSHash(ctx, &service.HashRequest{})
		require.NoError(t, err)
		assert.Equal(t, []byte("h"), resp.Hash)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSHash(ctx, &service.HashRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSHash(ctx, &service.HashRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKCryptoClient_KMSGenerateHMAC(t *testing.T) {
	ctx := context.Background()
	expected := &service.HMACResponse{KeyID: "hk"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSGenerateHMAC(ctx, &service.HMACRequest{})
		require.NoError(t, err)
		assert.Equal(t, "hk", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSGenerateHMAC(ctx, &service.HMACRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSGenerateHMAC(ctx, &service.HMACRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKCryptoClient_KMSVerifyHMAC(t *testing.T) {
	ctx := context.Background()
	expected := &service.VerifyHMACResponse{Valid: true}

	t.Run("Success", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSVerifyHMAC(ctx, &service.VerifyHMACRequest{})
		require.NoError(t, err)
		assert.True(t, resp.Valid)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSVerifyHMAC(ctx, &service.VerifyHMACRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSVerifyHMAC(ctx, &service.VerifyHMACRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKCryptoClient_KMSRewrap(t *testing.T) {
	ctx := context.Background()
	expected := &service.RewrapResponse{KeyID: "rk"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSRewrap(ctx, &service.RewrapRequest{})
		require.NoError(t, err)
		assert.Equal(t, "rk", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSRewrap(ctx, &service.RewrapRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSRewrap(ctx, &service.RewrapRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKCryptoClient_KMSGenerateDataKey(t *testing.T) {
	ctx := context.Background()
	expected := &service.GenerateDataKeyResponse{KeyID: "dk"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSGenerateDataKey(ctx, &service.GenerateDataKeyRequest{})
		require.NoError(t, err)
		assert.Equal(t, "dk", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSGenerateDataKey(ctx, &service.GenerateDataKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSGenerateDataKey(ctx, &service.GenerateDataKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKCryptoClient_KMSGenerateRandom(t *testing.T) {
	ctx := context.Background()
	expected := &service.GenerateRandomResponse{RandomBytes: []byte("rand")}

	t.Run("Success", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSGenerateRandom(ctx, &service.GenerateRandomRequest{})
		require.NoError(t, err)
		assert.Equal(t, []byte("rand"), resp.RandomBytes)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSGenerateRandom(ctx, &service.GenerateRandomRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkCryptoClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSGenerateRandom(ctx, &service.GenerateRandomRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKCryptoClient_Close(t *testing.T) {
	c := &sdkCryptoClient{sdk: newTestSDKClient(nil, nil)}
	assert.NoError(t, c.Close())
}

// --- SDK KMS Client Methods ---

func TestSDKKMSClient_KMSGenerateKey(t *testing.T) {
	ctx := context.Background()
	expected := &service.GenerateKeyResponse{}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSGenerateKey(ctx, &service.GenerateKeyRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSGenerateKey(ctx, &service.GenerateKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSGenerateKey(ctx, &service.GenerateKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSImportKey(t *testing.T) {
	ctx := context.Background()
	expected := &service.ImportKeyResponse{}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSImportKey(ctx, &service.ImportKeyRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSImportKey(ctx, &service.ImportKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSImportKey(ctx, &service.ImportKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSGetKey(t *testing.T) {
	ctx := context.Background()
	expected := &service.GetKeyResponse{}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSGetKey(ctx, &service.GetKeyRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSGetKey(ctx, &service.GetKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSGetKey(ctx, &service.GetKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSListKeys(t *testing.T) {
	ctx := context.Background()
	expected := &service.ListKeysResponse{}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSListKeys(ctx, &service.ListKeysRequest{})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSListKeys(ctx, &service.ListKeysRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSListKeys(ctx, &service.ListKeysRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSRotateKey(t *testing.T) {
	ctx := context.Background()
	expected := &service.RotateKeyResponse{KeyID: "rot-k"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSRotateKey(ctx, &service.RotateKeyRequest{})
		require.NoError(t, err)
		assert.Equal(t, "rot-k", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSRotateKey(ctx, &service.RotateKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSRotateKey(ctx, &service.RotateKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSDeleteKey(t *testing.T) {
	ctx := context.Background()
	expected := &service.DeleteKeyResponse{KeyID: "del-k"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSDeleteKey(ctx, &service.DeleteKeyRequest{})
		require.NoError(t, err)
		assert.Equal(t, "del-k", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSDeleteKey(ctx, &service.DeleteKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSDeleteKey(ctx, &service.DeleteKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSUpdateKeyStatus(t *testing.T) {
	ctx := context.Background()
	expected := &service.UpdateKeyStatusResponse{KeyID: "upd-k"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{})
		require.NoError(t, err)
		assert.Equal(t, "upd-k", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSListKeyVersions(t *testing.T) {
	ctx := context.Background()
	expected := &service.ListKeyVersionsResponse{KeyID: "lkv"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{})
		require.NoError(t, err)
		assert.Equal(t, "lkv", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSExportKey(t *testing.T) {
	ctx := context.Background()
	expected := &service.ExportKeyResponse{KeyID: "exp-k"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSExportKey(ctx, &service.ExportKeyRequest{})
		require.NoError(t, err)
		assert.Equal(t, "exp-k", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSExportKey(ctx, &service.ExportKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSExportKey(ctx, &service.ExportKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSUnwrapKey(t *testing.T) {
	ctx := context.Background()
	expected := &service.UnwrapKeyResponse{KeyID: "unw-k"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{})
		require.NoError(t, err)
		assert.Equal(t, "unw-k", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSSetKeySafety(t *testing.T) {
	ctx := context.Background()
	expected := &service.SetKeySafetyResponse{KeyID: "sks"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{})
		require.NoError(t, err)
		assert.Equal(t, "sks", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSClearKeySafety(t *testing.T) {
	ctx := context.Background()
	expected := &service.ClearKeySafetyResponse{KeyID: "cks"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{})
		require.NoError(t, err)
		assert.Equal(t, "cks", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSEncrypt(t *testing.T) {
	ctx := context.Background()
	expected := &service.EncryptResponse{KeyID: "ke"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSEncrypt(ctx, &service.EncryptRequest{})
		require.NoError(t, err)
		assert.Equal(t, "ke", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSEncrypt(ctx, &service.EncryptRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSEncrypt(ctx, &service.EncryptRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_KMSDecrypt(t *testing.T) {
	ctx := context.Background()
	expected := &service.DecryptResponse{KeyID: "kd"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSDecrypt(ctx, &service.DecryptRequest{})
		require.NoError(t, err)
		assert.Equal(t, "kd", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSDecrypt(ctx, &service.DecryptRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkKMSClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSDecrypt(ctx, &service.DecryptRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKKMSClient_Close(t *testing.T) {
	c := &sdkKMSClient{sdk: newTestSDKClient(nil, nil)}
	assert.NoError(t, c.Close())
}

// --- SDK DEK Client Methods ---

// --- SDK Seal Client Methods ---

func TestSDKSealClient_SealInit(t *testing.T) {
	ctx := context.Background()
	expected := &service.SealInitResponse{RootToken: "rt"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.SealInit(ctx, &service.SealInitRequest{})
		require.NoError(t, err)
		assert.Equal(t, "rt", resp.RootToken)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.SealInit(ctx, &service.SealInitRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.SealInit(ctx, &service.SealInitRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKSealClient_Unseal(t *testing.T) {
	ctx := context.Background()
	expected := &service.UnsealResponse{Sealed: false}

	t.Run("Success", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.Unseal(ctx, &service.UnsealRequest{})
		require.NoError(t, err)
		assert.False(t, resp.Sealed)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.Unseal(ctx, &service.UnsealRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.Unseal(ctx, &service.UnsealRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKSealClient_Seal(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient(nil, nil)}
		err := c.Seal(ctx)
		assert.NoError(t, err)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient(nil, fmt.Errorf("seal error"))}
		err := c.Seal(ctx)
		assert.Error(t, err)
	})
}

func TestSDKSealClient_SealStatus(t *testing.T) {
	ctx := context.Background()
	expected := &service.SealStatusResponse{Sealed: true}

	t.Run("Success", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.SealStatus(ctx)
		require.NoError(t, err)
		assert.True(t, resp.Sealed)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.SealStatus(ctx)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.SealStatus(ctx)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKSealClient_Rekey(t *testing.T) {
	ctx := context.Background()
	expected := &service.RekeyResponse{Version: 2}

	t.Run("Success", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.Rekey(ctx, &service.RekeyRequest{})
		require.NoError(t, err)
		assert.Equal(t, uint64(2), resp.Version)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.Rekey(ctx, &service.RekeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkSealClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.Rekey(ctx, &service.RekeyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKSealClient_Close(t *testing.T) {
	c := &sdkSealClient{sdk: newTestSDKClient(nil, nil)}
	assert.NoError(t, c.Close())
}

// --- SDK Backend Client Methods ---

func TestSDKBackendClient_KMSBackendStatus(t *testing.T) {
	ctx := context.Background()
	expected := &service.BackendStatusResponse{BackendType: "software"}

	t.Run("Success", func(t *testing.T) {
		c := &sdkBackendClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSBackendStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, "software", resp.BackendType)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkBackendClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSBackendStatus(ctx)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkBackendClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSBackendStatus(ctx)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKBackendClient_KMSBackendTest(t *testing.T) {
	ctx := context.Background()
	expected := &service.BackendTestResponse{Success: true}

	t.Run("Success", func(t *testing.T) {
		c := &sdkBackendClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSBackendTest(ctx)
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkBackendClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSBackendTest(ctx)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkBackendClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSBackendTest(ctx)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKBackendClient_Close(t *testing.T) {
	c := &sdkBackendClient{sdk: newTestSDKClient(nil, nil)}
	assert.NoError(t, c.Close())
}

// --- SDK Safety Client Methods ---

func TestSDKSafetyClient_KMSSafetyCheck(t *testing.T) {
	ctx := context.Background()
	expected := &service.SafetyCheckResponse{TotalKeysChecked: 5}

	t.Run("Success", func(t *testing.T) {
		c := &sdkSafetyClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		require.NoError(t, err)
		assert.Equal(t, 5, resp.TotalKeysChecked)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkSafetyClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkSafetyClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKSafetyClient_KMSSafetyReport(t *testing.T) {
	ctx := context.Background()
	expected := &service.SafetyReportResponse{TotalOperations: 100}

	t.Run("Success", func(t *testing.T) {
		c := &sdkSafetyClient{sdk: newTestSDKClient(expected, nil)}
		resp, err := c.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		require.NoError(t, err)
		assert.Equal(t, uint64(100), resp.TotalOperations)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkSafetyClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkSafetyClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKSafetyClient_Close(t *testing.T) {
	c := &sdkSafetyClient{sdk: newTestSDKClient(nil, nil)}
	assert.NoError(t, c.Close())
}

// --- SDK Backup Client Methods ---

func TestSDKBackupClient_CreateBackup(t *testing.T) {
	ctx := context.Background()
	svcResp := &service.CreateBackupResponse{BackupID: "b-new", Status: "completed", Size: 1024}

	t.Run("Success", func(t *testing.T) {
		c := &sdkBackupClient{sdk: newTestSDKClient(svcResp, nil)}
		resp, err := c.CreateBackup(ctx, &BackupCreateRequest{
			Name: "bk", BackupType: "full", Compression: "gzip", Encryption: "aes",
		})
		require.NoError(t, err)
		assert.Equal(t, "b-new", resp.BackupID)
		assert.True(t, resp.Success)
		assert.Equal(t, int64(1024), resp.Size)
		assert.Equal(t, "full", resp.Type)
		assert.Equal(t, "gzip", resp.Compression)
		assert.Equal(t, "aes", resp.Encryption)
	})

	t.Run("SuccessStatus", func(t *testing.T) {
		svcResp2 := &service.CreateBackupResponse{BackupID: "b-2", Status: "success"}
		c := &sdkBackupClient{sdk: newTestSDKClient(svcResp2, nil)}
		resp, err := c.CreateBackup(ctx, &BackupCreateRequest{})
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("FailedStatus", func(t *testing.T) {
		svcResp3 := &service.CreateBackupResponse{BackupID: "b-3", Status: "failed"}
		c := &sdkBackupClient{sdk: newTestSDKClient(svcResp3, nil)}
		resp, err := c.CreateBackup(ctx, &BackupCreateRequest{})
		require.NoError(t, err)
		assert.False(t, resp.Success)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkBackupClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.CreateBackup(ctx, &BackupCreateRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkBackupClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.CreateBackup(ctx, &BackupCreateRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKBackupClient_ListBackups(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		svcResp := &service.ListBackupsResponse{
			Backups: []*service.BackupMetadata{
				{BackupID: "b-1", Size: 100, Timestamp: time.Now().UnixNano()},
				{BackupID: "b-2", Size: 200},
			},
			TotalCount: 2,
		}
		c := &sdkBackupClient{sdk: newTestSDKClient(svcResp, nil)}
		resp, err := c.ListBackups(ctx, &BackupListRequest{Limit: 10, Offset: 0, StoragePath: "/sp"})
		require.NoError(t, err)
		assert.Equal(t, 2, resp.TotalCount)
		assert.Len(t, resp.Backups, 2)
		assert.Equal(t, "b-1", resp.Backups[0].ID)
		assert.Equal(t, "/sp", resp.StoragePath)
	})

	t.Run("EmptyList", func(t *testing.T) {
		svcResp := &service.ListBackupsResponse{
			Backups:    []*service.BackupMetadata{},
			TotalCount: 0,
		}
		c := &sdkBackupClient{sdk: newTestSDKClient(svcResp, nil)}
		resp, err := c.ListBackups(ctx, &BackupListRequest{})
		require.NoError(t, err)
		assert.Equal(t, 0, resp.TotalCount)
		assert.Empty(t, resp.Backups)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkBackupClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.ListBackups(ctx, &BackupListRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkBackupClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.ListBackups(ctx, &BackupListRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKBackupClient_RestoreBackup(t *testing.T) {
	ctx := context.Background()

	t.Run("SuccessCompleted", func(t *testing.T) {
		svcResp := &service.RestoreBackupResponse{Status: "completed"}
		c := &sdkBackupClient{sdk: newTestSDKClient(svcResp, nil)}
		resp, err := c.RestoreBackup(ctx, &BackupRestoreRequest{BackupID: "b-rest"})
		require.NoError(t, err)
		assert.Equal(t, "b-rest", resp.BackupID)
		assert.True(t, resp.Success)
	})

	t.Run("SuccessStatus", func(t *testing.T) {
		svcResp := &service.RestoreBackupResponse{Status: "success"}
		c := &sdkBackupClient{sdk: newTestSDKClient(svcResp, nil)}
		resp, err := c.RestoreBackup(ctx, &BackupRestoreRequest{BackupID: "b-rest2"})
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("FailedStatus", func(t *testing.T) {
		svcResp := &service.RestoreBackupResponse{Status: "failed"}
		c := &sdkBackupClient{sdk: newTestSDKClient(svcResp, nil)}
		resp, err := c.RestoreBackup(ctx, &BackupRestoreRequest{BackupID: "b-rest3"})
		require.NoError(t, err)
		assert.False(t, resp.Success)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkBackupClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.RestoreBackup(ctx, &BackupRestoreRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkBackupClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.RestoreBackup(ctx, &BackupRestoreRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKBackupClient_DeleteBackup(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		svcResp := &service.DeleteBackupResponse{Success: true, Message: "deleted"}
		c := &sdkBackupClient{sdk: newTestSDKClient(svcResp, nil)}
		resp, err := c.DeleteBackup(ctx, &BackupDeleteRequest{BackupID: "b-del"})
		require.NoError(t, err)
		assert.Equal(t, "b-del", resp.BackupID)
		assert.True(t, resp.Deleted)
		assert.Equal(t, "deleted", resp.Message)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkBackupClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.DeleteBackup(ctx, &BackupDeleteRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkBackupClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.DeleteBackup(ctx, &BackupDeleteRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKBackupClient_VerifyBackup(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		svcResp := &service.VerifyBackupResponse{
			Valid:         true,
			ChecksumMatch: true,
			Issues:        []string{"minor-warning"},
		}
		c := &sdkBackupClient{sdk: newTestSDKClient(svcResp, nil)}
		resp, err := c.VerifyBackup(ctx, &BackupVerifyRequest{BackupID: "b-ver"})
		require.NoError(t, err)
		assert.Equal(t, "b-ver", resp.BackupID)
		assert.True(t, resp.Valid)
		assert.True(t, resp.ChecksumValid)
		assert.Len(t, resp.Issues, 1)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &sdkBackupClient{sdk: newTestSDKClient(nil, fmt.Errorf("err"))}
		resp, err := c.VerifyBackup(ctx, &BackupVerifyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WrongType", func(t *testing.T) {
		c := &sdkBackupClient{sdk: newTestSDKClient("bad", nil)}
		resp, err := c.VerifyBackup(ctx, &BackupVerifyRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestSDKBackupClient_Close(t *testing.T) {
	c := &sdkBackupClient{sdk: newTestSDKClient(nil, nil)}
	assert.NoError(t, c.Close())
}
