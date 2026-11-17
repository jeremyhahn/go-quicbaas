package service

import (
	"context"
	stderrors "errors"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
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

// dataPlane returns the KV surface Put, Get, Delete and List use. When a
// qrdb service registry is wired it is the request tenant's scoped
// KVServicer, so the tenant's isolation backend, barrier and quotas apply;
// otherwise it is the shared qrdb client for shardID, as before.
func (k *kvService) dataPlane(ctx context.Context, shardID uint64) (kvDataPlane, error) {
	if reg := k.svc.qrdbServiceRegistry; reg != nil {
		tenantID, _ := qrdbsdk.TenantFromContext(ctx)
		kv, err := reg.KVService(qrdbsdk.NormalizeTenantID(tenantID), kvDatabaseDefault)
		if err != nil {
			return nil, err
		}
		if kv == nil {
			return nil, stderrors.New("qrdb service registry returned no KV service")
		}
		return kv, nil
	}
	client, err := k.svc.qrdbForShard(shardID)
	if err != nil {
		return nil, err
	}
	return sdkKVDataPlane{kv: client.KV()}, nil
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
