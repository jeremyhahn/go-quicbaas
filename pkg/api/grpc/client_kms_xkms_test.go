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
	stderrors "errors"
	"net"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const xkmsKeystoreBufSize = 1024 * 1024

// recordingKeystoreServer is an xkms.v1 KeystoreService that records the
// requests it receives and answers with a fixed result or error.
type recordingKeystoreServer struct {
	xkmssdk.ProtoUnimplementedKeystoreServiceServer

	mu          sync.Mutex
	generateReq *xkmssdk.ProtoGenerateKeyRequest
	listReq     *xkmssdk.ProtoListKeysRequest
	deleteReq   *xkmssdk.ProtoDeleteKeyRequest

	err      error
	listKeys []*xkmssdk.ProtoKeyInfo
	total    int32
}

func (s *recordingKeystoreServer) GenerateKey(_ context.Context, req *xkmssdk.ProtoGenerateKeyRequest) (*xkmssdk.ProtoGenerateKeyResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generateReq = req
	if req.GetKeyId() == "" || req.GetBackend() == "" || req.GetKeyType() == "" || req.GetAlgorithm() == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id, backend, key_type and algorithm are required")
	}
	if s.err != nil {
		return nil, s.err
	}
	return &xkmssdk.ProtoGenerateKeyResponse{
		KeyId:     req.GetKeyId(),
		KeyType:   req.GetKeyType(),
		CreatedAt: timestamppb.Now(),
	}, nil
}

func (s *recordingKeystoreServer) ListKeys(_ context.Context, req *xkmssdk.ProtoListKeysRequest) (*xkmssdk.ProtoListKeysResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listReq = req
	if req.GetBackend() == "" {
		return nil, status.Error(codes.InvalidArgument, "backend is required")
	}
	if s.err != nil {
		return nil, s.err
	}
	total := s.total
	if total == 0 {
		total = int32(len(s.listKeys))
	}
	return &xkmssdk.ProtoListKeysResponse{Keys: xkmsPage(s.listKeys, int(req.GetLimit()), int(req.GetOffset())), Total: total}, nil
}

// xkmsPage pages keys the way the pinned go-xkms gRPC ListKeys handler does:
// grpcPageRequest turns limit/offset into page = offset/limit + 1, a zero
// limit lists every key, and transport.PageRequest.Normalize caps the page
// size at transport.MaxPageSize (1000).
func xkmsPage(keys []*xkmssdk.ProtoKeyInfo, limit, offset int) []*xkmssdk.ProtoKeyInfo {
	if limit <= 0 {
		return keys
	}
	page := offset/limit + 1
	size := min(limit, 1000)
	start := (page - 1) * size
	if start >= len(keys) {
		return nil
	}
	return keys[start:min(start+size, len(keys))]
}

func (s *recordingKeystoreServer) DeleteKey(_ context.Context, req *xkmssdk.ProtoDeleteKeyRequest) (*xkmssdk.ProtoDeleteKeyResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteReq = req
	if req.GetKeyId() == "" || req.GetBackend() == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id and backend are required")
	}
	if s.err != nil {
		return nil, s.err
	}
	return &xkmssdk.ProtoDeleteKeyResponse{Success: true}, nil
}

// newXKMSKeystoreClient serves fake over bufconn with ONLY the xkms.v1
// KeystoreService registered, as the node does, and returns a connected
// Client.
func newXKMSKeystoreClient(t *testing.T, fake *recordingKeystoreServer) *Client {
	t.Helper()
	lis := bufconn.Listen(xkmsKeystoreBufSize)
	server := grpc.NewServer()
	xkmssdk.ProtoRegisterKeystoreServiceServer(server, fake)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, conn.Close())
		server.Stop()
		require.NoError(t, <-serveDone)
	})
	return &Client{conn: conn, connected: true, logger: log}
}

