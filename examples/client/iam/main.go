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

// Example: Identity and Access Management (IAM)
//
// This example demonstrates:
// - Connecting to QuicBaaS using the SDK with multiprotocol support
// - Creating and managing tenants (multi-tenant architecture)
// - Creating users with roles and permissions
// - Managing groups for user organization
// - Creating roles with fine-grained permissions
// - User lifecycle management (suspend/unsuspend)
// - Role assignment and revocation
//
// Prerequisites:
// - A running QuicBaaS server (default: localhost:50051)
// - Server must be initialized and unsealed
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

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func main() {
	// Parse command-line flags
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	flag.Parse()

	fmt.Println("=== QuicBaaS IAM Example ===")
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

	// Step 1: Create a tenant
	fmt.Println("Step 1: Creating a tenant...")
	tenant, err := client.IAM().Tenant().Create(ctx, &iam.CreateTenantRequest{
		Name:        "acme-corp",
		DisplayName: "ACME Corporation",
		Description: "Example tenant for ACME Corporation",
		Settings: map[string]string{
			"mfa_required": "true",
			"max_sessions": "5",
		},
		Metadata: map[string]string{
			"industry":   "technology",
			"tier":       "enterprise",
			"created_by": "admin",
		},
	})
	if err != nil {
		log.Fatalf("Failed to create tenant: %v", err)
	}
	tenantID := tenant.ID
	fmt.Printf("  Tenant ID:    %s\n", tenant.ID)
	fmt.Printf("  Name:         %s\n", tenant.Name)
	fmt.Printf("  Display Name: %s\n", tenant.DisplayName)
	fmt.Printf("  Status:       %s\n", tenant.Status)
	fmt.Printf("  Created:      %s\n", tenant.CreatedAt.Format(time.RFC3339))

	// Step 2: Create roles with different permission sets
	fmt.Println("\nStep 2: Creating roles...")

	// Admin role with full permissions
	adminRole, err := client.IAM().Role().Create(ctx, tenantID, &iam.CreateRoleRequest{
		Name:        "admin",
		DisplayName: "Administrator",
		Description: "Full administrative access",
		Permissions: []string{
			"quicbaas:*:*", // Full access to all QuicBaaS resources
			"iam:users:*",  // User management
			"iam:roles:*",  // Role management
			"iam:groups:*", // Group management
			"secrets:*:*",  // Secrets management
			"kms:*:*",      // KMS management
		},
		Metadata: map[string]string{
			"priority": "high",
		},
	})
	if err != nil {
		log.Fatalf("Failed to create admin role: %v", err)
	}
	fmt.Printf("  Created role: %s (ID: %s)\n", adminRole.Name, adminRole.ID)

	// Developer role with limited permissions
	devRole, err := client.IAM().Role().Create(ctx, tenantID, &iam.CreateRoleRequest{
		Name:        "developer",
		DisplayName: "Developer",
		Description: "Read/write access to development resources",
		Permissions: []string{
			"quicbaas:data:read",  // Read data
			"quicbaas:data:write", // Write data
			"secrets:dev/*:read",  // Read dev secrets
			"kms:keys:use",        // Use (but not manage) keys
		},
	})
	if err != nil {
		log.Fatalf("Failed to create developer role: %v", err)
	}
	fmt.Printf("  Created role: %s (ID: %s)\n", devRole.Name, devRole.ID)

	// Viewer role with read-only permissions
	viewerRole, err := client.IAM().Role().Create(ctx, tenantID, &iam.CreateRoleRequest{
		Name:        "viewer",
		DisplayName: "Viewer",
		Description: "Read-only access",
		Permissions: []string{
			"quicbaas:data:read", // Read data only
		},
	})
	if err != nil {
		log.Fatalf("Failed to create viewer role: %v", err)
	}
	fmt.Printf("  Created role: %s (ID: %s)\n", viewerRole.Name, viewerRole.ID)

	// Step 3: Create groups
	fmt.Println("\nStep 3: Creating groups...")

	engineeringGroup, err := client.IAM().Group().Create(ctx, tenantID, &iam.CreateGroupRequest{
		Name:        "engineering",
		DisplayName: "Engineering Team",
		Description: "Software engineering department",
		Roles:       []string{devRole.ID},
	})
	if err != nil {
		log.Fatalf("Failed to create engineering group: %v", err)
	}
	fmt.Printf("  Created group: %s (ID: %s)\n", engineeringGroup.Name, engineeringGroup.ID)

	opsGroup, err := client.IAM().Group().Create(ctx, tenantID, &iam.CreateGroupRequest{
		Name:        "operations",
		DisplayName: "Operations Team",
		Description: "DevOps and SRE department",
		Roles:       []string{adminRole.ID},
	})
	if err != nil {
		log.Fatalf("Failed to create operations group: %v", err)
	}
	fmt.Printf("  Created group: %s (ID: %s)\n", opsGroup.Name, opsGroup.ID)

	// Step 4: Create users
	fmt.Println("\nStep 4: Creating users...")

	// Admin user
	adminUser, err := client.IAM().User().Create(ctx, tenantID, &iam.CreateUserRequest{
		Username:    "admin.user",
		Email:       "admin@acme-corp.com",
		Password:    "SecureAdminP@ss123!",
		DisplayName: "Admin User",
		Roles:       []string{adminRole.ID},
		Metadata: map[string]string{
			"department": "IT",
			"title":      "System Administrator",
		},
	})
	if err != nil {
		log.Fatalf("Failed to create admin user: %v", err)
	}
	fmt.Printf("  Created user: %s (ID: %s)\n", adminUser.Username, adminUser.ID)

	// Developer user (added to engineering group)
	devUser, err := client.IAM().User().Create(ctx, tenantID, &iam.CreateUserRequest{
		Username:    "dev.user",
		Email:       "developer@acme-corp.com",
		Password:    "SecureDevP@ss456!",
		DisplayName: "Developer User",
		Groups:      []string{engineeringGroup.ID},
		Metadata: map[string]string{
			"department": "Engineering",
			"title":      "Senior Developer",
		},
	})
	if err != nil {
		log.Fatalf("Failed to create developer user: %v", err)
	}
	fmt.Printf("  Created user: %s (ID: %s)\n", devUser.Username, devUser.ID)

	// Viewer user
	viewerUser, err := client.IAM().User().Create(ctx, tenantID, &iam.CreateUserRequest{
		Username:    "viewer.user",
		Email:       "viewer@acme-corp.com",
		Password:    "SecureViewP@ss789!",
		DisplayName: "Viewer User",
		Roles:       []string{viewerRole.ID},
		Metadata: map[string]string{
			"department": "Support",
			"title":      "Support Analyst",
		},
	})
	if err != nil {
		log.Fatalf("Failed to create viewer user: %v", err)
	}
	fmt.Printf("  Created user: %s (ID: %s)\n", viewerUser.Username, viewerUser.ID)

	// Step 5: List all users in tenant
	fmt.Println("\nStep 5: Listing all users...")
	usersResp, err := client.IAM().User().List(ctx, tenantID, nil)
	if err != nil {
		log.Fatalf("Failed to list users: %v", err)
	}
	fmt.Printf("  Found %d users:\n", len(usersResp.Users))
	for _, u := range usersResp.Users {
		fmt.Printf("    - %s <%s> (Status: %s)\n", u.Username, u.Email, u.Status)
	}

	// Step 6: Get user details
	fmt.Println("\nStep 6: Getting user details...")
	userDetails, err := client.IAM().User().Get(ctx, tenantID, devUser.ID)
	if err != nil {
		log.Fatalf("Failed to get user: %v", err)
	}
	fmt.Printf("  Username:    %s\n", userDetails.Username)
	fmt.Printf("  Email:       %s\n", userDetails.Email)
	fmt.Printf("  DisplayName: %s\n", userDetails.DisplayName)
	fmt.Printf("  Status:      %s\n", userDetails.Status)
	fmt.Printf("  Groups:      %v\n", userDetails.Groups)
	fmt.Printf("  Roles:       %v\n", userDetails.Roles)
	fmt.Printf("  Created:     %s\n", userDetails.CreatedAt.Format(time.RFC3339))

	// Step 7: Assign additional role to user
	fmt.Println("\nStep 7: Assigning viewer role to developer user...")
	if err := client.IAM().User().AssignRole(ctx, tenantID, devUser.ID, viewerRole.ID); err != nil {
		log.Fatalf("Failed to assign role: %v", err)
	}
	fmt.Printf("  Assigned role '%s' to user '%s'\n", viewerRole.Name, devUser.Username)

	// Verify the assignment
	userDetails, _ = client.IAM().User().Get(ctx, tenantID, devUser.ID)
	fmt.Printf("  User now has roles: %v\n", userDetails.Roles)

	// Step 8: Add user to group
	fmt.Println("\nStep 8: Adding viewer user to engineering group...")
	if err := client.IAM().User().AddToGroup(ctx, tenantID, viewerUser.ID, engineeringGroup.ID); err != nil {
		log.Fatalf("Failed to add user to group: %v", err)
	}
	fmt.Printf("  Added user '%s' to group '%s'\n", viewerUser.Username, engineeringGroup.Name)

	// Step 9: Suspend a user
	fmt.Println("\nStep 9: Suspending viewer user...")
	if err := client.IAM().User().Suspend(ctx, tenantID, viewerUser.ID); err != nil {
		log.Fatalf("Failed to suspend user: %v", err)
	}
	fmt.Printf("  Suspended user: %s\n", viewerUser.Username)

	// Verify suspension
	userDetails, _ = client.IAM().User().Get(ctx, tenantID, viewerUser.ID)
	fmt.Printf("  User status: %s\n", userDetails.Status)

	// Step 10: Unsuspend the user
	fmt.Println("\nStep 10: Unsuspending viewer user...")
	if err := client.IAM().User().Unsuspend(ctx, tenantID, viewerUser.ID); err != nil {
		log.Fatalf("Failed to unsuspend user: %v", err)
	}
	fmt.Printf("  Unsuspended user: %s\n", viewerUser.Username)

	// Verify unsuspension
	userDetails, _ = client.IAM().User().Get(ctx, tenantID, viewerUser.ID)
	fmt.Printf("  User status: %s\n", userDetails.Status)

	// Step 11: List all roles
	fmt.Println("\nStep 11: Listing all roles...")
	rolesResp, err := client.IAM().Role().List(ctx, tenantID, nil)
	if err != nil {
		log.Fatalf("Failed to list roles: %v", err)
	}
	fmt.Printf("  Found %d roles:\n", len(rolesResp.Roles))
	for _, r := range rolesResp.Roles {
		fmt.Printf("    - %s: %d permissions\n", r.Name, len(r.Permissions))
	}

	// Step 12: Update tenant settings
	fmt.Println("\nStep 12: Updating tenant settings...")
	updatedTenant, err := client.IAM().Tenant().Update(ctx, tenantID, &iam.UpdateTenantRequest{
		Description: "ACME Corporation - Updated description",
		Settings: map[string]string{
			"mfa_required":    "true",
			"max_sessions":    "10",
			"session_timeout": "3600",
		},
	})
	if err != nil {
		log.Fatalf("Failed to update tenant: %v", err)
	}
	fmt.Printf("  Updated tenant: %s\n", updatedTenant.Name)
	fmt.Printf("  New description: %s\n", updatedTenant.Description)

	// Step 13: Cleanup - Delete all resources
	fmt.Println("\nStep 13: Cleaning up resources...")

	// Delete users
	for _, u := range []struct {
		id   string
		name string
	}{
		{adminUser.ID, adminUser.Username},
		{devUser.ID, devUser.Username},
		{viewerUser.ID, viewerUser.Username},
	} {
		if err := client.IAM().User().Delete(ctx, tenantID, u.id); err != nil {
			log.Printf("  Warning: Failed to delete user %s: %v", u.name, err)
		} else {
			fmt.Printf("  Deleted user: %s\n", u.name)
		}
	}

	// Delete groups
	for _, g := range []struct {
		id   string
		name string
	}{
		{engineeringGroup.ID, engineeringGroup.Name},
		{opsGroup.ID, opsGroup.Name},
	} {
		if err := client.IAM().Group().Delete(ctx, tenantID, g.id); err != nil {
			log.Printf("  Warning: Failed to delete group %s: %v", g.name, err)
		} else {
			fmt.Printf("  Deleted group: %s\n", g.name)
		}
	}

	// Delete roles
	for _, r := range []struct {
		id   string
		name string
	}{
		{adminRole.ID, adminRole.Name},
		{devRole.ID, devRole.Name},
		{viewerRole.ID, viewerRole.Name},
	} {
		if err := client.IAM().Role().Delete(ctx, tenantID, r.id); err != nil {
			log.Printf("  Warning: Failed to delete role %s: %v", r.name, err)
		} else {
			fmt.Printf("  Deleted role: %s\n", r.name)
		}
	}

	// Delete tenant
	if err := client.IAM().Tenant().Delete(ctx, tenantID); err != nil {
		log.Printf("  Warning: Failed to delete tenant: %v", err)
	} else {
		fmt.Printf("  Deleted tenant: %s\n", tenant.Name)
	}

	fmt.Println("\n=== Example Complete ===")
	fmt.Println("\nKey Takeaways:")
	fmt.Println("  1. Multi-tenant architecture isolates resources per organization")
	fmt.Println("  2. Roles define bundles of permissions (RBAC)")
	fmt.Println("  3. Groups simplify permission management for teams")
	fmt.Println("  4. Users can have direct roles and inherit from groups")
	fmt.Println("  5. User lifecycle management (suspend/unsuspend) for security")
	fmt.Println("  6. The SDK supports multiple transports (gRPC, REST, QUIC)")
}
