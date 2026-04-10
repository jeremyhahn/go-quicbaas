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

// Package mcp implements Parameter Store MCP integration handlers.
package mcp

import (
	"context"
	"encoding/json"
	"strconv"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerParameterTools registers all Parameter Store tool handlers on the ToolRegistry.
func (r *ToolRegistry) registerParameterTools() {
	parameterTools := getAllParameterTools()
	for _, tool := range parameterTools {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// executeParameterTool executes a Parameter Store tool and returns the result.
func (r *ToolRegistry) executeParameterTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	parameterService := r.getParameterStoreService()
	if parameterService == nil {
		return nil, api.NewServiceNotConfiguredError("parameter store")
	}

	switch name {
	case "quicbaas_parameter_get":
		return r.handleParameterGet(ctx, parameterService, args)
	case "quicbaas_parameter_set":
		return r.handleParameterSet(ctx, parameterService, args)
	case "quicbaas_parameter_delete":
		return r.handleParameterDelete(ctx, parameterService, args)
	case "quicbaas_parameter_list":
		return r.handleParameterList(ctx, parameterService, args)
	case "quicbaas_parameter_batch_get":
		return r.handleParameterBatchGet(ctx, parameterService, args)
	case "quicbaas_parameter_batch_set":
		return r.handleParameterBatchSet(ctx, parameterService, args)
	case "quicbaas_parameter_batch_delete":
		return r.handleParameterBatchDelete(ctx, parameterService, args)
	case "quicbaas_parameter_search":
		return r.handleParameterSearch(ctx, parameterService, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// getParameterStoreService retrieves the ParameterStoreService from the service.
func (r *ToolRegistry) getParameterStoreService() service.ParameterStoreService {
	return r.service.ParameterStoreService()
}

// handleParameterGet handles the quicbaas_parameter_get tool.
func (r *ToolRegistry) handleParameterGet(ctx context.Context, svc service.ParameterStoreService, args map[string]interface{}) (interface{}, error) {
	path := getString(args, "path")
	if path == "" {
		return nil, api.NewRequiredFieldError("path")
	}

	withDecryption := parseBool(getString(args, "with_decryption"), true)

	req := &interfaces.GetParameterRequest{
		Path:           path,
		WithDecryption: withDecryption,
	}

	resp, err := svc.GetParameter(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("get parameter", err)
	}

	return formatParameterResponse(resp.Parameter, resp.Timestamp), nil
}

// handleParameterSet handles the quicbaas_parameter_set tool.
func (r *ToolRegistry) handleParameterSet(ctx context.Context, svc service.ParameterStoreService, args map[string]interface{}) (interface{}, error) {
	path := getString(args, "path")
	if path == "" {
		return nil, api.NewRequiredFieldError("path")
	}

	value := getString(args, "value")
	if value == "" {
		return nil, api.NewRequiredFieldError("value")
	}

	// Parse tags from JSON string
	var tags map[string]string
	if tagsJSON := getString(args, "tags"); tagsJSON != "" {
		if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
			return nil, api.NewInvalidFormatError("tags JSON", err)
		}
	}

	ttlSeconds := parseInt64(getString(args, "ttl_seconds"), 0)
	auditEnabled := parseBool(getString(args, "audit_enabled"), false)
	overwrite := parseBool(getString(args, "overwrite"), true)

	req := &interfaces.SetParameterRequest{
		Path:         path,
		Value:        []byte(value),
		Type:         getStringDefault(args, "type", "string"),
		Description:  getString(args, "description"),
		KMSKeyID:     getString(args, "kms_key_id"),
		Tags:         tags,
		TTLSeconds:   ttlSeconds,
		AuditEnabled: auditEnabled,
		Overwrite:    overwrite,
	}

	resp, err := svc.SetParameter(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("set parameter", err)
	}

	return map[string]interface{}{
		"path":      resp.Path,
		"created":   resp.Created,
		"timestamp": resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleParameterDelete handles the quicbaas_parameter_delete tool.
func (r *ToolRegistry) handleParameterDelete(ctx context.Context, svc service.ParameterStoreService, args map[string]interface{}) (interface{}, error) {
	path := getString(args, "path")
	if path == "" {
		return nil, api.NewRequiredFieldError("path")
	}

	req := &interfaces.DeleteParameterRequest{
		Path: path,
	}

	resp, err := svc.DeleteParameter(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("delete parameter", err)
	}

	return map[string]interface{}{
		"path":      resp.Path,
		"deleted":   resp.Deleted,
		"timestamp": resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleParameterList handles the quicbaas_parameter_list tool.
func (r *ToolRegistry) handleParameterList(ctx context.Context, svc service.ParameterStoreService, args map[string]interface{}) (interface{}, error) {
	prefix := getString(args, "prefix")
	if prefix == "" {
		return nil, api.NewRequiredFieldError("prefix")
	}

	recursive := parseBool(getString(args, "recursive"), true)
	withDecryption := parseBool(getString(args, "with_decryption"), false)
	withValues := parseBool(getString(args, "with_values"), false)
	limit := parseInt(getString(args, "limit"), 100)

	req := &interfaces.ListParametersRequest{
		Prefix:         prefix,
		Recursive:      recursive,
		WithDecryption: withDecryption,
		WithValues:     withValues,
		Limit:          limit,
		Cursor:         getString(args, "cursor"),
	}

	resp, err := svc.ListParameters(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("list parameters", err)
	}

	parameters := make([]map[string]interface{}, 0, len(resp.Parameters))
	for _, p := range resp.Parameters {
		parameters = append(parameters, formatParameterValue(p))
	}

	result := map[string]interface{}{
		"parameters": parameters,
		"has_more":   resp.HasMore,
		"timestamp":  resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}

	if resp.NextCursor != "" {
		result["next_cursor"] = resp.NextCursor
	}
	if resp.TotalCount > 0 {
		result["total_count"] = resp.TotalCount
	}

	return result, nil
}

// handleParameterBatchGet handles the quicbaas_parameter_batch_get tool.
func (r *ToolRegistry) handleParameterBatchGet(ctx context.Context, svc service.ParameterStoreService, args map[string]interface{}) (interface{}, error) {
	pathsJSON := getString(args, "paths")
	if pathsJSON == "" {
		return nil, api.NewRequiredFieldError("paths")
	}

	var paths []string
	if err := json.Unmarshal([]byte(pathsJSON), &paths); err != nil {
		return nil, api.NewInvalidFormatError("paths JSON", err)
	}

	if len(paths) == 0 {
		return nil, api.NewEmptyCollectionError("paths array")
	}

	withDecryption := parseBool(getString(args, "with_decryption"), true)

	req := &interfaces.GetParametersRequest{
		Paths:          paths,
		WithDecryption: withDecryption,
	}

	resp, err := svc.GetParameters(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("batch get parameters", err)
	}

	parameters := make([]map[string]interface{}, 0, len(resp.Parameters))
	for _, p := range resp.Parameters {
		parameters = append(parameters, formatParameterValue(p))
	}

	result := map[string]interface{}{
		"parameters": parameters,
		"timestamp":  resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}

	if len(resp.Errors) > 0 {
		result["errors"] = resp.Errors
	}

	return result, nil
}

// handleParameterBatchSet handles the quicbaas_parameter_batch_set tool.
func (r *ToolRegistry) handleParameterBatchSet(ctx context.Context, svc service.ParameterStoreService, args map[string]interface{}) (interface{}, error) {
	parametersJSON := getString(args, "parameters")
	if parametersJSON == "" {
		return nil, api.NewRequiredFieldError("parameters")
	}

	var paramInputs []parameterSetInput
	if err := json.Unmarshal([]byte(parametersJSON), &paramInputs); err != nil {
		return nil, api.NewInvalidFormatError("parameters JSON", err)
	}

	if len(paramInputs) == 0 {
		return nil, api.NewEmptyCollectionError("parameters array")
	}

	// Convert to request objects
	params := make([]*interfaces.SetParameterRequest, 0, len(paramInputs))
	for _, p := range paramInputs {
		params = append(params, &interfaces.SetParameterRequest{
			Path:         p.Path,
			Value:        []byte(p.Value),
			Type:         p.Type,
			Description:  p.Description,
			KMSKeyID:     p.KMSKeyID,
			Tags:         p.Tags,
			TTLSeconds:   p.TTLSeconds,
			AuditEnabled: p.AuditEnabled,
			Overwrite:    p.Overwrite,
		})
	}

	req := &interfaces.SetParametersRequest{
		Parameters: params,
	}

	resp, err := svc.SetParameters(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("batch set parameters", err)
	}

	results := make([]map[string]interface{}, 0, len(resp.Results))
	for _, r := range resp.Results {
		resultMap := map[string]interface{}{
			"path":    r.Path,
			"created": r.Created,
			"success": r.Success,
		}
		if r.Error != "" {
			resultMap["error"] = r.Error
		}
		results = append(results, resultMap)
	}

	return map[string]interface{}{
		"results":   results,
		"timestamp": resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleParameterBatchDelete handles the quicbaas_parameter_batch_delete tool.
func (r *ToolRegistry) handleParameterBatchDelete(ctx context.Context, svc service.ParameterStoreService, args map[string]interface{}) (interface{}, error) {
	pathsJSON := getString(args, "paths")
	if pathsJSON == "" {
		return nil, api.NewRequiredFieldError("paths")
	}

	var paths []string
	if err := json.Unmarshal([]byte(pathsJSON), &paths); err != nil {
		return nil, api.NewInvalidFormatError("paths JSON", err)
	}

	if len(paths) == 0 {
		return nil, api.NewEmptyCollectionError("paths array")
	}

	req := &interfaces.DeleteParametersRequest{
		Paths: paths,
	}

	resp, err := svc.DeleteParameters(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("batch delete parameters", err)
	}

	results := make([]map[string]interface{}, 0, len(resp.Results))
	for _, r := range resp.Results {
		resultMap := map[string]interface{}{
			"path":    r.Path,
			"deleted": r.Deleted,
		}
		if r.Error != "" {
			resultMap["error"] = r.Error
		}
		results = append(results, resultMap)
	}

	return map[string]interface{}{
		"results":       results,
		"deleted_count": resp.DeletedCount,
		"timestamp":     resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}

// handleParameterSearch handles the quicbaas_parameter_search tool.
func (r *ToolRegistry) handleParameterSearch(ctx context.Context, svc service.ParameterStoreService, args map[string]interface{}) (interface{}, error) {
	tagsJSON := getString(args, "tags")
	if tagsJSON == "" {
		return nil, api.NewRequiredFieldError("tags")
	}

	var tags map[string]string
	if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
		return nil, api.NewInvalidFormatError("tags JSON", err)
	}

	if len(tags) == 0 {
		return nil, api.NewEmptyCollectionError("tags")
	}

	withValues := parseBool(getString(args, "with_values"), false)
	withDecryption := parseBool(getString(args, "with_decryption"), false)
	limit := parseInt(getString(args, "limit"), 100)

	req := &interfaces.SearchByTagsRequest{
		Tags:           tags,
		Prefix:         getString(args, "prefix"),
		WithValues:     withValues,
		WithDecryption: withDecryption,
		Limit:          limit,
		Cursor:         getString(args, "cursor"),
	}

	resp, err := svc.SearchByTags(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("search parameters", err)
	}

	parameters := make([]map[string]interface{}, 0, len(resp.Parameters))
	for _, p := range resp.Parameters {
		parameters = append(parameters, formatParameterValue(p))
	}

	result := map[string]interface{}{
		"parameters": parameters,
		"has_more":   resp.HasMore,
		"timestamp":  resp.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}

	if resp.NextCursor != "" {
		result["next_cursor"] = resp.NextCursor
	}

	return result, nil
}

// isParameterTool returns true if the tool name is a Parameter Store tool.
func isParameterTool(name string) bool {
	switch name {
	case "quicbaas_parameter_get",
		"quicbaas_parameter_set",
		"quicbaas_parameter_delete",
		"quicbaas_parameter_list",
		"quicbaas_parameter_batch_get",
		"quicbaas_parameter_batch_set",
		"quicbaas_parameter_batch_delete",
		"quicbaas_parameter_search":
		return true
	default:
		return false
	}
}

// parameterSetInput represents the JSON input for a parameter set operation.
type parameterSetInput struct {
	Path         string            `json:"path"`
	Value        string            `json:"value"`
	Type         string            `json:"type,omitempty"`
	Description  string            `json:"description,omitempty"`
	KMSKeyID     string            `json:"kms_key_id,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	TTLSeconds   int64             `json:"ttl_seconds,omitempty"`
	AuditEnabled bool              `json:"audit_enabled,omitempty"`
	Overwrite    bool              `json:"overwrite,omitempty"`
}

// formatParameterResponse formats a GetParameterResponse for MCP output.
// Wraps the parameter in a "parameter" field for consistency with other responses.
func formatParameterResponse(param *interfaces.ParameterValue, timestamp interface{}) map[string]interface{} {
	result := map[string]interface{}{
		"parameter": formatParameterValue(param),
	}
	if ts, ok := timestamp.(interface{ Format(string) string }); ok {
		result["response_timestamp"] = ts.Format("2006-01-02T15:04:05Z07:00")
	}
	return result
}

// formatParameterValue formats a ParameterValue for MCP output.
func formatParameterValue(p *interfaces.ParameterValue) map[string]interface{} {
	result := map[string]interface{}{
		"path":          p.Path,
		"type":          p.Type,
		"is_encrypted":  p.IsEncrypted,
		"audit_enabled": p.AuditEnabled,
		"created_at":    p.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"updated_at":    p.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if len(p.Value) > 0 {
		result["value"] = string(p.Value)
	}
	if p.Description != "" {
		result["description"] = p.Description
	}
	if len(p.Tags) > 0 {
		result["tags"] = p.Tags
	}
	if p.KMSKeyID != "" {
		result["kms_key_id"] = p.KMSKeyID
	}
	if p.ExpiresAt != nil {
		result["expires_at"] = p.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if p.CreatedBy != "" {
		result["created_by"] = p.CreatedBy
	}
	if p.UpdatedBy != "" {
		result["updated_by"] = p.UpdatedBy
	}

	return result
}

// Helper functions for parsing string arguments to typed values.

// parseBool parses a string to bool with a default value.
func parseBool(s string, defaultValue bool) bool {
	if s == "" {
		return defaultValue
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return defaultValue
	}
	return v
}

// parseInt parses a string to int with a default value.
func parseInt(s string, defaultValue int) int {
	if s == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultValue
	}
	return v
}

// parseInt64 parses a string to int64 with a default value.
func parseInt64(s string, defaultValue int64) int64 {
	if s == "" {
		return defaultValue
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return defaultValue
	}
	return v
}

// getStringDefault returns the string value for a key or a default if empty.
func getStringDefault(args map[string]interface{}, key, defaultValue string) string {
	v := getString(args, key)
	if v == "" {
		return defaultValue
	}
	return v
}
