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

// Example: Attribute-Based Access Control (ABAC)
//
// This example demonstrates:
// - Creating ABAC policies with fine-grained rules
// - Using subject matchers (users, groups, roles)
// - Using action and resource matchers with wildcards
// - Defining conditions (time-based, IP-based, attribute-based)
// - Validating policies before deployment
// - Testing policies with sample requests
// - Simulating authorization decisions
// - Retrieving effective permissions for a subject
//
// Prerequisites:
// - A running QuicBaaS server (default: localhost:50051)
// - Server must be initialized and unsealed
// - A tenant must exist (example creates "demo-tenant")
//
// Usage:
//
//	# Run with default gRPC transport
//	go run main.go
//
//	# Run with REST transport
//	go run main.go -transport rest -address http://localhost:8080
//
//	# Run with QUIC transport
//	go run main.go -transport quic -address localhost:50052
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

const tenantID = "demo-tenant"

func main() {
	// Parse command-line flags
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	flag.Parse()

	fmt.Println("=== QuicBaaS ABAC Example ===")
	fmt.Printf("Transport: %s\n", *transportType)
	fmt.Printf("Address:   %s\n", *address)
	fmt.Println()

	// Create client with the specified transport
	var opts []quicbaas.Option
	opts = append(opts, quicbaas.WithTimeout(30*time.Second))
	// Select transport based on flag
	switch *transportType {
	case "rest":
		opts = append(opts, quicbaas.WithREST())
	case "quic":
		opts = append(opts, quicbaas.WithQUIC())
		// default is gRPC, no option needed
	}

	client, err := quicbaas.NewClient(*address, opts...)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Ensure tenant exists
	if err := ensureTenantExists(ctx, client); err != nil {
		log.Fatalf("Failed to ensure tenant exists: %v", err)
	}

	// Step 1: Create an ABAC policy
	fmt.Println("Step 1: Creating ABAC policy...")
	policy, err := createDataAccessPolicy(ctx, client)
	if err != nil {
		log.Fatalf("Failed to create policy: %v", err)
	}
	fmt.Printf("  Created policy: %s (version %d)\n", policy.Name, policy.Version)
	fmt.Println()

	// Step 2: Validate a policy before deployment
	fmt.Println("Step 2: Validating policy...")
	if err := validatePolicy(ctx, client, policy); err != nil {
		log.Fatalf("Policy validation failed: %v", err)
	}
	fmt.Println()

	// Step 3: Test policy with sample requests
	fmt.Println("Step 3: Testing policy with sample requests...")
	if err := testPolicyWithCases(ctx, client, policy.Name); err != nil {
		log.Fatalf("Policy testing failed: %v", err)
	}
	fmt.Println()

	// Step 4: Simulate authorization decisions
	fmt.Println("Step 4: Simulating authorization decisions...")
	if err := simulateAuthorization(ctx, client); err != nil {
		log.Fatalf("Authorization simulation failed: %v", err)
	}
	fmt.Println()

	// Step 5: Get effective permissions
	fmt.Println("Step 5: Getting effective permissions...")
	if err := getEffectivePermissions(ctx, client); err != nil {
		log.Fatalf("Failed to get effective permissions: %v", err)
	}
	fmt.Println()

	// Step 6: Create a time-based policy
	fmt.Println("Step 6: Creating time-based policy...")
	timePolicy, err := createTimeBasedPolicy(ctx, client)
	if err != nil {
		log.Fatalf("Failed to create time-based policy: %v", err)
	}
	fmt.Printf("  Created policy: %s\n", timePolicy.Name)
	fmt.Println()

	// Step 7: Create an IP-based policy
	fmt.Println("Step 7: Creating IP-based policy...")
	ipPolicy, err := createIPBasedPolicy(ctx, client)
	if err != nil {
		log.Fatalf("Failed to create IP-based policy: %v", err)
	}
	fmt.Printf("  Created policy: %s\n", ipPolicy.Name)
	fmt.Println()

	// Step 8: Cleanup - Delete policies
	fmt.Println("Step 8: Cleaning up policies...")
	policies := []string{policy.Name, timePolicy.Name, ipPolicy.Name}
	for _, name := range policies {
		if err := client.Identity().IAM().Policy().Delete(ctx, tenantID, name); err != nil {
			fmt.Printf("  Warning: failed to delete policy %s: %v\n", name, err)
		} else {
			fmt.Printf("  Deleted policy: %s\n", name)
		}
	}
	fmt.Println()

	fmt.Println("=== ABAC Example Complete ===")
}

