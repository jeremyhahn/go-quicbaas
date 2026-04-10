//go:build mcp && iam

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

package mcp

import (
	"context"
	"fmt"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// GDPR Manager Access
// =============================================================================

// gdprManagerAccessor provides access to the GDPR manager from the QuicBaaS service.
type gdprManagerAccessor interface {
	GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
}

// GDPRServiceUnavailableError indicates the GDPR service is not available.
type GDPRServiceUnavailableError struct {
	Reason string
}

// Error returns the error message.
func (e *GDPRServiceUnavailableError) Error() string {
	return fmt.Sprintf("GDPR service unavailable: %s", e.Reason)
}

// getGDPRManager retrieves the GDPR manager from the QuicBaaS service.
func (r *ToolRegistry) getGDPRManager(ctx context.Context) (*iam.GDPRManager, error) {
	accessor, ok := r.service.(gdprManagerAccessor)
	if !ok {
		return nil, api.NewServiceUnavailableError("GDPR")
	}
	mgr, err := accessor.GetGDPRManager(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("get GDPR manager", err)
	}
	if mgr == nil {
		return nil, api.NewServiceUnavailableError("GDPR")
	}
	return mgr, nil
}

// =============================================================================
// GDPR Tools Registration
// =============================================================================

func (r *ToolRegistry) registerGDPRTools() {
	// Consent Management Tools
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_gdpr_consent_grant",
		Description: "Grant user consent for a specific purpose",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID granting consent",
				},
				"purpose": map[string]interface{}{
					"type":        "string",
					"description": "Purpose for consent (marketing, analytics, personalization, third_party, data_processing, data_retention, functional)",
					"enum":        []string{"marketing", "analytics", "personalization", "third_party", "data_processing", "data_retention", "functional"},
				},
				"version": map[string]interface{}{
					"type":        "string",
					"description": "Version of the consent policy being agreed to",
				},
				"expires_at": map[string]interface{}{
					"type":        "string",
					"description": "When consent expires in RFC3339 format (optional)",
					"format":      "date-time",
				},
				"ip_address": map[string]interface{}{
					"type":        "string",
					"description": "IP address of the user granting consent (for audit)",
				},
				"user_agent": map[string]interface{}{
					"type":        "string",
					"description": "User agent string (for audit)",
				},
			},
			"required": []string{"tenant_id", "user_id", "purpose", "version"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_gdpr_consent_withdraw",
		Description: "Withdraw previously granted consent",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID withdrawing consent",
				},
				"purpose": map[string]interface{}{
					"type":        "string",
					"description": "Purpose for which consent is being withdrawn",
					"enum":        []string{"marketing", "analytics", "personalization", "third_party", "data_processing", "data_retention", "functional"},
				},
				"ip_address": map[string]interface{}{
					"type":        "string",
					"description": "IP address of the user withdrawing consent (for audit)",
				},
				"user_agent": map[string]interface{}{
					"type":        "string",
					"description": "User agent string (for audit)",
				},
			},
			"required": []string{"tenant_id", "user_id", "purpose"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_gdpr_consent_get",
		Description: "Get consent status for a specific purpose",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID",
				},
				"purpose": map[string]interface{}{
					"type":        "string",
					"description": "Consent purpose to retrieve",
					"enum":        []string{"marketing", "analytics", "personalization", "third_party", "data_processing", "data_retention", "functional"},
				},
			},
			"required": []string{"tenant_id", "user_id", "purpose"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_gdpr_consent_list",
		Description: "List all consents for a user",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID to list consents for",
				},
			},
			"required": []string{"tenant_id", "user_id"},
		},
	})

	// Data Export Tools
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_gdpr_export_request",
		Description: "Request a data export for a user (GDPR Article 20 - Right to Data Portability)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID whose data to export",
				},
				"requested_by": map[string]interface{}{
					"type":        "string",
					"description": "ID of user or admin requesting the export",
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "Export format",
					"enum":        []string{"json", "csv", "xml"},
					"default":     "json",
				},
				"include_categories": map[string]interface{}{
					"type":        "array",
					"description": "Data categories to include (empty means all)",
					"items": map[string]interface{}{
						"type": "string",
						"enum": []string{"profile", "sessions", "consents", "groups", "roles", "audit_logs", "credentials", "mfa"},
					},
				},
				"exclude_categories": map[string]interface{}{
					"type":        "array",
					"description": "Data categories to exclude",
					"items": map[string]interface{}{
						"type": "string",
						"enum": []string{"profile", "sessions", "consents", "groups", "roles", "audit_logs", "credentials", "mfa"},
					},
				},
			},
			"required": []string{"tenant_id", "user_id", "requested_by"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_gdpr_export_status",
		Description: "Get the status of a data export request",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"export_id": map[string]interface{}{
					"type":        "string",
					"description": "Export request ID",
				},
			},
			"required": []string{"export_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_gdpr_export_generate",
		Description: "Generate the export data (processes a pending export request)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"export_id": map[string]interface{}{
					"type":        "string",
					"description": "Export request ID to process",
				},
			},
			"required": []string{"export_id"},
		},
	})

	// Data Deletion Tools
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_gdpr_deletion_request",
		Description: "Request data deletion for a user (GDPR Article 17 - Right to Erasure)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID whose data to delete",
				},
				"requested_by": map[string]interface{}{
					"type":        "string",
					"description": "ID of user or admin requesting the deletion",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Reason for the deletion request",
				},
				"delete_categories": map[string]interface{}{
					"type":        "array",
					"description": "Data categories to delete (empty means complete deletion)",
					"items": map[string]interface{}{
						"type": "string",
						"enum": []string{"profile", "sessions", "consents", "groups", "roles", "audit_logs", "credentials", "mfa"},
					},
				},
				"preserve_categories": map[string]interface{}{
					"type":        "array",
					"description": "Data categories to preserve (for legal holds or compliance)",
					"items": map[string]interface{}{
						"type": "string",
						"enum": []string{"profile", "sessions", "consents", "groups", "roles", "audit_logs", "credentials", "mfa"},
					},
				},
				"grace_period_days": map[string]interface{}{
					"type":        "integer",
					"description": "Grace period before deletion begins (default: 30 days)",
					"default":     30,
					"minimum":     0,
					"maximum":     90,
				},
			},
			"required": []string{"tenant_id", "user_id", "requested_by"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_gdpr_deletion_cancel",
		Description: "Cancel a pending deletion request during grace period",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"deletion_id": map[string]interface{}{
					"type":        "string",
					"description": "Deletion request ID to cancel",
				},
				"cancelled_by": map[string]interface{}{
					"type":        "string",
					"description": "ID of user or admin cancelling the request",
				},
			},
			"required": []string{"deletion_id", "cancelled_by"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_gdpr_deletion_process",
		Description: "Process a deletion request (executes the deletion after grace period)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"deletion_id": map[string]interface{}{
					"type":        "string",
					"description": "Deletion request ID to process",
				},
			},
			"required": []string{"deletion_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_gdpr_deletion_status",
		Description: "Get the status of a deletion request",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"deletion_id": map[string]interface{}{
					"type":        "string",
					"description": "Deletion request ID",
				},
			},
			"required": []string{"deletion_id"},
		},
	})
}