// TestClientKMSGenerateKey_XKMSKeystoreNonDefaultBackend: service.DeleteKeyRequest
// names no backend, so KMSDeleteKey can only reach the default one. A key
// generated in any other backend could never be deleted again, so the client
// refuses to create it.
func TestClientKMSGenerateKey_XKMSKeystoreNonDefaultBackend(t *testing.T) {
	fake := &recordingKeystoreServer{}
	client := newXKMSKeystoreClient(t, fake)

	resp, err := client.KMSGenerateKey(context.Background(), &service.GenerateKeyRequest{
		Name: "hsm-key",
		KeyAttributes: &xkmssdk.KeyAttributes{
			StoreType:          xkmssdk.StorePKCS11,
			SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr), "error must be a typed QBError, got %T", err)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	assert.Contains(t, err.Error(), string(xkmssdk.StorePKCS11))

	fake.mu.Lock()
	assert.Nil(t, fake.generateReq, "no key may be generated in a backend KMSDeleteKey cannot reach")
	fake.mu.Unlock()
}

func TestClientKMSListKeys_XKMSKeystore(t *testing.T) {
	fake := &recordingKeystoreServer{
		listKeys: []*xkmssdk.ProtoKeyInfo{
			{KeyId: "k1", KeyType: "SECRET", Algorithm: "aes256-gcm", KeyVersion: 2},
			{KeyId: "k2", KeyType: "SIGNING", Algorithm: "ECDSA", Curve: "P-384", KeyVersion: 1},
			{KeyId: "k3", KeyType: "SIGNING", Algorithm: "RSA", KeySize: 4096, KeyVersion: 1},
			{KeyId: "k4", KeyType: "SIGNING", Algorithm: "Ed25519", KeyVersion: 1},
			{KeyId: "k5", KeyType: "SIGNING", Algorithm: "ML-DSA-65", KeyVersion: 1},
		},
		total: 9,
	}
	client := newXKMSKeystoreClient(t, fake)

	resp, err := client.KMSListKeys(context.Background(), &service.ListKeysRequest{Limit: 5})
	require.NoError(t, err)

	fake.mu.Lock()
	got := fake.listReq
	fake.mu.Unlock()
	require.NotNil(t, got)
	assert.Equal(t, int32(5), got.GetLimit())
	assert.Equal(t, int32(0), got.GetOffset())
	assert.Equal(t, "software", got.GetBackend())

	require.Len(t, resp.Keys, 5)
	assert.Equal(t, "k1", resp.Keys[0].KeyID)
	assert.Equal(t, uint64(2), resp.Keys[0].Version)
	assert.Equal(t, service.KeyAlgAES256GCM, resp.Keys[0].Algorithm)
	assert.Equal(t, service.KeyAlgECDSAP384, resp.Keys[1].Algorithm)
	assert.Equal(t, service.KeyAlgRSA4096, resp.Keys[2].Algorithm)
	assert.Equal(t, service.KeyAlgEd25519, resp.Keys[3].Algorithm)
	// An algorithm the client has no name for keeps the name xkms reports.
	assert.Equal(t, service.KeyAlgorithm("ML-DSA-65"), resp.Keys[4].Algorithm)
	assert.Equal(t, 9, resp.TotalCount)
	assert.True(t, resp.HasMore)
	assert.Equal(t, "5", resp.NextCursor)

	// The cursor is the xkms.v1 offset of the next page.
	// The default backend, named in the filter, is the one listed.
	_, err = client.KMSListKeys(context.Background(), &service.ListKeysRequest{
		Limit:  5,
		Cursor: resp.NextCursor,
		Filter: map[string]string{"backend": string(xkmssdk.StoreSoftware)},
	})
	require.NoError(t, err)
	fake.mu.Lock()
	assert.Equal(t, int32(5), fake.listReq.GetOffset())
	assert.Equal(t, string(xkmssdk.StoreSoftware), fake.listReq.GetBackend())
	fake.mu.Unlock()
}

// TestClientKMSListKeys_XKMSKeystoreUnappliedFilter verifies that a filter the
// xkms.v1 ListKeys handler does not apply is refused before any request is
// sent: the server would answer with unfiltered keys, totals and cursors.
func TestClientKMSListKeys_XKMSKeystoreUnappliedFilter(t *testing.T) {
	tests := []struct {
		name   string
		filter map[string]string
		key    string
	}{
		{name: "prefix", filter: map[string]string{"prefix": "app-"}, key: "prefix"},
		{name: "partition", filter: map[string]string{"partition": "p1"}, key: "partition"},
		{name: "non-default backend", filter: map[string]string{"backend": string(xkmssdk.StorePKCS11)}, key: "backend"},
		{name: "unknown key", filter: map[string]string{"owner": "alice"}, key: "owner"},
		{name: "prefix beside the default backend", filter: map[string]string{"backend": "software", "prefix": "app-"}, key: "prefix"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &recordingKeystoreServer{
				listKeys: []*xkmssdk.ProtoKeyInfo{
					{KeyId: "app-1", KeyType: "SECRET", Algorithm: "aes256-gcm", KeyVersion: 1},
					{KeyId: "other-1", KeyType: "SECRET", Algorithm: "aes256-gcm", KeyVersion: 1},
				},
				total: 2,
			}
			client := newXKMSKeystoreClient(t, fake)

			resp, err := client.KMSListKeys(context.Background(), &service.ListKeysRequest{Filter: tt.filter})
			if resp != nil {
				ids := make([]string, len(resp.Keys))
				for i, k := range resp.Keys {
					ids[i] = k.KeyID
				}
				t.Logf("unfiltered keys returned: %v (total %d)", ids, resp.TotalCount)
			}
			require.Error(t, err)
			var filterErr *KMSListKeysFilterUnsupportedError
			require.True(t, stderrors.As(err, &filterErr), "got %T: %v", err, err)
			assert.Equal(t, tt.key, filterErr.Key)
			assert.Equal(t, tt.filter[tt.key], filterErr.Value)
			assert.Contains(t, err.Error(), tt.key)
			assert.Contains(t, err.Error(), "xkms.v1")
			var qbErr *errors.QBError
			require.True(t, stderrors.As(err, &qbErr), "error must be a typed QBError, got %T", err)
			assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)

			fake.mu.Lock()
			assert.Nil(t, fake.listReq, "no ListKeys request may be sent")
			fake.mu.Unlock()
		})
	}
}

