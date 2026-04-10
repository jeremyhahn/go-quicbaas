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

// Package dynamicsecrets provides the dynamic secrets client for the QuicBaaS SDK.
//
// The dynamic secrets client provides lease-based credential generation for
// databases and other services. Credentials are auto-generated, time-limited,
// and automatically revoked on expiry.
//
// # Creating Roles
//
// Roles define how credentials are generated, including the generator type,
// configuration, and TTL limits.
//
//	client := dynamicsecrets.NewClient(transport)
//
//	// Create a PostgreSQL credential role
//	resp, err := client.CreateRole(ctx, &dynamicsecrets.CreateRoleRequest{
//	    Name:          "postgres-readonly",
//	    GeneratorType: "postgres",
//	    Config: map[string]string{
//	        "connection_url":      "postgres://admin:secret@localhost:5432/mydb",
//	        "creation_statements": "CREATE ROLE {{name}} WITH LOGIN PASSWORD '{{password}}' VALID UNTIL '{{expiration}}';",
//	    },
//	    DefaultTTL: time.Hour,
//	    MaxTTL:     24 * time.Hour,
//	})
//
// # Generating Credentials
//
// Generate dynamic credentials based on a role. Each call creates unique
// credentials with an associated lease.
//
//	// Generate credentials with default TTL
//	cred, err := client.GenerateCredential(ctx, &dynamicsecrets.GenerateCredentialRequest{
//	    RoleName: "postgres-readonly",
//	})
//
//	// Use the credentials
//	username := cred.Credential.Data["username"]
//	password := cred.Credential.Data["password"]
//
// # Managing Leases
//
// Leases track credential lifecycle and support renewal and revocation.
//
//	// Renew a lease
//	renewed, err := client.RenewLease(ctx, &dynamicsecrets.RenewLeaseRequest{
//	    LeaseID:   cred.LeaseID,
//	    Increment: 30 * time.Minute,
//	})
//
//	// Revoke a lease when done
//	_, err = client.RevokeLease(ctx, &dynamicsecrets.RevokeLeaseRequest{
//	    LeaseID: cred.LeaseID,
//	})
//
// # Listing Leases
//
// List active leases with optional prefix filtering.
//
//	resp, err := client.ListLeases(ctx, &dynamicsecrets.ListLeasesRequest{
//	    Prefix: "database/",
//	})
//	for _, lease := range resp.Leases {
//	    fmt.Printf("Lease %s expires at %s\n", lease.LeaseID, lease.ExpiresAt)
//	}
package dynamicsecrets
