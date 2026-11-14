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
	"context"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// ObjectStore is the part of the go-quicbaas object-storage service
// (pkg/service, which re-exports go-objstore) the registry keeps its
// blobs, manifests and tags in. service.ObjectStorageService
// satisfies it.
type ObjectStore interface {
	PutObject(ctx context.Context, req *service.PutObjectRequest) (*service.PutObjectResponse, error)
	GetObject(ctx context.Context, req *service.GetObjectRequest) (*service.GetObjectResponse, error)
	DeleteObject(ctx context.Context, req *service.DeleteObjectRequest) (*service.DeleteObjectResponse, error)
	ListObjects(ctx context.Context, req *service.ListObjectsRequest) (*service.ListObjectsResponse, error)
}

const (
	// objectContentType is the content type registry objects are
	// stored with; the registry tracks OCI media types itself.
	objectContentType = "application/octet-stream"

	// objectEmptyTag marks an object holding a zero-length value: the
	// object-storage service refuses empty values, and an empty blob
	// is a valid OCI blob.
	objectEmptyTag = "ociregistry.empty"
)

// objectStatusActive is the catalog status of a live object.
var objectStatusActive = string(catalog.StatusActive)

// ObjectStoreClient is the QuicBaaSStorageClient over the object-storage
// service: each (entityType, key) pair is the object "entityType:key".
// The registry acts as its own tenant (the system tenant unless
// WithObjectStoreTenant says otherwise): who may reach the registry is
// the /v2/ gates' decision, not the object store's.
type ObjectStoreClient struct {
	objects ObjectStore
	tenant  string
}

// ObjectStoreOption configures an ObjectStoreClient.
type ObjectStoreOption func(*ObjectStoreClient)

// WithObjectStoreTenant runs the registry's object operations as tenant.
func WithObjectStoreTenant(tenant string) ObjectStoreOption {
	return func(c *ObjectStoreClient) { c.tenant = tenant }
}

// NewObjectStoreClient returns the storage client over objects.
func NewObjectStoreClient(objects ObjectStore, opts ...ObjectStoreOption) (*ObjectStoreClient, error) {
	if objects == nil {
		return nil, ErrNilClient{Client: "object storage"}
	}
	c := &ObjectStoreClient{objects: objects, tenant: tenantctx.SystemTenantID}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// NewObjectStorage returns the registry storage kept in the
// object-storage service.
func NewObjectStorage(objects ObjectStore, storageOpts []StorageOption, clientOpts ...ObjectStoreOption) (*QuicBaaSStorage, error) {
	client, err := NewObjectStoreClient(objects, clientOpts...)
	if err != nil {
		return nil, err
	}
	return NewQuicBaaSStorage(client, storageOpts...)
}

func (c *ObjectStoreClient) ctx(ctx context.Context) context.Context {
	return tenantctx.WithTenant(ctx, c.tenant)
}

// objectKey is the object-store key of (entityType, key): the entity
// type is the namespace and the escaped key the path.
func objectKey(entityType, key string) string {
	return entityType + ":" + escapeObjectPath(key)
}

// escapeObjectPath makes a registry key a valid object path. Paths
// allow letters, digits, '-', '_', '.' and '/', and registry keys also
// carry ':' (name:tag, sha256:...). The escape is per character, so it
// is reversible and an escaped prefix is a prefix of the escaped key:
// '_' becomes "_u" and ':' becomes "_c".
func escapeObjectPath(key string) string {
	if !strings.ContainsAny(key, "_:") {
		return key
	}
	var b strings.Builder
	b.Grow(len(key) + 8)
	for i := 0; i < len(key); i++ {
		switch key[i] {
		case '_':
			b.WriteString("_u")
		case ':':
			b.WriteString("_c")
		default:
			b.WriteByte(key[i])
		}
	}
	return b.String()
}

// unescapeObjectPath inverts escapeObjectPath; ok is false for a path
// escapeObjectPath cannot have produced.
func unescapeObjectPath(path string) (string, bool) {
	if !strings.Contains(path, "_") {
		return path, true
	}
	var b strings.Builder
	b.Grow(len(path))
	for i := 0; i < len(path); i++ {
		if path[i] != '_' {
			b.WriteByte(path[i])
			continue
		}
		if i+1 == len(path) {
			return "", false
		}
		i++
		switch path[i] {
		case 'u':
			b.WriteByte('_')
		case 'c':
			b.WriteByte(':')
		default:
			return "", false
		}
	}
	return b.String(), true
}

// Put stores value under (entityType, key). The object store keeps
// objects until they are deleted, so ttl must be zero.
func (c *ObjectStoreClient) Put(ctx context.Context, entityType, key string, value []byte, ttl time.Duration) error {
	if ttl != 0 {
		return ErrObjectStoreTTL{TTL: ttl}
	}
	req := &service.PutObjectRequest{
		Key:         objectKey(entityType, key),
		Value:       value,
		ContentType: objectContentType,
	}
	if len(value) == 0 {
		req.Value = []byte{0}
		req.Tags = map[string]string{objectEmptyTag: "true"}
	}
	_, err := c.objects.PutObject(c.ctx(ctx), req)
	return err
}

// Get returns the value under (entityType, key); found is false when
// there is none.
func (c *ObjectStoreClient) Get(ctx context.Context, entityType, key string) ([]byte, bool, error) {
	resp, err := c.objects.GetObject(c.ctx(ctx), &service.GetObjectRequest{
		Key:             objectKey(entityType, key),
		IncludeMetadata: true,
	})
	if err != nil {
		if qberrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if resp.Tags[objectEmptyTag] == "true" {
		return []byte{}, true, nil
	}
	return resp.Value, true, nil
}

// Delete removes (entityType, key); removing what is not there is not
// an error.
func (c *ObjectStoreClient) Delete(ctx context.Context, entityType, key string) error {
	_, err := c.objects.DeleteObject(c.ctx(ctx), &service.DeleteObjectRequest{Key: objectKey(entityType, key)})
	if err != nil && !qberrors.IsNotFound(err) {
		return err
	}
	return nil
}

// List returns the live keys of entityType that begin with prefix.
func (c *ObjectStoreClient) List(ctx context.Context, entityType, prefix string, limit int, cursor string) ([]string, string, bool, error) {
	resp, err := c.objects.ListObjects(c.ctx(ctx), &service.ListObjectsRequest{
		Prefix: objectKey(entityType, prefix),
		Limit:  limit,
		Cursor: cursor,
	})
	if err != nil {
		return nil, "", false, err
	}
	keys := make([]string, 0, len(resp.Objects))
	for _, o := range resp.Objects {
		if o == nil || (o.Status != "" && o.Status != objectStatusActive) {
			continue
		}
		path, ok := strings.CutPrefix(o.Key, entityType+":")
		if !ok {
			continue
		}
		key, ok := unescapeObjectPath(path)
		if !ok {
			continue
		}
		keys = append(keys, key)
	}
	return keys, resp.NextCursor, resp.HasMore, nil
}