// ensureTenantExists creates the demo tenant if it doesn't exist.
func ensureTenantExists(ctx context.Context, client quicbaas.Client) error {
	// Try to get the tenant first
	_, err := client.Identity().IAM().Tenant().Get(ctx, tenantID)
	if err == nil {
		return nil // Tenant exists
	}

	// Create the tenant
	_, err = client.Identity().IAM().Tenant().Create(ctx, &iam.CreateTenantRequest{
		Name:        tenantID,
		DisplayName: "Demo Tenant",
		Description: "Tenant for ABAC examples",
	})
	return err
}

// createDataAccessPolicy creates a comprehensive data access policy.
func createDataAccessPolicy(ctx context.Context, client quicbaas.Client) (*iam.Policy, error) {
	return client.Identity().IAM().Policy().Create(ctx, tenantID, &iam.CreatePolicyRequest{
		Name:        "data-access-policy",
		Description: "Policy for controlling access to data resources",
		Enabled:     true,
		Priority:    100,
		Effect:      iam.EffectDeny, // Default deny
		Rules: []iam.Rule{
			{
				Name:        "allow-admins-full-access",
				Description: "Administrators have full access to all data",
				Effect:      iam.EffectAllow,
				Subjects: []iam.SubjectMatcher{
					{
						Type: iam.SubjectTypeRole,
						ID:   "admin",
					},
				},
				Actions: []iam.ActionMatcher{
					{Pattern: "data:*"}, // All data actions
				},
				Resources: []iam.ResourceMatcher{
					{Pattern: "data://*"}, // All data resources
				},
				Priority: 100,
			},
			{
				Name:        "allow-users-read",
				Description: "Regular users can read non-sensitive data",
				Effect:      iam.EffectAllow,
				Subjects: []iam.SubjectMatcher{
					{
						Type: iam.SubjectTypeRole,
						ID:   "user",
					},
				},
				Actions: []iam.ActionMatcher{
					{Pattern: "data:read"},
					{Pattern: "data:list"},
				},
				Resources: []iam.ResourceMatcher{
					{
						Pattern: "data://*",
						Attributes: map[string]iam.AttributeMatcher{
							"sensitivity": {
								Operator: iam.ComparisonNotIn,
								Values:   []interface{}{"confidential", "restricted"},
							},
						},
					},
				},
				Priority: 50,
			},
			{
				Name:        "allow-owner-write",
				Description: "Resource owners can write to their own resources",
				Effect:      iam.EffectAllow,
				Subjects: []iam.SubjectMatcher{
					{
						Type: iam.SubjectTypeAny,
						Attributes: map[string]iam.AttributeMatcher{
							"id": {
								Operator: iam.ComparisonEquals,
								Value:    "${resource.owner_id}",
							},
						},
					},
				},
				Actions: []iam.ActionMatcher{
					{Pattern: "data:write"},
					{Pattern: "data:update"},
					{Pattern: "data:delete"},
				},
				Resources: []iam.ResourceMatcher{
					{Pattern: "data://*"},
				},
				Priority: 75,
			},
			{
				Name:        "deny-delete-audit-logs",
				Description: "No one can delete audit logs",
				Effect:      iam.EffectDeny,
				Subjects: []iam.SubjectMatcher{
					{Type: iam.SubjectTypeAny},
				},
				Actions: []iam.ActionMatcher{
					{Pattern: "data:delete"},
				},
				Resources: []iam.ResourceMatcher{
					{Pattern: "data://audit/*"},
				},
				Priority: 200, // High priority to override other rules
			},
		},
		Targets: []iam.Target{
			{
				ResourceTypes: []string{"data"},
				Actions:       []string{"read", "write", "delete", "list", "update"},
			},
		},
		Labels: map[string]string{
			"category":    "data-access",
			"environment": "production",
		},
	})
}

