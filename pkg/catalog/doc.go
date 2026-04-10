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

// Package catalog provides the internal object catalog for QuicBaaS's
// distributed object storage system.
//
// # Overview
//
// The catalog is the source of truth for all object metadata and storage
// locations. Objects are identified by location-agnostic keys in the format
// {namespace}:{path}, allowing the same object identity to exist across
// multiple storage backends.
//
// # Key Format
//
// Object keys follow the format:
//
//	{namespace}:{path}
//
// Examples:
//   - backups:daily/2024-01-15.tar.gz
//   - tenant-acme:documents/report.pdf
//   - blobs:user/123/avatar.png
//
// # Catalog Entry
//
// Each object in the catalog has a CatalogEntry that tracks:
//   - Identity: key, object ID
//   - Content: checksum, size, content type
//   - Locations: primary store, replicas, archive
//   - Policies: replication, archive, retention
//   - Encryption: KMS key, algorithm
//   - Multi-tenancy: tenant ID, owner
//   - Metadata: tags, labels, custom metadata
//
// # Storage Layout
//
// The catalog uses PebbleDB with the following key prefixes:
//
//	cat:{key}                    - Primary catalog entry
//	idx:store:{store}:{key}      - Index by backend
//	idx:tenant:{tenant}:{key}    - Index by tenant
//	idx:checksum:{hex}:{key}     - Index by checksum (dedup)
//	idx:status:{status}:{key}    - Index by status
//	idx:tag:{tag}:{value}:{key}  - Index by tag
//	queue:replicate:...          - Replication queue
//	queue:archive:...            - Archive queue
//	stats:tenant:{tenant}        - Tenant usage stats
//
// # Usage
//
// Create a catalog manager:
//
//	config := &CatalogManagerConfig{
//	    StorageConfig: &StorageConfig{
//	        Path: "/var/lib/quicbaas/catalog",
//	    },
//	}
//	mgr, err := NewCatalogManager(config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer mgr.Close()
//
// Store an object entry:
//
//	entry := NewCatalogEntry("tenant-acme:docs/report.pdf", 1024, checksum)
//	entry.PrimaryStore = StoreLocation{Store: "s3-primary", ObjectRef: "obj/123"}
//	entry.TenantID = "tenant-acme"
//	err := mgr.Put(ctx, entry)
//
// Query objects:
//
//	entries, err := mgr.ListByTenant(ctx, "tenant-acme", &ListOptions{Limit: 100})
//
// # Background Operations
//
// The catalog supports async operations via work queues:
//   - Replication: copy objects to replica backends
//   - Archive: move objects to cold storage
//   - Verify: check object integrity
//   - Delete: async deletion from backends
//   - Re-encrypt: key rotation
//
// # Thread Safety
//
// CatalogManager is safe for concurrent use by multiple goroutines.
// Individual CatalogEntry structs are not safe for concurrent modification.
package catalog
