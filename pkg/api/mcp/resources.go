//go:build mcp

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

// Package mcp provides MCP resources that expose QuicBaaS data.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ResourceRegistry manages MCP resources and their access.
type ResourceRegistry struct {
	service service.QuicBaaS
	logger  *logger.Logger
}

// ResourceDefinition describes an MCP resource.
type ResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

// NewResourceRegistry creates a new resource registry.
func NewResourceRegistry(svc service.QuicBaaS, log *logger.Logger) *ResourceRegistry {
	return &ResourceRegistry{
		service: svc,
		logger:  log,
	}
}

// ListResources returns all available resources.
func (r *ResourceRegistry) ListResources() []ResourceDefinition {
	return []ResourceDefinition{
		{
			URI:         "quicbaas://cluster/status",
			Name:        "Cluster Status",
			Description: "Current cluster status including nodes, shards, and health",
			MimeType:    "application/json",
		},
		{
			URI:         "quicbaas://cluster/nodes",
			Name:        "Cluster Nodes",
			Description: "List of all cluster nodes with status",
			MimeType:    "application/json",
		},
		{
			URI:         "quicbaas://cluster/shards",
			Name:        "Cluster Shards",
			Description: "List of all shards with leadership and replica information",
			MimeType:    "application/json",
		},
		{
			URI:         "quicbaas://metrics/summary",
			Name:        "Metrics Summary",
			Description: "Performance metrics summary",
			MimeType:    "application/json",
		},
		{
			URI:         "quicbaas://cache/stats",
			Name:        "Cache Statistics",
			Description: "Cache hit rates, size, and eviction statistics",
			MimeType:    "application/json",
		},
		{
			URI:         "quicbaas://health",
			Name:        "Health Status",
			Description: "Detailed health status of all components",
			MimeType:    "application/json",
		},
		{
			URI:         "quicbaas://backups",
			Name:        "Available Backups",
			Description: "List of all available backups",
			MimeType:    "application/json",
		},
		{
			URI:         "quicbaas://users",
			Name:        "User List",
			Description: "List of all users and their roles",
			MimeType:    "application/json",
		},
		{
			URI:         "quicbaas://roles",
			Name:        "Role List",
			Description: "List of all roles and their permissions",
			MimeType:    "application/json",
		},
		{
			URI:         "quicbaas://entities/{type}",
			Name:        "Entity List by Type",
			Description: "List all entities of a specific type (template)",
			MimeType:    "application/json",
		},
		{
			URI:         "quicbaas://entities/{type}/{id}",
			Name:        "Specific Entity",
			Description: "Retrieve a specific entity by type and ID (template)",
			MimeType:    "application/json",
		},
	}
}

// ReadResource reads a resource by URI.
func (r *ResourceRegistry) ReadResource(ctx context.Context, uri string) (interface{}, error) {
	r.logger.WithField("uri", uri).Debug("Reading resource")

	// Add timeout if not present
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// Parse URI and route to appropriate handler
	if !strings.HasPrefix(uri, "quicbaas://") {
		return nil, api.NewInvalidInputError("invalid resource URI: must start with quicbaas://")
	}

	path := strings.TrimPrefix(uri, "quicbaas://")
	parts := strings.Split(path, "/")

	if len(parts) == 0 {
		return nil, api.NewInvalidInputError("invalid resource URI: empty path")
	}

	// Route based on resource type
	switch parts[0] {
	case "cluster":
		return r.readClusterResource(ctx, parts[1:])
	case "metrics":
		return r.readMetricsResource(ctx, parts[1:])
	case "cache":
		return r.readCacheResource(ctx, parts[1:])
	case "health":
		return r.readHealthResource(ctx)
	case "backups":
		return r.readBackupsResource(ctx)
	case "users":
		return r.readUsersResource(ctx)
	case "roles":
		return r.readRolesResource(ctx)
	case "entities":
		return r.readEntitiesResource(ctx, parts[1:])
	default:
		return nil, api.NewNotFoundError("resource type", parts[0])
	}
}

// readClusterResource reads cluster-related resources.
func (r *ResourceRegistry) readClusterResource(ctx context.Context, parts []string) (interface{}, error) {
	if len(parts) == 0 {
		return nil, api.NewRequiredFieldError("cluster resource type")
	}

	switch parts[0] {
	case "status":
		req := &service.ClusterStatusRequest{IncludeDetails: true}
		resp, err := r.service.ClusterStatus(ctx, req)
		if err != nil {
			return nil, err
		}
		return r.marshalResource(resp)

	case "nodes":
		req := &service.ClusterStatusRequest{IncludeDetails: true}
		resp, err := r.service.ClusterStatus(ctx, req)
		if err != nil {
			return nil, err
		}
		return r.marshalResource(map[string]interface{}{
			"nodes":     resp.Nodes,
			"timestamp": resp.Timestamp,
		})

	case "shards":
		req := &service.ClusterStatusRequest{IncludeDetails: true}
		resp, err := r.service.ClusterStatus(ctx, req)
		if err != nil {
			return nil, err
		}
		return r.marshalResource(map[string]interface{}{
			"shards":    resp.Shards,
			"timestamp": resp.Timestamp,
		})

	default:
		return nil, api.NewNotFoundError("cluster resource", parts[0])
	}
}