// validatePolicy validates a policy and reports any issues.
func validatePolicy(ctx context.Context, client quicbaas.Client, policy *iam.Policy) error {
	result, err := client.Identity().IAM().Policy().Validate(ctx, tenantID, policy)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "validation request failed", Cause: err}
	}

	if result.Valid {
		fmt.Println("  Policy is valid")
	} else {
		fmt.Println("  Policy validation failed:")
		for _, e := range result.Errors {
			fmt.Printf("    Error [%s] %s: %s\n", e.Code, e.Field, e.Message)
		}
	}

	if len(result.Warnings) > 0 {
		fmt.Println("  Warnings:")
		for _, w := range result.Warnings {
			fmt.Printf("    Warning [%s] %s: %s\n", w.Code, w.Field, w.Message)
		}
	}

	return nil
}

// testPolicyWithCases tests a policy with predefined test cases.
func testPolicyWithCases(ctx context.Context, client quicbaas.Client, policyName string) error {
	resp, err := client.Identity().IAM().Policy().Test(ctx, tenantID, &iam.TestPolicyRequest{
		PolicyName: policyName,
		TestCases: []iam.TestCase{
			{
				Name:        "admin-can-read",
				Description: "Admin should be able to read any data",
				Subject: iam.Subject{
					ID:       "admin-user-1",
					Type:     iam.SubjectTypeUser,
					TenantID: tenantID,
					Roles:    []string{"admin"},
				},
				Action:         "data:read",
				Resource:       "data://confidential/secrets",
				ExpectedResult: iam.EffectAllow,
			},
			{
				Name:        "admin-can-delete",
				Description: "Admin should be able to delete data",
				Subject: iam.Subject{
					ID:       "admin-user-1",
					Type:     iam.SubjectTypeUser,
					TenantID: tenantID,
					Roles:    []string{"admin"},
				},
				Action:         "data:delete",
				Resource:       "data://documents/report.pdf",
				ExpectedResult: iam.EffectAllow,
			},
			{
				Name:        "user-can-read-public",
				Description: "Regular user should be able to read public data",
				Subject: iam.Subject{
					ID:       "regular-user-1",
					Type:     iam.SubjectTypeUser,
					TenantID: tenantID,
					Roles:    []string{"user"},
				},
				Action:   "data:read",
				Resource: "data://public/readme.txt",
				Context: map[string]interface{}{
					"resource.sensitivity": "public",
				},
				ExpectedResult: iam.EffectAllow,
			},
			{
				Name:        "user-cannot-delete",
				Description: "Regular user should not be able to delete data",
				Subject: iam.Subject{
					ID:       "regular-user-1",
					Type:     iam.SubjectTypeUser,
					TenantID: tenantID,
					Roles:    []string{"user"},
				},
				Action:         "data:delete",
				Resource:       "data://documents/report.pdf",
				ExpectedResult: iam.EffectDeny,
			},
			{
				Name:        "no-one-deletes-audit-logs",
				Description: "Even admin cannot delete audit logs",
				Subject: iam.Subject{
					ID:       "admin-user-1",
					Type:     iam.SubjectTypeUser,
					TenantID: tenantID,
					Roles:    []string{"admin"},
				},
				Action:         "data:delete",
				Resource:       "data://audit/2024-01-login.log",
				ExpectedResult: iam.EffectDeny,
			},
		},
	})
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "test request failed", Cause: err}
	}

	fmt.Printf("  Test Results: %d passed, %d failed\n", resp.Passed, resp.Failed)
	for _, result := range resp.Results {
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
		}
		fmt.Printf("    [%s] %s (expected: %s, got: %s)\n",
			status, result.Name, result.ExpectedResult, result.ActualResult)
		if result.MatchedRule != "" {
			fmt.Printf("           Matched rule: %s\n", result.MatchedRule)
		}
	}

	return nil
}

