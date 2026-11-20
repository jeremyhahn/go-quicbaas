package service

import (
	"context"
	stderrors "errors"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// kvDataPlane is the slice of KV operations the kv service's Put, Get,
// Delete and List perform. go-qrdb's tenant-scoped KVServicer satisfies it
// directly; the shared node client is adapted by sdkKVDataPlane.
type kvDataPlane interface {
	Put(ctx context.Context, key string, value []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	Scan(ctx context.Context, prefix string) (map[string][]byte, error)
}

// sdkKVDataPlane adapts the qrdb SDK client's KV surface to kvDataPlane,
// calling it with no options exactly as the kv service always has.
type sdkKVDataPlane struct {
	kv qrdbtransport.KVClient
}

func (s sdkKVDataPlane) Put(ctx context.Context, key string, value []byte) error {
	return s.kv.Put(ctx, key, value)
}

func (s sdkKVDataPlane) Get(ctx context.Context, key string) ([]byte, error) {
	return s.kv.Get(ctx, key)
}

func (s sdkKVDataPlane) Delete(ctx context.Context, key string) error {
	return s.kv.Delete(ctx, key)
}

func (s sdkKVDataPlane) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	return s.kv.Scan(ctx, prefix)
}

// kvDatabaseDefault is the database the registry resolves when the request
// names none: nothing in the request context carries a database, so every
// kv operation addresses the tenant's default database.
const kvDatabaseDefault = ""

// errKVRegistryUnavailable is returned by dataPlane when a qrdb service
// registry provider is configured but has none to give yet (the node is
// sealed or its server is not running).
var errKVRegistryUnavailable = stderrors.New("qrdb service registry unavailable: the node is sealed or not serving")

// kvServiceRegistry returns the qrdb service registry the kv service routes
// through and whether a provider is configured at all. The provider is
// called on every operation and its current answer is returned, never a
// cached one: go-qrdb replaces its server, and with it the registry, during
// a system encryption migration, and a provider that returns nil (sealed or
// recycling) must make the operation fail closed rather than reach a
// stopped service graph.
func (s *quicBaaS) kvServiceRegistry() (qrdbservice.Registry, bool) {
	if s.qrdbServiceRegistry == nil {
		return nil, false
	}
	reg := s.qrdbServiceRegistry()
	if isNilInterface(reg) {
		return nil, true
	}
	return reg, true
}

// dataPlane returns the KV surface Put, Get, Delete and List use. When a
// qrdb service registry provider is configured it is the request tenant's
// scoped KVServicer, so the tenant's isolation backend, barrier and quotas
// apply; if the provider has no registry yet, the operation fails closed
// with an ErrUnavailable QBError and never touches the shared client. With
// no provider configured it is the shared qrdb client for shardID, as before.
func (k *kvService) dataPlane(ctx context.Context, op string, shardID uint64) (kvDataPlane, error) {
	reg, configured := k.svc.kvServiceRegistry()
	if !configured {
		client, err := k.svc.qrdbForShard(shardID)
		if err != nil {
			return nil, err
		}
		return sdkKVDataPlane{kv: client.KV()}, nil
	}
	if reg == nil {
		return nil, &errors.QBError{Code: errors.ErrUnavailable, Op: op, Err: errKVRegistryUnavailable}
	}
	tenantID, _ := qrdbsdk.TenantFromContext(ctx)
	kv, err := reg.KVService(qrdbsdk.NormalizeTenantID(tenantID), kvDatabaseDefault)
	if err != nil {
		return nil, err
	}
	if isNilInterface(kv) {
		return nil, &errors.QBError{Code: errors.ErrUnavailable, Op: op, Err: stderrors.New("qrdb service registry returned no KV service")}
	}
	return kv, nil
}

// kvDataPlaneError converts a dataPlane error for op: a QBError with Code
// ErrUnavailable (the fail-closed registry case) passes through unchanged;
// anything else, including a QBError with another code (such as the
// ErrInternal qrdbForShard returns when no client is configured), is a Raft
// error as the shared-client path always reported.
func kvDataPlaneError(op string, err error) error {
	var qbe *errors.QBError
	if stderrors.As(err, &qbe) && qbe.Code == errors.ErrUnavailable {
		return qbe
	}
	return &errors.QBError{Code: errors.ErrRaft, Op: op, Err: err}
}

// isKVKeyNotFound reports whether err is a key-not-found from either the
// SDK transport or a scoped KVServicer.
func isKVKeyNotFound(err error) bool {
	var transportKNF *qrdbtransport.KeyNotFoundError
	if stderrors.As(err, &transportKNF) {
		return true
	}
	var serviceKNF *qrdbservice.KeyNotFoundError
	return stderrors.As(err, &serviceKNF)
}