// =============================================================================
// Consent Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeIAMGDPRConsentGrant(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	gdprMgr, err := r.getGDPRManager(ctx)
	if err != nil {
		return nil, err
	}

	// Parse optional expiration
	var expiresAt *time.Time
	if expiresStr := getString(args, "expires_at"); expiresStr != "" {
		t, parseErr := time.Parse(time.RFC3339, expiresStr)
		if parseErr != nil {
			return nil, api.NewInvalidFormatError("expires_at", parseErr)
		}
		expiresAt = &t
	}

	consent, err := gdprMgr.GrantConsent(ctx,
		getString(args, "tenant_id"),
		getString(args, "user_id"),
		iam.ConsentPurpose(getString(args, "purpose")),
		getString(args, "version"),
		expiresAt,
		getString(args, "ip_address"),
		getString(args, "user_agent"),
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(consent)
}

func (r *ToolRegistry) executeIAMGDPRConsentWithdraw(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	gdprMgr, err := r.getGDPRManager(ctx)
	if err != nil {
		return nil, err
	}

	err = gdprMgr.WithdrawConsent(ctx,
		getString(args, "tenant_id"),
		getString(args, "user_id"),
		iam.ConsentPurpose(getString(args, "purpose")),
		getString(args, "ip_address"),
		getString(args, "user_agent"),
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"withdrawn": true,
		"timestamp": time.Now(),
	})
}

func (r *ToolRegistry) executeIAMGDPRConsentGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	gdprMgr, err := r.getGDPRManager(ctx)
	if err != nil {
		return nil, err
	}

	consent, err := gdprMgr.GetConsent(ctx,
		getString(args, "tenant_id"),
		getString(args, "user_id"),
		iam.ConsentPurpose(getString(args, "purpose")),
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(consent)
}