// readMetricsResource reads metrics-related resources.
func (r *ResourceRegistry) readMetricsResource(ctx context.Context, parts []string) (interface{}, error) {
	if len(parts) == 0 {
		return nil, api.NewRequiredFieldError("metrics resource type")
	}

	switch parts[0] {
	case "summary":
		// Get cache stats as a proxy for metrics summary
		req := &service.GetCacheStatsRequest{}
		resp, err := r.service.GetCacheStats(ctx, req)
		if err != nil {
			return nil, err
		}

		// Get health for component status
		healthReq := &service.HealthRequest{IncludeDetails: true}
		healthResp, err := r.service.Health(ctx, healthReq)
		if err != nil {
			return nil, err
		}

		return r.marshalResource(map[string]interface{}{
			"cache": resp,
			"health": map[string]interface{}{
				"status":         healthResp.Status,
				"uptime":         healthResp.UptimeSeconds,
				"componentCount": len(healthResp.Details),
			},
			"timestamp": time.Now().Unix(),
		})

	default:
		return nil, api.NewNotFoundError("metrics resource", parts[0])
	}
}

// readCacheResource reads cache-related resources.
func (r *ResourceRegistry) readCacheResource(ctx context.Context, parts []string) (interface{}, error) {
	if len(parts) == 0 {
		return nil, api.NewRequiredFieldError("cache resource type")
	}

	switch parts[0] {
	case "stats":
		req := &service.GetCacheStatsRequest{}
		resp, err := r.service.GetCacheStats(ctx, req)
		if err != nil {
			return nil, err
		}
		return r.marshalResource(resp)

	default:
		return nil, api.NewNotFoundError("cache resource", parts[0])
	}
}

// readHealthResource reads health status.
func (r *ResourceRegistry) readHealthResource(ctx context.Context) (interface{}, error) {
	req := &service.HealthRequest{IncludeDetails: true}
	resp, err := r.service.Health(ctx, req)
	if err != nil {
		return nil, err
	}
	return r.marshalResource(resp)
}

// readBackupsResource reads backup list.
func (r *ResourceRegistry) readBackupsResource(ctx context.Context) (interface{}, error) {
	req := &service.ListBackupsRequest{
		Limit:  100,
		Offset: 0,
	}
	resp, err := r.service.ListBackups(ctx, req)
	if err != nil {
		return nil, err
	}
	return r.marshalResource(resp)
}

// readUsersResource reads user list.
func (r *ResourceRegistry) readUsersResource(ctx context.Context) (interface{}, error) {
	req := &service.ListUsersRequest{
		Limit:  100,
		Offset: 0,
	}
	resp, err := r.service.ListUsers(ctx, req)
	if err != nil {
		return nil, err
	}
	return r.marshalResource(resp)
}

// readRolesResource reads role list.
func (r *ResourceRegistry) readRolesResource(ctx context.Context) (interface{}, error) {
	req := &service.AuthListRolesRequest{}
	resp, err := r.service.ListAuthRoles(ctx, req)
	if err != nil {
		return nil, err
	}
	return r.marshalResource(resp)
}

// readEntitiesResource reads entity-related resources.
func (r *ResourceRegistry) readEntitiesResource(ctx context.Context, parts []string) (interface{}, error) {
	if len(parts) == 0 {
		return nil, api.NewRequiredFieldError("entity type")
	}

	entityType := parts[0]

	// If ID is provided, get specific entity
	if len(parts) >= 2 {
		// Parse entity ID
		var id uint64
		if _, err := fmt.Sscanf(parts[1], "%d", &id); err != nil {
			return nil, api.NewInvalidFormatMessageError("entity ID", parts[1])
		}

		req := &service.GetRequest{
			EntityType: entityType,
			ID:         id,
		}
		resp, err := r.service.Get(ctx, req)
		if err != nil {
			return nil, err
		}

		return r.marshalResource(map[string]interface{}{
			"entity": map[string]interface{}{
				"type":      entityType,
				"id":        resp.ID,
				"data":      string(resp.Data),
				"version":   resp.Version,
				"timestamp": resp.Timestamp,
				"found":     resp.Found,
			},
		})
	}

	// Otherwise, list all entities of this type
	req := &service.ListRequest{
		EntityType: entityType,
		PageSize:   100,
	}

	// Create channel for streaming results
	stream := make(chan *service.ListResponse, 10)

	// Start list operation in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.service.List(ctx, req, stream)
	}()

	// Collect all results
	var entities []interface{}
	for resp := range stream {
		for _, entity := range resp.Entities {
			entities = append(entities, map[string]interface{}{
				"type":      entity.EntityType,
				"id":        entity.ID,
				"data":      string(entity.Data),
				"version":   entity.Version,
				"timestamp": entity.Timestamp,
			})
		}
	}

	// Check for errors
	if err := <-errCh; err != nil {
		return nil, err
	}

	return r.marshalResource(map[string]interface{}{
		"entityType": entityType,
		"entities":   entities,
		"count":      len(entities),
	})
}

// marshalResource marshals a resource to JSON-compatible format.
func (r *ResourceRegistry) marshalResource(v interface{}) (interface{}, error) {
	// Convert to JSON and back to get JSON-compatible types
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	// Wrap in MCP resource format
	return map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"uri":      "resource",
				"mimeType": "application/json",
				"text":     string(data),
			},
		},
	}, nil
}