// simulateAuthorization demonstrates authorization simulation.
func simulateAuthorization(ctx context.Context, client quicbaas.Client) error {
	scenarios := []struct {
		name     string
		subject  iam.Subject
		action   string
		resource string
		context  map[string]interface{}
	}{
		{
			name: "Admin reading confidential data",
			subject: iam.Subject{
				ID:       "admin-user-1",
				Type:     iam.SubjectTypeUser,
				TenantID: tenantID,
				Roles:    []string{"admin"},
			},
			action:   "data:read",
			resource: "data://confidential/quarterly-report.xlsx",
		},
		{
			name: "User reading public data",
			subject: iam.Subject{
				ID:       "user-42",
				Type:     iam.SubjectTypeUser,
				TenantID: tenantID,
				Roles:    []string{"user"},
			},
			action:   "data:read",
			resource: "data://public/announcement.txt",
			context: map[string]interface{}{
				"resource.sensitivity": "public",
			},
		},
		{
			name: "Service account accessing API",
			subject: iam.Subject{
				ID:       "api-service",
				Type:     iam.SubjectTypeService,
				TenantID: tenantID,
				Roles:    []string{"service"},
				Attributes: map[string]interface{}{
					"service_type": "internal",
				},
			},
			action:   "data:write",
			resource: "data://cache/temp-data",
		},
	}

	for _, scenario := range scenarios {
		resp, err := client.Identity().IAM().Policy().Simulate(ctx, tenantID, &iam.SimulateRequest{
			Subject:      scenario.subject,
			Action:       scenario.action,
			Resource:     scenario.resource,
			Context:      scenario.context,
			IncludeTrace: true,
		})
		if err != nil {
			fmt.Printf("  [ERROR] %s: %v\n", scenario.name, err)
			continue
		}

		decision := "DENIED"
		if resp.Allowed {
			decision = "ALLOWED"
		}
		fmt.Printf("  %s: %s\n", scenario.name, decision)
		fmt.Printf("    Action: %s on %s\n", scenario.action, scenario.resource)
		fmt.Printf("    Evaluation time: %dns\n", resp.EvaluationTimeNs)

		if len(resp.MatchedPolicies) > 0 {
			fmt.Printf("    Matched policies: %v\n", resp.MatchedPolicies)
		}
		if len(resp.MatchedRules) > 0 {
			fmt.Printf("    Matched rules: %v\n", resp.MatchedRules)
		}
	}

	return nil
}

// getEffectivePermissions retrieves and displays effective permissions.
func getEffectivePermissions(ctx context.Context, client quicbaas.Client) error {
	subjects := []iam.Subject{
		{
			ID:       "admin-user-1",
			Type:     iam.SubjectTypeUser,
			TenantID: tenantID,
			Roles:    []string{"admin"},
		},
		{
			ID:       "regular-user-1",
			Type:     iam.SubjectTypeUser,
			TenantID: tenantID,
			Roles:    []string{"user"},
		},
	}

	for _, subject := range subjects {
		resp, err := client.Identity().IAM().Policy().GetEffectivePermissions(ctx, tenantID, &iam.EffectivePermissionsRequest{
			Subject:       subject,
			ResourceType:  "data",
			IncludeDenied: true,
		})
		if err != nil {
			fmt.Printf("  [ERROR] Failed to get permissions for %s: %v\n", subject.ID, err)
			continue
		}

		fmt.Printf("  Effective permissions for %s (roles: %v):\n", subject.ID, subject.Roles)
		for _, perm := range resp.Permissions {
			fmt.Printf("    %s %s on %s\n", perm.Effect, perm.Action, perm.ResourcePattern)
			if len(perm.Conditions) > 0 {
				fmt.Printf("      Conditions: %v\n", perm.Conditions)
			}
		}
		fmt.Printf("    Evaluated %d policies in %dns\n",
			len(resp.EvaluatedPolicies), resp.EvaluationTimeNs)
	}

	return nil
}

