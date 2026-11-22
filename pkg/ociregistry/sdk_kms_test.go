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

package ociregistry

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

var errFakeKMSUnavailable = errors.New("kms: deadline exceeded")

// fakeSDKEncryptionKMS stands in for the SDK KMS and crypto clients: it holds
// AES-256 keys by ID and seals with the platform AES-GCM, as the KMS does.
type fakeSDKEncryptionKMS struct {
	keys      map[string][]byte
	usages    map[string][]kms.KeyUsage
	generated int
	getErr    error
	genReqs   []kms.GenerateKeyRequest
}

func newFakeSDKEncryptionKMS() *fakeSDKEncryptionKMS {
	return &fakeSDKEncryptionKMS{keys: map[string][]byte{}, usages: map[string][]kms.KeyUsage{}}
}

func (f *fakeSDKEncryptionKMS) GetKey(_ context.Context, keyID string) (*kms.KeyInfo, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if _, ok := f.keys[keyID]; !ok {
		return nil, sdkerrors.NewNotFound("GetKey", "key "+keyID+" not found")
	}
	return &kms.KeyInfo{
		KeyID:     keyID,
		Name:      keyID,
		Algorithm: kms.AlgorithmAES256GCM,
		Version:   1,
		Usage:     f.usages[keyID],
		CreatedAt: time.Unix(1700000000, 0).UTC(),
	}, nil
}

func (f *fakeSDKEncryptionKMS) GenerateKey(_ context.Context, req *kms.GenerateKeyRequest) (*kms.GenerateKeyResponse, error) {
	// The KMS uses the creation name as the key ID and refuses a duplicate.
	id := req.Name
	if _, ok := f.keys[id]; ok {
		return nil, sdkerrors.NewAlreadyExists("GenerateKey", "key "+id+" already exists")
	}
	f.generated++
	f.genReqs = append(f.genReqs, *req)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f.keys[id] = key
	f.usages[id] = req.Usage
	return &kms.GenerateKeyResponse{KeyID: id, Version: 1, Algorithm: req.Algorithm}, nil
}

func (f *fakeSDKEncryptionKMS) Encrypt(_ context.Context, req *quicbaas.EncryptRequest) (*quicbaas.EncryptResponse, error) {
	key, ok := f.keys[req.KeyID]
	if !ok {
		return nil, sdkerrors.NewNotFound("Encrypt", "key "+req.KeyID+" not found")
	}
	sealed, err := gcm.Seal(key, req.Plaintext, req.Context)
	if err != nil {
		return nil, err
	}
	return &quicbaas.EncryptResponse{Ciphertext: sealed, KeyVersion: 1}, nil
}

func (f *fakeSDKEncryptionKMS) Decrypt(_ context.Context, req *quicbaas.DecryptRequest) (*quicbaas.DecryptResponse, error) {
	key, ok := f.keys[req.KeyID]
	if !ok {
		return nil, sdkerrors.NewNotFound("Decrypt", "key "+req.KeyID+" not found")
	}
	plaintext, err := gcm.Open(key, req.Ciphertext, req.Context)
	if err != nil {
		return nil, err
	}
	return &quicbaas.DecryptResponse{Plaintext: plaintext}, nil
}

func newTestSDKKMSClient(t *testing.T) (*SDKKMSClient, *fakeSDKEncryptionKMS) {
	t.Helper()
	fake := newFakeSDKEncryptionKMS()
	client, err := NewSDKKMSClient(fake, fake)
	if err != nil {
		t.Fatalf("NewSDKKMSClient: %v", err)
	}
	return client, fake
}

