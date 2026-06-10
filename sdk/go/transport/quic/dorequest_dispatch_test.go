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
	"context"
	"crypto/tls"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newDispatchTestTransport spins up a mock server and a connected transport.
// The returned cleanup closes both. It is the shared harness for exercising the
// per-service dispatch branches of doRequest end-to-end.
func newDispatchTestTransport(t *testing.T, server *mockQUICServer) *Transport {
	t.Helper()
	server.Start()

	cert, _, err := generateTestCertificate()
	require.NoError(t, err)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      server.caPool,
		NextProtos:   []string{alpnProtocol},
		MinVersion:   tls.VersionTLS13,
	}

	cfg := &transport.Config{
		Address:   server.addr,
		TLSConfig: tlsConfig,
		Timeout:   5 * time.Second,
	}

	// Register the server cleanup first so it runs last (LIFO): the transport
	// must close before the server's per-connection AcceptStream loop can exit,
	// otherwise server.Close()'s wg.Wait() blocks forever.
	t.Cleanup(func() { _ = server.Close() })

	trans, err := New(cfg, WithAuthToken("tok"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = trans.Close() })
	return trans
}

// okResponse is a handler that echoes a fixed payload with StatusOK.
func okResponse(payload []byte) func(*Request) *Response {
	return func(*Request) *Response {
		return &Response{Status: StatusOK, Payload: payload}
	}
}