// createTimeBasedPolicy creates a policy with time-based conditions.
func createTimeBasedPolicy(ctx context.Context, client quicbaas.Client) (*iam.Policy, error) {
	return client.Identity().IAM().Policy().Create(ctx, tenantID, &iam.CreatePolicyRequest{
		Name:        "business-hours-policy",
		Description: "Allow access only during business hours (9AM-5PM weekdays)",
		Enabled:     true,
		Priority:    50,
		Effect:      iam.EffectDeny,
		Rules: []iam.Rule{
			{
				Name:        "allow-business-hours",
				Description: "Allow access during business hours",
				Effect:      iam.EffectAllow,
				Subjects: []iam.SubjectMatcher{
					{Type: iam.SubjectTypeUser},
				},
				Actions: []iam.ActionMatcher{
					{Pattern: "sensitive:*"},
				},
				Resources: []iam.ResourceMatcher{
					{Pattern: "sensitive://*"},
				},
				Conditions: []iam.Condition{
					{
						Type:        iam.ConditionTypeAnd,
						Description: "Must be during business hours",
						SubConditions: []iam.Condition{
							{
								Type:     iam.ConditionTypeTime,
								Key:      "hour_of_day",
								Operator: iam.ComparisonGreaterThanOrEqual,
								Value:    9,
							},
							{
								Type:     iam.ConditionTypeTime,
								Key:      "hour_of_day",
								Operator: iam.ComparisonLessThan,
								Value:    17,
							},
							{
								Type:     iam.ConditionTypeTime,
								Key:      "day_of_week",
								Operator: iam.ComparisonIn,
								Values:   []interface{}{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"},
							},
						},
					},
				},
				Priority: 100,
			},
		},
		Labels: map[string]string{
			"category": "time-based",
			"type":     "business-hours",
		},
	})
}

// createIPBasedPolicy creates a policy with IP-based conditions.
func createIPBasedPolicy(ctx context.Context, client quicbaas.Client) (*iam.Policy, error) {
	return client.Identity().IAM().Policy().Create(ctx, tenantID, &iam.CreatePolicyRequest{
		Name:        "network-access-policy",
		Description: "Restrict access based on source IP address",
		Enabled:     true,
		Priority:    75,
		Effect:      iam.EffectDeny,
		Rules: []iam.Rule{
			{
				Name:        "allow-internal-network",
				Description: "Allow full access from internal network",
				Effect:      iam.EffectAllow,
				Subjects: []iam.SubjectMatcher{
					{Type: iam.SubjectTypeAny},
				},
				Actions: []iam.ActionMatcher{
					{Pattern: "*"},
				},
				Resources: []iam.ResourceMatcher{
					{Pattern: "*"},
				},
				Conditions: []iam.Condition{
					{
						Type:        iam.ConditionTypeOr,
						Description: "Must be from internal network",
						SubConditions: []iam.Condition{
							{
								Type:     iam.ConditionTypeIP,
								Key:      "source_ip",
								Operator: iam.ComparisonStartsWith,
								Value:    "10.",
							},
							{
								Type:     iam.ConditionTypeIP,
								Key:      "source_ip",
								Operator: iam.ComparisonStartsWith,
								Value:    "192.0.2.",
							},
							{
								Type:     iam.ConditionTypeIP,
								Key:      "source_ip",
								Operator: iam.ComparisonEquals,
								Value:    "127.0.0.1",
							},
						},
					},
				},
				Priority: 100,
			},
			{
				Name:        "allow-vpn-read-only",
				Description: "Allow read-only access from VPN",
				Effect:      iam.EffectAllow,
				Subjects: []iam.SubjectMatcher{
					{Type: iam.SubjectTypeUser},
				},
				Actions: []iam.ActionMatcher{
					{Pattern: "*:read"},
					{Pattern: "*:list"},
				},
				Resources: []iam.ResourceMatcher{
					{Pattern: "*"},
				},
				Conditions: []iam.Condition{
					{
						Type:     iam.ConditionTypeIP,
						Key:      "source_ip",
						Operator: iam.ComparisonStartsWith,
						Value:    "172.16.",
					},
				},
				Priority: 50,
			},
		},
		Labels: map[string]string{
			"category": "network",
			"type":     "ip-based",
		},
	})
}