// TestClientKMSListKeys_XKMSKeystoreAppliedFilter verifies that empty filter
// values count as absent and that the default backend, the one filter the
// xkms.v1 ListKeys handler applies, is forwarded.
func TestClientKMSListKeys_XKMSKeystoreAppliedFilter(t *testing.T) {
	tests := []struct {
		name   string
		filter map[string]string
	}{
		{name: "empty prefix", filter: map[string]string{"prefix": ""}},
		{name: "empty values", filter: map[string]string{"prefix": "", "partition": "", "backend": "", "owner": ""}},
		{name: "default backend", filter: map[string]string{"backend": "software"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &recordingKeystoreServer{
				listKeys: []*xkmssdk.ProtoKeyInfo{
					{KeyId: "app-1", KeyType: "SECRET", Algorithm: "aes256-gcm", KeyVersion: 1},
					{KeyId: "other-1", KeyType: "SECRET", Algorithm: "aes256-gcm", KeyVersion: 1},
				},
				total: 2,
			}
			client := newXKMSKeystoreClient(t, fake)

			resp, err := client.KMSListKeys(context.Background(), &service.ListKeysRequest{Filter: tt.filter})
			require.NoError(t, err)
			require.Len(t, resp.Keys, 2)
			assert.Equal(t, 2, resp.TotalCount)

			fake.mu.Lock()
			defer fake.mu.Unlock()
			require.NotNil(t, fake.listReq)
			assert.Equal(t, "software", fake.listReq.GetBackend())
			assert.Empty(t, fake.listReq.GetPartition())
		})
	}
}