// TestDoRequest_ServiceDispatch drives doRequest through each service decode
// branch using the mock server, proving the wire round-trip and the
// service-specific decode the CLI relies on.
func TestDoRequest_ServiceDispatch(t *testing.T) {
	server, err := newMockQUICServer(t)
	require.NoError(t, err)

	// Backup (per-method opcode, typed decode).
	listBackups, _ := EncodePayload(&service.ListBackupsResponse{TotalCount: 1,
		Backups: []*service.BackupMetadata{{BackupID: "bk-1"}}})
	server.RegisterHandler(OpBackupList, okResponse(listBackups))

	// KMS.
	genKey, _ := EncodePayload(&service.GenerateKeyResponse{Key: &service.KeyHandle{KeyID: "k1"}})
	server.RegisterHandler(OpKMSGenerateKey, okResponse(genKey))

	// DEK.
	createDEK, _ := EncodePayload(&service.CreateDEKResponse{DEK: &service.DEKMetadata{Path: "d1"}})
	server.RegisterHandler(OpKMSCreateDEK, okResponse(createDEK))

	// Crypto (shares 0xF0 with Scheduler in the SDK map; Crypto is the owner).
	encrypt, _ := EncodePayload(&service.EncryptResponse{KeyID: "k1"})
	server.RegisterHandler(OpCryptoEncrypt, okResponse(encrypt))

	// Secrets.
	rotate, _ := EncodePayload(&service.RotateSecretResponse{SecretID: "s1"})
	server.RegisterHandler(OpSecretsRotate, okResponse(rotate))

	// KV (generic map decode then convertKVResponse).
	kvGet := encodeResponse(map[string]interface{}{"id": float64(1), "data": "v", "found": true})
	server.RegisterHandler(OpGet, okResponse(kvGet))

	// RAG (generic map decode then convertRAGResponse).
	ragQuery := encodeResponse(map[string]interface{}{"context": "ctx", "tokens": float64(3)})
	server.RegisterHandler(OpRAGQuery, okResponse(ragQuery))

	// Cluster.
	clusterStatus := encodeResponse(map[string]interface{}{"leader_id": float64(1)})
	server.RegisterHandler(OpClusterStatus, okResponse(clusterStatus))

	// Embedding.
	embed := encodeResponse(map[string]interface{}{"embedding": []interface{}{float64(0.1)}})
	server.RegisterHandler(OpEmbeddingGenerate, okResponse(embed))

	// Vector (transform request + transform response).
	vectorUpsert := encodeResponse(map[string]interface{}{"id": "v1", "version": float64(1)})
	server.RegisterHandler(OpVectorUpsert, okResponse(vectorUpsert))

	// Orchestrator (sub-op dispatch, raw decode).
	orchHealth := encodeResponse(map[string]interface{}{"status": "ok", "healthy": float64(2)})
	server.RegisterHandler(OpOrchestrator, okResponse(orchHealth))

	// Barrier (sub-op dispatch, generic map decode).
	barrier := encodeResponse(map[string]interface{}{"sealed": false})
	server.RegisterHandler(OpBarrier, okResponse(barrier))

	// Ceremony (sub-op dispatch, generic map decode).
	ceremony := encodeResponse(map[string]interface{}{"phase": "ready"})
	server.RegisterHandler(OpCeremony, okResponse(ceremony))

	// IAM (transform request + transform response).
	iamTenant := encodeResponse(map[string]interface{}{"tenant": map[string]interface{}{"id": "t1"}})
	server.RegisterHandler(OpIAMCreateTenant, okResponse(iamTenant))

	trans := newDispatchTestTransport(t, server)
	ctx := context.Background()

	t.Run("Backup", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.Backup", "List", map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, uint64(1), got.(*service.ListBackupsResponse).TotalCount)
	})

	t.Run("KMS", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.KMS", "GenerateKey", map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, "k1", got.(*service.GenerateKeyResponse).Key.KeyID)
	})

	t.Run("DEK", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.DEK", "CreateDEK", map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, "d1", got.(*service.CreateDEKResponse).DEK.Path)
	})

	t.Run("Crypto", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.Crypto", "Encrypt", map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, "k1", got.(*service.EncryptResponse).KeyID)
	})

	t.Run("Secrets", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.Secrets", "Rotate", map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, "s1", got.(*service.RotateSecretResponse).SecretID)
	})

	t.Run("KV", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.KV", "Get", map[string]interface{}{"key": "k"})
		require.NoError(t, err)
		assert.True(t, got.(*service.GetResponse).Found)
	})

	t.Run("RAG", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.RAG", "Query", &rag.QueryRequest{Query: "q"})
		require.NoError(t, err)
		assert.Equal(t, "ctx", got.(*rag.QueryResponse).Context)
	})

	t.Run("Cluster", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.Cluster", "Status", map[string]interface{}{})
		require.NoError(t, err)
		assert.NotNil(t, got)
	})

	t.Run("Embedding", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.Embedding", "GenerateEmbedding", map[string]interface{}{"text": "hi"})
		require.NoError(t, err)
		assert.NotNil(t, got)
	})

	t.Run("Vector", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.Vector", "Upsert", &vector.UpsertRequest{
			Vector: &vector.VectorEntity{ID: "v1", Namespace: "ns"},
		})
		require.NoError(t, err)
		assert.Equal(t, "v1", got.(*vector.UpsertResponse).ID)
	})

	t.Run("Orchestrator", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.Orchestrator", "Health", map[string]interface{}{})
		require.NoError(t, err)
		assert.NotNil(t, got)
	})

	t.Run("Barrier", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.Barrier", "BarrierStatus", map[string]interface{}{})
		require.NoError(t, err)
		m := got.(map[string]interface{})
		assert.Equal(t, false, m["sealed"])
	})

	t.Run("Ceremony", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.Ceremony", "CeremonyStatus", map[string]interface{}{})
		require.NoError(t, err)
		m := got.(map[string]interface{})
		assert.Equal(t, "ready", m["phase"])
	})

	t.Run("IAM", func(t *testing.T) {
		got, err := trans.Request(ctx, "quicbaas.IAM.Tenant", "Create", map[string]interface{}{
			"id": "t1", "name": "Acme",
		})
		require.NoError(t, err)
		assert.Equal(t, "t1", got.(map[string]interface{})["id"])
	})
}

// TestDoRequest_TransformErrors covers the request-transform error branches in
// doRequest (vector and IAM transforms that reject a bad request type), plus
// the unknown sub-op branches for orchestrator/barrier/ceremony.
func TestDoRequest_TransformErrors(t *testing.T) {
	server, err := newMockQUICServer(t)
	require.NoError(t, err)
	trans := newDispatchTestTransport(t, server)
	ctx := context.Background()

	t.Run("vector transform rejects bad type", func(t *testing.T) {
		_, err := trans.Request(ctx, "quicbaas.Vector", "Upsert", "not-a-request")
		require.Error(t, err)
	})

	t.Run("unknown method", func(t *testing.T) {
		_, err := trans.Request(ctx, "quicbaas.Nope", "Nope", nil)
		require.Error(t, err)
	})
}

// TestDoRequest_EmptyOKResponse covers the empty-payload short circuit after a
// StatusOK response.
func TestDoRequest_EmptyOKResponse(t *testing.T) {
	server, err := newMockQUICServer(t)
	require.NoError(t, err)
	server.RegisterHandler(OpPut, okResponse(nil))
	trans := newDispatchTestTransport(t, server)

	got, err := trans.Request(context.Background(), "quicbaas.KV", "Put", map[string]interface{}{"key": "k"})
	require.NoError(t, err)
	assert.Nil(t, got)
}