func (r *ToolRegistry) executeIAMGDPRConsentList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	gdprMgr, err := r.getGDPRManager(ctx)
	if err != nil {
		return nil, err
	}

	consents, err := gdprMgr.ListUserConsents(ctx,
		getString(args, "tenant_id"),
		getString(args, "user_id"),
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"consents":  consents,
		"timestamp": time.Now(),
	})
}

// =============================================================================
// Export Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeIAMGDPRExportRequest(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	gdprMgr, err := r.getGDPRManager(ctx)
	if err != nil {
		return nil, err
	}

	format := getString(args, "format")
	if format == "" {
		format = "json"
	}

	// Parse include categories
	var includeCategories []string
	if categoriesRaw, ok := args["include_categories"].([]interface{}); ok {
		includeCategories = make([]string, len(categoriesRaw))
		for i, cat := range categoriesRaw {
			if str, ok := cat.(string); ok {
				includeCategories[i] = str
			}
		}
	}

	// Parse exclude categories
	var excludeCategories []string
	if categoriesRaw, ok := args["exclude_categories"].([]interface{}); ok {
		excludeCategories = make([]string, len(categoriesRaw))
		for i, cat := range categoriesRaw {
			if str, ok := cat.(string); ok {
				excludeCategories[i] = str
			}
		}
	}

	exportReq, err := gdprMgr.RequestExport(ctx,
		getString(args, "tenant_id"),
		getString(args, "user_id"),
		getString(args, "requested_by"),
		iam.DataExportFormat(format),
		includeCategories,
		excludeCategories,
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(exportReq)
}

func (r *ToolRegistry) executeIAMGDPRExportStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	gdprMgr, err := r.getGDPRManager(ctx)
	if err != nil {
		return nil, err
	}

	exportReq, err := gdprMgr.GetExportStatus(ctx, getString(args, "export_id"))
	if err != nil {
		return nil, err
	}
	return marshalResult(exportReq)
}

func (r *ToolRegistry) executeIAMGDPRExportGenerate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	gdprMgr, err := r.getGDPRManager(ctx)
	if err != nil {
		return nil, err
	}

	export, err := gdprMgr.GenerateExport(ctx, getString(args, "export_id"))
	if err != nil {
		return nil, err
	}
	return marshalResult(export)
}

// =============================================================================
// Deletion Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeIAMGDPRDeletionRequest(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	gdprMgr, err := r.getGDPRManager(ctx)
	if err != nil {
		return nil, err
	}

	gracePeriodDays := int(getUint64(args, "grace_period_days"))
	if gracePeriodDays == 0 {
		gracePeriodDays = 30
	}

	// Parse delete categories
	var deleteCategories []string
	if categoriesRaw, ok := args["delete_categories"].([]interface{}); ok {
		deleteCategories = make([]string, len(categoriesRaw))
		for i, cat := range categoriesRaw {
			if str, ok := cat.(string); ok {
				deleteCategories[i] = str
			}
		}
	}

	// Parse preserve categories
	var preserveCategories []string
	if categoriesRaw, ok := args["preserve_categories"].([]interface{}); ok {
		preserveCategories = make([]string, len(categoriesRaw))
		for i, cat := range categoriesRaw {
			if str, ok := cat.(string); ok {
				preserveCategories[i] = str
			}
		}
	}

	deletionReq, err := gdprMgr.RequestDeletion(ctx,
		getString(args, "tenant_id"),
		getString(args, "user_id"),
		getString(args, "requested_by"),
		getString(args, "reason"),
		deleteCategories,
		preserveCategories,
		gracePeriodDays,
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(deletionReq)
}

func (r *ToolRegistry) executeIAMGDPRDeletionCancel(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	gdprMgr, err := r.getGDPRManager(ctx)
	if err != nil {
		return nil, err
	}

	err = gdprMgr.CancelDeletion(ctx,
		getString(args, "deletion_id"),
		getString(args, "cancelled_by"),
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"cancelled": true,
		"timestamp": time.Now(),
	})
}

func (r *ToolRegistry) executeIAMGDPRDeletionProcess(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	gdprMgr, err := r.getGDPRManager(ctx)
	if err != nil {
		return nil, err
	}

	summary, err := gdprMgr.ProcessDeletion(ctx, getString(args, "deletion_id"))
	if err != nil {
		return nil, err
	}
	return marshalResult(summary)
}

func (r *ToolRegistry) executeIAMGDPRDeletionStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	gdprMgr, err := r.getGDPRManager(ctx)
	if err != nil {
		return nil, err
	}

	deletionReq, err := gdprMgr.GetDeletionStatus(ctx, getString(args, "deletion_id"))
	if err != nil {
		return nil, err
	}
	return marshalResult(deletionReq)
}
