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

package service

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// svcBoostMockTransport implements transport.Transport, transport.KVClient,
// and transport.AdminClient for unit tests that need a qrdb SDK client
// returning canned Scan and GetTopology results.
type svcBoostMockTransport struct {
	putErr           error
	deleteErr        error
	batchErr         error
	getResult        []byte
	getErr           error
	scanResult       map[string][]byte
	scanErr          error
	topologyResult   *transport.TopologyResponse
	topologyErr      error
	shardStateResult *transport.ShardStateResponse
	shardStateErr    error
	addMemberErr     error
	removeMemberErr  error

	// Key capture fields for verifying key construction in tests.
	mu            sync.Mutex
	lastPutKey    string
	lastGetKey    string
	lastDeleteKey string
	lastScanKey   string
	// batchCalls records the ops passed to each ExecuteBatch invocation.
	batchCalls [][]transport.Op
	// txCalls records each ExecuteTransaction request; txResult and txErr
	// are its outcome (a nil txResult commits the Success branch).
	txCalls  []*transport.TransactionRequest
	txResult *transport.TransactionResponse
	txErr    error
}

func (t *svcBoostMockTransport) Connect(_ context.Context) error { return nil }
func (t *svcBoostMockTransport) Close() error                    { return nil }
func (t *svcBoostMockTransport) Healthy(_ context.Context) bool  { return true }
func (t *svcBoostMockTransport) Request(_ context.Context, _ string, _, _ any) error {
	return nil
}

// KVClient methods.

func (t *svcBoostMockTransport) Put(_ context.Context, key string, _ []byte, _ ...transport.WriteOption) error {
	t.mu.Lock()
	t.lastPutKey = key
	t.mu.Unlock()
	return t.putErr
}
func (t *svcBoostMockTransport) Get(_ context.Context, key string, _ ...transport.ReadOption) ([]byte, error) {
	t.mu.Lock()
	t.lastGetKey = key
	t.mu.Unlock()
	return t.getResult, t.getErr
}
func (t *svcBoostMockTransport) Delete(_ context.Context, key string, _ ...transport.WriteOption) error {
	t.mu.Lock()
	t.lastDeleteKey = key
	t.mu.Unlock()
	return t.deleteErr
}
func (t *svcBoostMockTransport) List(_ context.Context, _ string, _ ...transport.ReadOption) ([]string, error) {
	return nil, nil
}

func (t *svcBoostMockTransport) Scan(_ context.Context, prefix string, _ ...transport.ReadOption) (map[string][]byte, error) {
	t.mu.Lock()
	t.lastScanKey = prefix
	t.mu.Unlock()
	if t.scanErr != nil {
		return nil, t.scanErr
	}
	return t.scanResult, nil
}

func (t *svcBoostMockTransport) Exists(_ context.Context, _ string, _ ...transport.ReadOption) (bool, error) {
	return false, nil
}

// AdminClient methods.

func (t *svcBoostMockTransport) Health(_ context.Context) (*transport.HealthResponse, error) {
	return &transport.HealthResponse{Status: "ok"}, nil
}
func (t *svcBoostMockTransport) Status(_ context.Context) (*transport.StatusResponse, error) {
	return &transport.StatusResponse{}, nil
}
func (t *svcBoostMockTransport) Peers(_ context.Context) (*transport.PeersResponse, error) {
	return &transport.PeersResponse{}, nil
}
func (t *svcBoostMockTransport) AddMember(_ context.Context, _, _ uint64, _, _ string) error {
	return t.addMemberErr
}
func (t *svcBoostMockTransport) RemoveMember(_ context.Context, _, _ uint64) error {
	return t.removeMemberErr
}
func (t *svcBoostMockTransport) RequestSnapshot(_ context.Context, _ uint64) error { return nil }
func (t *svcBoostMockTransport) Drain(_ context.Context) error                     { return nil }
func (t *svcBoostMockTransport) Capabilities(_ context.Context) (*transport.CapabilitiesResponse, error) {
	return &transport.CapabilitiesResponse{}, nil
}
func (t *svcBoostMockTransport) StartShard(_ context.Context, _ *transport.StartShardRequest) error {
	return nil
}
func (t *svcBoostMockTransport) StopShard(_ context.Context, _ uint64) error { return nil }
func (t *svcBoostMockTransport) GetShardState(_ context.Context, shardID uint64) (*transport.ShardStateResponse, error) {
	if t.shardStateErr != nil {
		return nil, t.shardStateErr
	}
	if t.shardStateResult != nil {
		return t.shardStateResult, nil
	}
	return &transport.ShardStateResponse{
		ShardID:   shardID,
		LeaderID:  1,
		HasLeader: true,
	}, nil
}
func (t *svcBoostMockTransport) GetTopology(_ context.Context) (*transport.TopologyResponse, error) {
	if t.topologyErr != nil {
		return nil, t.topologyErr
	}
	return t.topologyResult, nil
}

