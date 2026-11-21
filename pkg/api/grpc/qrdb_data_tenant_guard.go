// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// qrdbRegistryDataMethods is the set of registry-backed qrdb.v1.QRDBService
// data methods the server serves on the Unix socket through its data-registry
// proxy. go-qrdb's handlers resolve the tenant themselves and read an absent
// binding as the system tenant, so these methods need a bound tenant before
// they dispatch.
var qrdbRegistryDataMethods = map[string]bool{
	// KV
	"/qrdb.v1.QRDBService/Put":              true,
	"/qrdb.v1.QRDBService/PutIfAbsent":      true,
	"/qrdb.v1.QRDBService/CompareAndSwap":   true,
	"/qrdb.v1.QRDBService/Get":              true,
	"/qrdb.v1.QRDBService/Delete":           true,
	"/qrdb.v1.QRDBService/CompareAndDelete": true,
	"/qrdb.v1.QRDBService/List":             true,
	"/qrdb.v1.QRDBService/Scan":             true,
	// MVCC
	"/qrdb.v1.QRDBService/PutWithLease":      true,
	"/qrdb.v1.QRDBService/GetRevision":       true,
	"/qrdb.v1.QRDBService/GetAtRevision":     true,
	"/qrdb.v1.QRDBService/Range":             true,
	"/qrdb.v1.QRDBService/Compact":           true,
	"/qrdb.v1.QRDBService/CurrentRevision":   true,
	"/qrdb.v1.QRDBService/RestoreToRevision": true,
	// Transaction, batch
	"/qrdb.v1.QRDBService/Transaction": true,
	"/qrdb.v1.QRDBService/Batch":       true,
	"/qrdb.v1.QRDBService/BatchImport": true,
	// Index
	"/qrdb.v1.QRDBService/IndexCreate": true,
	"/qrdb.v1.QRDBService/IndexDrop":   true,
	"/qrdb.v1.QRDBService/IndexQuery":  true,
	"/qrdb.v1.QRDBService/IndexScan":   true,
	"/qrdb.v1.QRDBService/IndexList":   true,
	// Timeseries
	"/qrdb.v1.QRDBService/TSWrite":        true,
	"/qrdb.v1.QRDBService/TSWriteBatch":   true,
	"/qrdb.v1.QRDBService/TSQuery":        true,
	"/qrdb.v1.QRDBService/TSAggregate":    true,
	"/qrdb.v1.QRDBService/TSWritePoint":   true,
	"/qrdb.v1.QRDBService/TSRichQuery":    true,
	"/qrdb.v1.QRDBService/TSDeleteSeries": true,
	"/qrdb.v1.QRDBService/TSGetRetention": true,
	"/qrdb.v1.QRDBService/TSSetRetention": true,
	"/qrdb.v1.QRDBService/TSListMetrics":  true,
	// Vector
	"/qrdb.v1.QRDBService/VectorCreateNamespace":       true,
	"/qrdb.v1.QRDBService/VectorDeleteNamespace":       true,
	"/qrdb.v1.QRDBService/VectorListNamespaces":        true,
	"/qrdb.v1.QRDBService/VectorGetNamespaceInfo":      true,
	"/qrdb.v1.QRDBService/VectorUpsert":                true,
	"/qrdb.v1.QRDBService/VectorGet":                   true,
	"/qrdb.v1.QRDBService/VectorDelete":                true,
	"/qrdb.v1.QRDBService/VectorBatchUpsert":           true,
	"/qrdb.v1.QRDBService/VectorBatchDelete":           true,
	"/qrdb.v1.QRDBService/VectorList":                  true,
	"/qrdb.v1.QRDBService/VectorUpdateMetadata":        true,
	"/qrdb.v1.QRDBService/VectorSearchSimilar":         true,
	"/qrdb.v1.QRDBService/VectorSearchByMetadata":      true,
	"/qrdb.v1.QRDBService/VectorHybridSearch":          true,
	"/qrdb.v1.QRDBService/VectorSearchByID":            true,
	"/qrdb.v1.QRDBService/VectorRebuildIndex":          true,
	"/qrdb.v1.QRDBService/VectorGetRebuildProgress":    true,
	"/qrdb.v1.QRDBService/VectorGetIndexStats":         true,
	"/qrdb.v1.QRDBService/VectorCompactNamespace":      true,
	"/qrdb.v1.QRDBService/VectorGetCompactionProgress": true,
	// Collections
	"/qrdb.v1.QRDBService/CreateCollection":  true,
	"/qrdb.v1.QRDBService/GetCollectionInfo": true,
	"/qrdb.v1.QRDBService/ListCollections":   true,
	"/qrdb.v1.QRDBService/DropCollection":    true,
	"/qrdb.v1.QRDBService/InsertDocument":    true,
	"/qrdb.v1.QRDBService/GetDocument":       true,
	"/qrdb.v1.QRDBService/UpdateDocument":    true,
	"/qrdb.v1.QRDBService/DeleteDocument":    true,
	"/qrdb.v1.QRDBService/ListDocuments":     true,
}

// QRDBDataTenantError reports a registry-backed qrdb data call that reached
// the server with no tenant bound to its context.
type QRDBDataTenantError struct {
	Method string
	Cause  error
}

// Error implements error.
func (e *QRDBDataTenantError) Error() string {
	return fmt.Sprintf("qrdb data call %s refused: %v", e.Method, e.Cause)
}

// Unwrap returns the tenant resolution error.
func (e *QRDBDataTenantError) Unwrap() error { return e.Cause }

// qrdbDataTenantCheck returns the PermissionDenied status for a registry-backed
// data method whose context has no bound tenant, and nil otherwise.
func qrdbDataTenantCheck(ctx context.Context, method string) error {
	if !qrdbRegistryDataMethods[method] {
		return nil
	}
	if _, err := tenantctx.FromContext(ctx, method); err != nil {
		return status.Error(codes.PermissionDenied, (&QRDBDataTenantError{Method: method, Cause: err}).Error())
	}
	return nil
}

// UnaryQRDBDataTenantInterceptor refuses a registry-backed qrdb data call with
// PermissionDenied unless a tenant is bound to its context. An absent tenant
// is refused, never read as the system tenant. Other methods pass through.
func UnaryQRDBDataTenantInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if err := qrdbDataTenantCheck(ctx, info.FullMethod); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamQRDBDataTenantInterceptor refuses a registry-backed qrdb data stream
// with PermissionDenied unless a tenant is bound to its context. An absent
// tenant is refused, never read as the system tenant. Other methods pass
// through.
func StreamQRDBDataTenantInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := qrdbDataTenantCheck(ss.Context(), info.FullMethod); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}
