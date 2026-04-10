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

// Package iam provides Identity and Access Management (IAM) operations for QuicBaaS.
//
// The IAM client provides comprehensive identity management with support for:
//   - Multi-tenant architecture with isolated user bases
//   - Attribute-Based Access Control (ABAC) policies
//   - OAuth2/OIDC application registration
//   - Session management and audit logging
//   - GDPR compliance operations (consent, data export, deletion)
//   - Delegated administration with scoped permissions
//
// # Getting Started
//
// Create an IAM client using a transport:
//
//	client := iam.NewClient(transport)
//
// The client provides access to all IAM sub-clients:
//   - client.Tenant() - Multi-tenant management
//   - client.User() - User management
//   - client.Group() - Group management
//   - client.Role() - Role management
//   - client.Policy() - ABAC policy management
//   - client.PEP() - Policy Enforcement Point (authorization checks)
//   - client.App() - OAuth2 application management
//   - client.Session() - Session management
//   - client.Audit() - Audit logging
//   - client.GDPR() - GDPR compliance operations
//   - client.Admin() - Delegated administration
//
// # Multi-Tenant Architecture
//
// QuicBaaS IAM supports multi-tenant deployments where each tenant has
// isolated users, groups, roles, policies, and applications:
//
//	// Create a new tenant
//	tenant, err := client.Tenant().Create(ctx, &iam.CreateTenantRequest{
//	    Name:        "acme-corp",
//	    DisplayName: "Acme Corporation",
//	})
//
//	// Create a user in the tenant
//	user, err := client.User().Create(ctx, tenant.ID, &iam.CreateUserRequest{
//	    Username: "john.doe",
//	    Email:    "john.doe@acme.com",
//	    Password: "secure-password",
//	})
//
// # Attribute-Based Access Control (ABAC)
//
// ABAC policies allow fine-grained access control based on subject attributes,
// resource properties, actions, and environmental conditions:
//
//	// Create an ABAC policy
//	policy, err := client.Policy().Create(ctx, tenantID, &iam.CreatePolicyRequest{
//	    Name:        "order-access",
//	    Description: "Order service access policy",
//	    Rules: []iam.Rule{
//	        {
//	            Name:   "read-own-orders",
//	            Effect: iam.EffectAllow,
//	            Actions: []iam.ActionMatcher{
//	                {Pattern: "orders:read"},
//	            },
//	            Resources: []iam.ResourceMatcher{
//	                {Pattern: "orders://${subject.id}/*"},
//	            },
//	        },
//	    },
//	})
//
//	// Check authorization via Policy Enforcement Point (PEP)
//	result, err := client.PEP().Check(ctx, &iam.CheckRequest{
//	    Subject: iam.CheckSubject{
//	        ID:       "user_abc123",
//	        TenantID: "acme-corp",
//	        Roles:    []string{"developer"},
//	    },
//	    Action:   "orders:read",
//	    Resource: "orders://user_abc123/order-456",
//	})
//	if result.Allowed {
//	    fmt.Println("Access granted")
//	}
//
// # OAuth2 Applications
//
// Register and manage OAuth2/OIDC client applications:
//
//	// Register a new application
//	app, err := client.App().Register(ctx, tenantID, &iam.RegisterAppRequest{
//	    Name:         "order-service",
//	    Type:         iam.ApplicationTypeService,
//	    RedirectURIs: []string{"https://orders.acme.com/callback"},
//	    GrantTypes:   []iam.GrantType{iam.GrantTypeClientCredentials},
//	})
//	fmt.Printf("Client ID: %s\n", app.ClientID)
//	fmt.Printf("Client Secret: %s\n", app.ClientSecret)
//
//	// Rotate client secret
//	rotated, err := client.App().RotateSecret(ctx, tenantID, app.ClientID)
//
// # Session Management
//
// Manage user sessions across devices and applications:
//
//	// List all sessions for a user
//	sessions, err := client.Session().List(ctx, &iam.ListSessionsRequest{
//	    TenantID: tenantID,
//	    UserID:   userID,
//	})
//
//	// Revoke a specific session
//	err = client.Session().Revoke(ctx, sessionID)
//
//	// Revoke all other sessions (security measure)
//	err = client.Session().RevokeOthers(ctx, currentSessionID)
//
// # Audit Logging
//
// Query and manage audit events for compliance:
//
//	// Query audit events
//	events, err := client.Audit().Query(ctx, &iam.AuditFilter{
//	    TenantID:  tenantID,
//	    EventType: iam.AuditEventTypeAuthentication,
//	    StartTime: time.Now().Add(-24 * time.Hour),
//	    EndTime:   time.Now(),
//	    Limit:     100,
//	})
//
//	for _, event := range events.Events {
//	    fmt.Printf("[%s] %s by %s: %s\n",
//	        event.Timestamp.Format(time.RFC3339),
//	        event.Action,
//	        event.Actor.ID,
//	        event.Outcome,
//	    )
//	}
//
// # GDPR Compliance
//
// Handle GDPR requirements for data subject rights:
//
//	// Record user consent
//	err := client.GDPR().GrantConsent(ctx, tenantID, userID, &iam.GrantConsentRequest{
//	    Purpose:    "marketing",
//	    LegalBasis: iam.LegalBasisConsent,
//	})
//
//	// Request data export (right to data portability)
//	export, err := client.GDPR().RequestExport(ctx, tenantID, userID, &iam.ExportRequest{
//	    Format: iam.ExportFormatJSON,
//	})
//
//	// Request data deletion (right to be forgotten)
//	deletion, err := client.GDPR().RequestDeletion(ctx, tenantID, userID, &iam.DeletionRequest{
//	    Reason: "User requested account deletion",
//	})
//
// # Delegated Administration
//
// Assign administrative roles to users for delegated management:
//
//	// Create an admin assignment
//	assignment, err := client.Admin().CreateAssignment(ctx, &iam.CreateAdminAssignmentRequest{
//	    AdminID:  userID,
//	    TenantID: tenantID,
//	    Scope:    iam.AdminScopeTenant,
//	    ScopeID:  tenantID,
//	    Permissions: []string{
//	        "users:read", "users:write",
//	        "groups:read", "groups:write",
//	    },
//	})
//
//	// Check admin permission
//	check, err := client.Admin().CheckPermission(ctx, &iam.AdminCheckRequest{
//	    AdminID:    userID,
//	    TenantID:   tenantID,
//	    Permission: "users:write",
//	})
//
// # Error Handling
//
// The IAM client returns typed errors for different failure conditions:
//
//	user, err := client.User().Get(ctx, tenantID, "nonexistent")
//	if errors.IsNotFound(err) {
//	    fmt.Println("User not found")
//	} else if errors.IsForbidden(err) {
//	    fmt.Println("Access denied")
//	} else if errors.IsInvalidInput(err) {
//	    fmt.Println("Invalid request:", err)
//	}
package iam
