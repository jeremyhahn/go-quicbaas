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

// Package objectstorage provides the Object Storage client for the QuicBaaS SDK.
//
// The Object Storage client enables applications to store, retrieve, and manage
// objects in QuicBaaS with support for multi-tenancy, replication, archival,
// and policy-based lifecycle management.
//
// # Basic Usage
//
// Create a client using a transport connection:
//
//	client := objectstorage.NewClient(transport)
//
//	// Store an object
//	resp, err := client.PutObject(ctx, &objectstorage.PutObjectRequest{
//	    Key:         "my-namespace:path/to/object",
//	    Value:       []byte("object data"),
//	    ContentType: "application/octet-stream",
//	    TenantID:    "tenant-123",
//	})
//
//	// Retrieve an object
//	getResp, err := client.GetObject(ctx, &objectstorage.GetObjectRequest{
//	    Key: "my-namespace:path/to/object",
//	})
//
// # Multi-Tenancy
//
// Objects can be organized by tenant for access control and isolation:
//
//	// Store with tenant
//	resp, err := client.PutObject(ctx, &objectstorage.PutObjectRequest{
//	    Key:      "data:file.txt",
//	    Value:    content,
//	    TenantID: "tenant-456",
//	})
//
//	// List objects for a tenant
//	listResp, err := client.ListByTenant(ctx, &objectstorage.ListByTenantRequest{
//	    TenantID: "tenant-456",
//	    Limit:    100,
//	})
//
// # Tagging and Metadata
//
// Objects support user-defined tags for organization and filtering:
//
//	resp, err := client.PutObject(ctx, &objectstorage.PutObjectRequest{
//	    Key:   "data:report.pdf",
//	    Value: reportData,
//	    Tags: map[string]string{
//	        "department": "finance",
//	        "year":       "2025",
//	    },
//	})
//
//	// List by tag
//	tagged, err := client.ListByTag(ctx, &objectstorage.ListByTagRequest{
//	    TagName:  "department",
//	    TagValue: "finance",
//	})
//
// # Replication
//
// Objects can be replicated across multiple storage backends:
//
//	// Replicate to specific backends
//	replResp, err := client.ReplicateObject(ctx, &objectstorage.ReplicateObjectRequest{
//	    Key:         "critical:data.bin",
//	    Targets:     []string{"backend-us-west", "backend-eu-central"},
//	    Synchronous: true,
//	})
//
//	// Set replication policy on object
//	_, err := client.SetObjectPolicy(ctx, &objectstorage.SetObjectPolicyRequest{
//	    Key: "critical:data.bin",
//	    ReplicationPolicy: &catalog.ReplicationPolicy{
//	        Enabled:     true,
//	        MinReplicas: 3,
//	        Mode:        catalog.ReplicationModeSync,
//	    },
//	})
//
// # Archive and Restore
//
// Objects can be archived to cold storage for cost optimization:
//
//	// Archive an object
//	archResp, err := client.ArchiveObject(ctx, &objectstorage.ArchiveObjectRequest{
//	    Key:               "logs:2024-01.log",
//	    TargetStore:       "cold-storage",
//	    DeleteFromPrimary: true,
//	})
//
//	// Restore when needed
//	restResp, err := client.RestoreObject(ctx, &objectstorage.RestoreObjectRequest{
//	    Key:           "logs:2024-01.log",
//	    TargetBackend: "hot-storage",
//	})
//
// # Backend Management
//
// Monitor and manage storage backends:
//
//	// List all backends
//	backends, err := client.ListBackends(ctx, &objectstorage.ListBackendsRequest{
//	    IncludeHealth: true,
//	})
//
//	// Check specific backend health
//	health, err := client.GetBackendHealth(ctx, &objectstorage.GetBackendHealthRequest{
//	    Name: "primary-s3",
//	})
//
// # Quota and Usage
//
// Monitor tenant usage and quotas:
//
//	// Get tenant usage
//	usage, err := client.GetTenantUsage(ctx, &objectstorage.GetTenantUsageRequest{
//	    TenantID: "tenant-123",
//	})
//
//	// Get tenant quotas
//	quotas, err := client.GetTenantQuotas(ctx, &objectstorage.GetTenantQuotasRequest{
//	    TenantID: "tenant-123",
//	})
package objectstorage