func TestClientKMSListKeys_XKMSKeystoreErrors(t *testing.T) {
	t.Run("bad cursor", func(t *testing.T) {
		client := newXKMSKeystoreClient(t, &recordingKeystoreServer{})
		resp, err := client.KMSListKeys(context.Background(), &service.ListKeysRequest{Cursor: "not-a-number"})
		require.Error(t, err)
		assert.Nil(t, resp)
		var qbErr *errors.QBError
		require.True(t, stderrors.As(err, &qbErr), "error must be a typed QBError, got %T", err)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("server error", func(t *testing.T) {
		client := newXKMSKeystoreClient(t, &recordingKeystoreServer{err: status.Error(codes.PermissionDenied, "denied")})
		resp, err := client.KMSListKeys(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, resp)
		var qbErr *errors.QBError
		require.True(t, stderrors.As(err, &qbErr), "error must be a typed QBError, got %T", err)
		assert.Equal(t, errors.ErrForbidden, qbErr.Code)
	})
}

// TestClientKMSListKeys_XKMSKeystorePaging walks every page of a listing
// larger than the server's page-size cap: a limit above the cap must not make
// the client re-read a page or skip keys.
func TestClientKMSListKeys_XKMSKeystorePaging(t *testing.T) {
	const keyCount = 2500
	keys := make([]*xkmssdk.ProtoKeyInfo, keyCount)
	for i := range keys {
		keys[i] = &xkmssdk.ProtoKeyInfo{KeyId: "k" + strconv.Itoa(i), KeyType: "SECRET", Algorithm: "aes256-gcm"}
	}
	client := newXKMSKeystoreClient(t, &recordingKeystoreServer{listKeys: keys})

	seen := make(map[string]int, keyCount)
	cursor := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, keyCount, "the listing never ends")
		resp, err := client.KMSListKeys(context.Background(), &service.ListKeysRequest{Limit: 2000, Cursor: cursor})
		require.NoError(t, err)
		for _, key := range resp.Keys {
			seen[key.KeyID]++
		}
		if resp.NextCursor == "" {
			assert.False(t, resp.HasMore)
			break
		}
		cursor = resp.NextCursor
	}

	for _, key := range keys {
		assert.Equal(t, 1, seen[key.GetKeyId()], "key %s must be listed exactly once", key.GetKeyId())
	}
	assert.Len(t, seen, keyCount)
}

func TestClientKMSListKeys_XKMSKeystoreUnalignedCursor(t *testing.T) {
	tests := []struct {
		name   string
		limit  int
		cursor string
	}{
		{name: "offset not a multiple of the limit", limit: 300, cursor: "1000"},
		{name: "offset with an unpaginated listing", limit: 0, cursor: "10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &recordingKeystoreServer{}
			client := newXKMSKeystoreClient(t, fake)
			resp, err := client.KMSListKeys(context.Background(), &service.ListKeysRequest{Limit: tt.limit, Cursor: tt.cursor})
			require.Error(t, err)
			assert.Nil(t, resp)
			var qbErr *errors.QBError
			require.True(t, stderrors.As(err, &qbErr), "error must be a typed QBError, got %T", err)
			assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
			fake.mu.Lock()
			assert.Nil(t, fake.listReq, "an unaligned cursor must not reach the server")
			fake.mu.Unlock()
		})
	}
}

func TestClientKMSDeleteKey_XKMSKeystore(t *testing.T) {
	fake := &recordingKeystoreServer{}
	client := newXKMSKeystoreClient(t, fake)

	resp, err := client.KMSDeleteKey(context.Background(), &service.DeleteKeyRequest{KeyID: "k1", Force: true})
	require.NoError(t, err)
	assert.Equal(t, "k1", resp.KeyID)
	assert.Equal(t, service.KeyStatusDeleted, resp.Status)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.NotNil(t, fake.deleteReq)
	assert.Equal(t, "k1", fake.deleteReq.GetKeyId())
	assert.Equal(t, "software", fake.deleteReq.GetBackend())
}

func TestClientKMSDeleteKey_XKMSKeystoreNotFound(t *testing.T) {
	client := newXKMSKeystoreClient(t, &recordingKeystoreServer{err: status.Error(codes.NotFound, "no such key")})

	resp, err := client.KMSDeleteKey(context.Background(), &service.DeleteKeyRequest{KeyID: "missing"})
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr), "error must be a typed QBError, got %T", err)
	assert.Equal(t, errors.ErrNotFound, qbErr.Code)
}