func TestSDKKMSClient_ImageRoundTrip(t *testing.T) {
	client, fake := newTestSDKKMSClient(t)
	enc, err := NewImageEncryptor(client)
	if err != nil {
		t.Fatalf("NewImageEncryptor: %v", err)
	}
	image := createTestImage(t)
	ctx := context.Background()

	encrypted, err := enc.EncryptImage(ctx, image, "img-key")
	if err != nil {
		t.Fatalf("EncryptImage: %v", err)
	}
	if fake.generated != 1 {
		t.Fatalf("generated = %d, want 1", fake.generated)
	}
	req := fake.genReqs[0]
	if req.Algorithm != kms.AlgorithmAES256GCM {
		t.Errorf("GenerateKey algorithm = %v, want AES-256-GCM", req.Algorithm)
	}
	if len(req.Usage) != 4 {
		t.Errorf("GenerateKey usage = %v, want four usages", req.Usage)
	}
	for i, layer := range encrypted.EncryptedLayers {
		if bytes.Equal(layer.Encrypted, image.Layers[i].Content) {
			t.Fatalf("layer %d left in plaintext", i)
		}
	}

	decrypted, err := enc.DecryptImage(ctx, encrypted)
	if err != nil {
		t.Fatalf("DecryptImage: %v", err)
	}
	if len(decrypted.Layers) != len(image.Layers) {
		t.Fatalf("decrypted %d layers, want %d", len(decrypted.Layers), len(image.Layers))
	}
	for i := range image.Layers {
		if !bytes.Equal(decrypted.Layers[i].Content, image.Layers[i].Content) {
			t.Errorf("layer %d = %q, want %q", i, decrypted.Layers[i].Content, image.Layers[i].Content)
		}
	}

	// A second image under the same key finds the key it created.
	second, err := enc.EncryptImage(ctx, createTestImage(t), "img-key")
	if err != nil {
		t.Fatalf("second EncryptImage: %v", err)
	}
	if fake.generated != 1 {
		t.Fatalf("generated = %d after the second image, want 1", fake.generated)
	}
	if _, err := enc.DecryptImage(ctx, second); err != nil {
		t.Fatalf("DecryptImage second: %v", err)
	}
}

func TestSDKKMSClient_GenerateKeyAlreadyExists(t *testing.T) {
	client, _ := newTestSDKKMSClient(t)
	ctx := context.Background()
	usage := []string{"encrypt", "decrypt"}
	if _, err := client.GenerateKey(ctx, "container-images/dup", DefaultKeyAlgorithm, usage); err != nil {
		t.Fatalf("first GenerateKey: %v", err)
	}
	_, err := client.GenerateKey(ctx, "container-images/dup", DefaultKeyAlgorithm, usage)
	var exists KeyAlreadyExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("second GenerateKey error = %v, want KeyAlreadyExistsError", err)
	}
	if exists.KeyID != "container-images/dup" {
		t.Errorf("KeyID = %q", exists.KeyID)
	}
}