// BatchClient methods.

func (t *svcBoostMockTransport) ExecuteBatch(_ context.Context, ops []transport.Op, _ ...transport.WriteOption) (*transport.BatchResponse, error) {
	t.mu.Lock()
	captured := make([]transport.Op, len(ops))
	copy(captured, ops)
	t.batchCalls = append(t.batchCalls, captured)
	t.mu.Unlock()
	if t.batchErr != nil {
		return nil, t.batchErr
	}
	resp := &transport.BatchResponse{
		Responses: make([]transport.OpResponse, len(ops)),
	}
	for i := range ops {
		resp.Responses[i] = transport.OpResponse{Succeeded: true}
	}
	return resp, nil
}

// TransactionClient methods.

func (t *svcBoostMockTransport) ExecuteTransaction(_ context.Context, req *transport.TransactionRequest, _ ...transport.WriteOption) (*transport.TransactionResponse, error) {
	t.mu.Lock()
	t.txCalls = append(t.txCalls, req)
	t.mu.Unlock()
	if t.txErr != nil {
		return nil, t.txErr
	}
	if t.txResult != nil {
		return t.txResult, nil
	}
	resp := &transport.TransactionResponse{
		Succeeded: true,
		Responses: make([]transport.OpResponse, len(req.SuccessOps)),
	}
	for i := range req.SuccessOps {
		resp.Responses[i] = transport.OpResponse{Succeeded: true}
	}
	return resp, nil
}

func (t *svcBoostMockTransport) ImportFile(_ context.Context, _ string, _ io.Reader, _ transport.ImportOptions) (*transport.ImportResult, error) {
	return nil, nil
}

// svcBoostMockQRDBProvider implements QRDBClientProvider for unit tests.
// It returns a qrdb SDK client backed by the mock transport above.
type svcBoostMockQRDBProvider struct {
	scanResult map[string][]byte
	scanErr    error
	client     *qrdbsdk.Client
	transport  *svcBoostMockTransport
	// lastShardID records the most recent shard ID passed to ForShard.
	lastShardID atomic.Uint64
	// shardMu guards shardCalls.
	shardMu sync.Mutex
	// shardCalls records every shard ID passed to ForShard, in order.
	shardCalls []uint64
}

// newSvcBoostMockQRDBProvider creates a QRDBClientProvider that returns
// canned Scan results. The scanData maps entity keys to their serialized
// JSON values, matching the map[string][]byte format returned by the
// real qrdb KV.Scan method.
func newSvcBoostMockQRDBProvider(scanData map[string][]byte, scanErr error) *svcBoostMockQRDBProvider {
	mockTransport := &svcBoostMockTransport{
		scanResult: scanData,
		scanErr:    scanErr,
	}
	client, err := qrdbsdk.NewClient(mockTransport)
	if err != nil {
		panic(fmt.Sprintf("failed to create mock qrdb client: %v", err))
	}
	return &svcBoostMockQRDBProvider{
		scanResult: scanData,
		scanErr:    scanErr,
		client:     client,
		transport:  mockTransport,
	}
}

// newSvcBoostMockQRDBProviderWithGet creates a QRDBClientProvider that returns
// canned Get results. This is used by tests that call client.KV().Get().
func newSvcBoostMockQRDBProviderWithGet(getData []byte, getErr error) *svcBoostMockQRDBProvider {
	mockTransport := &svcBoostMockTransport{
		getResult: getData,
		getErr:    getErr,
	}
	client, err := qrdbsdk.NewClient(mockTransport)
	if err != nil {
		panic(fmt.Sprintf("failed to create mock qrdb client: %v", err))
	}
	return &svcBoostMockQRDBProvider{
		client:    client,
		transport: mockTransport,
	}
}

// newSvcBoostMockQRDBProviderWithTopology creates a QRDBClientProvider that
// returns canned Scan results and a canned GetTopology response. This is
// used by ClusterStatus tests that call client.Admin().GetTopology().
func newSvcBoostMockQRDBProviderWithTopology(
	scanData map[string][]byte,
	scanErr error,
	topology *transport.TopologyResponse,
	topologyErr error,
) *svcBoostMockQRDBProvider {
	mockTransport := &svcBoostMockTransport{
		scanResult:     scanData,
		scanErr:        scanErr,
		topologyResult: topology,
		topologyErr:    topologyErr,
	}
	client, err := qrdbsdk.NewClient(mockTransport)
	if err != nil {
		panic(fmt.Sprintf("failed to create mock qrdb client: %v", err))
	}
	return &svcBoostMockQRDBProvider{
		scanResult: scanData,
		scanErr:    scanErr,
		client:     client,
		transport:  mockTransport,
	}
}

// newSvcBoostMockQRDBProviderWithErrors creates a QRDBClientProvider where
// Put, Delete, and ExecuteBatch return the specified errors. Use this for
// tests that verify error propagation through the SDK path.
func newSvcBoostMockQRDBProviderWithErrors(putErr, deleteErr, batchErr error) *svcBoostMockQRDBProvider {
	mockTransport := &svcBoostMockTransport{
		putErr:    putErr,
		deleteErr: deleteErr,
		batchErr:  batchErr,
	}
	client, err := qrdbsdk.NewClient(mockTransport)
	if err != nil {
		panic(fmt.Sprintf("failed to create mock qrdb client: %v", err))
	}
	return &svcBoostMockQRDBProvider{
		client:    client,
		transport: mockTransport,
	}
}

// newSvcBoostMockQRDBProviderFull creates a QRDBClientProvider with full
// control over all transport behaviors. This allows tests to configure
// Get results, Scan results, and error injection in a single mock.
func newSvcBoostMockQRDBProviderFull(t *svcBoostMockTransport) *svcBoostMockQRDBProvider {
	client, err := qrdbsdk.NewClient(t)
	if err != nil {
		panic(fmt.Sprintf("failed to create mock qrdb client: %v", err))
	}
	return &svcBoostMockQRDBProvider{
		scanResult: t.scanResult,
		scanErr:    t.scanErr,
		client:     client,
		transport:  t,
	}
}

func (p *svcBoostMockQRDBProvider) ForShard(shardID uint64) (*qrdbsdk.Client, error) {
	p.lastShardID.Store(shardID)
	p.shardMu.Lock()
	p.shardCalls = append(p.shardCalls, shardID)
	p.shardMu.Unlock()
	return p.client, nil
}

// capturedBatchCalls returns the ops passed to each ExecuteBatch call.
func (p *svcBoostMockQRDBProvider) capturedBatchCalls() [][]transport.Op {
	p.transport.mu.Lock()
	defer p.transport.mu.Unlock()
	calls := make([][]transport.Op, len(p.transport.batchCalls))
	copy(calls, p.transport.batchCalls)
	return calls
}

// capturedShardCalls returns every shard ID passed to ForShard, in order.
func (p *svcBoostMockQRDBProvider) capturedShardCalls() []uint64 {
	p.shardMu.Lock()
	defer p.shardMu.Unlock()
	calls := make([]uint64, len(p.shardCalls))
	copy(calls, p.shardCalls)
	return calls
}

func (p *svcBoostMockQRDBProvider) Close() error {
	return p.client.Close()
}

func (p *svcBoostMockQRDBProvider) IsEmbedded() bool { return true }

// capturedPutKey returns the last key passed to the Put method.
func (p *svcBoostMockQRDBProvider) capturedPutKey() string {
	p.transport.mu.Lock()
	defer p.transport.mu.Unlock()
	return p.transport.lastPutKey
}

// capturedGetKey returns the last key passed to the Get method.
func (p *svcBoostMockQRDBProvider) capturedGetKey() string {
	p.transport.mu.Lock()
	defer p.transport.mu.Unlock()
	return p.transport.lastGetKey
}

// capturedDeleteKey returns the last key passed to the Delete method.
func (p *svcBoostMockQRDBProvider) capturedDeleteKey() string {
	p.transport.mu.Lock()
	defer p.transport.mu.Unlock()
	return p.transport.lastDeleteKey
}

// capturedScanKey returns the last prefix passed to the Scan method.
func (p *svcBoostMockQRDBProvider) capturedScanKey() string {
	p.transport.mu.Lock()
	defer p.transport.mu.Unlock()
	return p.transport.lastScanKey
}

// sliceToCannedScanResult converts a [][]byte of serialized JSON entities
// into a map[string][]byte keyed by "entity-0", "entity-1", etc.
// This bridges the old raftNode.SyncRead test data format to the new
// qrdb KV.Scan format.
func sliceToCannedScanResult(data [][]byte) map[string][]byte {
	if data == nil {
		return nil
	}
	result := make(map[string][]byte, len(data))
	for i, d := range data {
		result[fmt.Sprintf("entity-%d", i)] = d
	}
	return result
}

// Compile-time interface checks
var _ transport.Transport = (*svcBoostMockTransport)(nil)
var _ transport.KVClient = (*svcBoostMockTransport)(nil)
var _ transport.AdminClient = (*svcBoostMockTransport)(nil)
var _ transport.BatchClient = (*svcBoostMockTransport)(nil)
var _ QRDBClientProvider = (*svcBoostMockQRDBProvider)(nil)