func TestSDKKMSClient_WrongAADFails(t *testing.T) {
	client, _ := newTestSDKKMSClient(t)
	ctx := context.Background()
	keyID, err := client.GenerateKey(ctx, "aad", DefaultKeyAlgorithm, []string{"wrap", "unwrap"})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	wrapped, version, err := client.Encrypt(ctx, keyID, []byte("dek-bytes"), []byte("sha256:a"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if version != 1 {
		t.Errorf("version = %d, want 1", version)
	}
	got, err := client.Decrypt(ctx, keyID, wrapped, []byte("sha256:a"))
	if err != nil || string(got) != "dek-bytes" {
		t.Fatalf("Decrypt = %q, %v; want dek-bytes", got, err)
	}
	if _, err := client.Decrypt(ctx, keyID, wrapped, []byte("sha256:b")); err == nil {
		t.Fatal("Decrypt with a different AAD succeeded")
	}
}

func TestSDKKMSClient_GetKey(t *testing.T) {
	client, fake := newTestSDKKMSClient(t)
	ctx := context.Background()

	_, err := client.GetKey(ctx, "missing")
	var notFound KeyNotFoundError
	if !errors.As(err, &notFound) || notFound.KeyID != "missing" {
		t.Fatalf("GetKey(missing) = %v, want KeyNotFoundError", err)
	}

	keyID, err := client.GenerateKey(ctx, "known", DefaultKeyAlgorithm, []string{"ENCRYPT", "Decrypt"})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	info, err := client.GetKey(ctx, keyID)
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if info.KeyID != keyID || info.Version != 1 || info.CreatedAt.IsZero() {
		t.Errorf("GetKey info = %+v", info)
	}
	if info.Algorithm != kms.AlgorithmAES256GCM.String() {
		t.Errorf("Algorithm = %q, want %q", info.Algorithm, kms.AlgorithmAES256GCM.String())
	}
	if len(info.Usage) != 2 || info.Usage[0] != "encrypt" || info.Usage[1] != "decrypt" {
		t.Errorf("Usage = %v, want [encrypt decrypt]", info.Usage)
	}

	fake.getErr = errFakeKMSUnavailable
	_, err = client.GetKey(ctx, keyID)
	if !errors.Is(err, errFakeKMSUnavailable) || errors.As(err, &notFound) {
		t.Fatalf("GetKey with a failing KMS = %v, want the wrapped SDK error", err)
	}
}

func TestSDKKMSClient_TransientLookupCreatesNoKey(t *testing.T) {
	client, fake := newTestSDKKMSClient(t)
	fake.getErr = errFakeKMSUnavailable
	enc, err := NewImageEncryptor(client)
	if err != nil {
		t.Fatalf("NewImageEncryptor: %v", err)
	}
	_, err = enc.EncryptImage(context.Background(), createTestImage(t), "img-key")
	if err == nil {
		t.Fatal("EncryptImage succeeded with a failing key lookup")
	}
	if !errors.Is(err, errFakeKMSUnavailable) {
		t.Errorf("EncryptImage error = %v, want the lookup failure", err)
	}
	if fake.generated != 0 {
		t.Fatalf("generated = %d after a failed lookup, want 0", fake.generated)
	}
}

func TestSDKKMSClient_GenerateKeyRefusesUnsupported(t *testing.T) {
	client, fake := newTestSDKKMSClient(t)
	ctx := context.Background()
	var cfgErr InvalidConfigError

	_, err := client.GenerateKey(ctx, "k", "RSA-2048", []string{"encrypt"})
	if !errors.As(err, &cfgErr) || cfgErr.Field != "algorithm" {
		t.Fatalf("GenerateKey(RSA-2048) = %v, want InvalidConfigError on algorithm", err)
	}
	_, err = client.GenerateKey(ctx, "k", DefaultKeyAlgorithm, []string{"encrypt", "sign"})
	if !errors.As(err, &cfgErr) || cfgErr.Field != "usage" {
		t.Fatalf("GenerateKey(usage sign) = %v, want InvalidConfigError on usage", err)
	}
	if fake.generated != 0 {
		t.Fatalf("generated = %d, want 0", fake.generated)
	}
}

func TestNewSDKKMSClient_RequiresBothClients(t *testing.T) {
	fake := newFakeSDKEncryptionKMS()
	var nilErr NilClientError
	if _, err := NewSDKKMSClient(nil, fake); !errors.As(err, &nilErr) || nilErr.Client != "kms" {
		t.Errorf("NewSDKKMSClient(nil, crypto) = %v, want NilClientError{kms}", err)
	}
	if _, err := NewSDKKMSClient(fake, nil); !errors.As(err, &nilErr) || nilErr.Client != "crypto" {
		t.Errorf("NewSDKKMSClient(keys, nil) = %v, want NilClientError{crypto}", err)
	}
}

func TestSDKKMSClient_CancelledContext(t *testing.T) {
	client, fake := newTestSDKKMSClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := client.GenerateKey(ctx, "k", DefaultKeyAlgorithm, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("GenerateKey = %v, want context.Canceled", err)
	}
	if _, err := client.GetKey(ctx, "k"); !errors.Is(err, context.Canceled) {
		t.Errorf("GetKey = %v, want context.Canceled", err)
	}
	if _, _, err := client.Encrypt(ctx, "k", []byte("x"), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Encrypt = %v, want context.Canceled", err)
	}
	if _, err := client.Decrypt(ctx, "k", []byte("x"), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Decrypt = %v, want context.Canceled", err)
	}
	if fake.generated != 0 {
		t.Fatalf("generated = %d, want 0", fake.generated)
	}
}
