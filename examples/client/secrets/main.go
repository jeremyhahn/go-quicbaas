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

// Example: Secrets Management
//
// This example demonstrates:
// - Connecting to QuicBaaS using the SDK with multiprotocol support
// - Storing secrets with versioning
// - Retrieving secrets (latest or specific version)
// - Listing secrets by prefix
// - Deleting secrets
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
)

func main() {
	// Parse command-line flags
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	flag.Parse()

	fmt.Println("=== QuicBaaS Secrets Example ===")
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

	// Step 1: Store a database credential secret
	fmt.Println("Step 1: Storing database credentials...")
	dbSecretPath := "secrets/database/production/postgres"
	storeResp, err := client.Security().Secrets().Store(ctx, &quicbaas.StoreSecretRequest{
		Path: dbSecretPath,
		Data: map[string]interface{}{
			"username": "dbadmin",
			"password": "super-secret-password-v1",
			"host":     "postgres.internal:5432",
			"database": "production_db",
		},
		Metadata: map[string]string{
			"environment": "production",
			"team":        "platform",
			"rotation":    "quarterly",
		},
	})
	if err != nil {
		log.Fatalf("Failed to store database secret: %v", err)
	}
	fmt.Printf("  Path:    %s\n", dbSecretPath)
	fmt.Printf("  Version: %d\n", storeResp.Version)

	// Step 2: Store an API key secret
	fmt.Println("\nStep 2: Storing API key secret...")
	apiSecretPath := "secrets/api/external/payment-gateway"
	storeResp, err = client.Security().Secrets().Store(ctx, &quicbaas.StoreSecretRequest{
		Path: apiSecretPath,
		Data: map[string]interface{}{
			"api_key":        "pk_live_abc123xyz789",
			"api_secret":     "sk_live_secret456def",
			"webhook_secret": "whsec_abc123",
		},
		Metadata: map[string]string{
			"provider": "stripe",
			"tier":     "enterprise",
		},
	})
	if err != nil {
		log.Fatalf("Failed to store API secret: %v", err)
	}
	fmt.Printf("  Path:    %s\n", apiSecretPath)
	fmt.Printf("  Version: %d\n", storeResp.Version)

	// Step 3: Store a TLS certificate secret
	fmt.Println("\nStep 3: Storing TLS certificate...")
	certSecretPath := "secrets/certificates/wildcard-example-com"
	storeResp, err = client.Security().Secrets().Store(ctx, &quicbaas.StoreSecretRequest{
		Path: certSecretPath,
		Data: map[string]interface{}{
			"certificate": "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----",
			"private_key": "-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----",
			"chain":       "-----BEGIN CERTIFICATE-----\nMIID...\n-----END CERTIFICATE-----",
		},
		Metadata: map[string]string{
			"domain":     "*.example.com",
			"issuer":     "lets-encrypt",
			"expires_at": "2026-01-01T00:00:00Z",
		},
	})
	if err != nil {
		log.Fatalf("Failed to store certificate secret: %v", err)
	}
	fmt.Printf("  Path:    %s\n", certSecretPath)
	fmt.Printf("  Version: %d\n", storeResp.Version)

	// Step 4: Update the database secret (creates new version)
	fmt.Println("\nStep 4: Rotating database password (new version)...")
	storeResp, err = client.Security().Secrets().Store(ctx, &quicbaas.StoreSecretRequest{
		Path: dbSecretPath,
		Data: map[string]interface{}{
			"username": "dbadmin",
			"password": "super-secret-password-v2-rotated",
			"host":     "postgres.internal:5432",
			"database": "production_db",
		},
		Metadata: map[string]string{
			"environment": "production",
			"team":        "platform",
			"rotation":    "quarterly",
			"rotated_at":  time.Now().Format(time.RFC3339),
		},
	})
	if err != nil {
		log.Fatalf("Failed to update database secret: %v", err)
	}
	fmt.Printf("  Path:        %s\n", dbSecretPath)
	fmt.Printf("  New Version: %d\n", storeResp.Version)

	// Step 5: Retrieve the latest database secret
	fmt.Println("\nStep 5: Retrieving latest database secret...")
	secret, err := client.Security().Secrets().Get(ctx, dbSecretPath)
	if err != nil {
		log.Fatalf("Failed to get database secret: %v", err)
	}
	fmt.Printf("  Path:     %s\n", secret.Path)
	fmt.Printf("  Version:  %d\n", secret.Version)
	fmt.Printf("  Created:  %s\n", secret.CreatedAt.Format(time.RFC3339))
	fmt.Printf("  Data Keys: ")
	for key := range secret.Data {
		fmt.Printf("%s ", key)
	}
	fmt.Println()
	fmt.Printf("  Metadata:\n")
	for key, value := range secret.Metadata {
		fmt.Printf("    %s: %s\n", key, value)
	}

	// Step 6: Retrieve a specific version
	fmt.Println("\nStep 6: Retrieving specific version (v1)...")
	secretV1, err := client.Security().Secrets().Get(ctx, dbSecretPath, quicbaas.WithSecretVersion(1))
	if err != nil {
		log.Printf("  Warning: Failed to get version 1: %v (version may not be supported)", err)
	} else {
		fmt.Printf("  Path:    %s\n", secretV1.Path)
		fmt.Printf("  Version: %d\n", secretV1.Version)
		if pw, ok := secretV1.Data["password"].(string); ok {
			fmt.Printf("  Password: %s... (truncated)\n", pw[:20])
		}
	}

	// Step 7: List all secrets under a prefix
	fmt.Println("\nStep 7: Listing secrets under 'secrets/'...")
	listResp, err := client.Security().Secrets().List(ctx, "secrets/")
	if err != nil {
		log.Fatalf("Failed to list secrets: %v", err)
	}
	fmt.Printf("  Found %d secrets:\n", len(listResp.Paths))
	for _, path := range listResp.Paths {
		fmt.Printf("    - %s\n", path)
	}

	// Step 8: List secrets under database prefix
	fmt.Println("\nStep 8: Listing database secrets...")
	listResp, err = client.Security().Secrets().List(ctx, "secrets/database/")
	if err != nil {
		log.Fatalf("Failed to list database secrets: %v", err)
	}
	fmt.Printf("  Found %d database secrets:\n", len(listResp.Paths))
	for _, path := range listResp.Paths {
		fmt.Printf("    - %s\n", path)
	}

	// Step 9: Delete secrets (cleanup)
	fmt.Println("\nStep 9: Cleaning up secrets...")
	secretsToDelete := []string{dbSecretPath, apiSecretPath, certSecretPath}
	for _, path := range secretsToDelete {
		if err := client.Security().Secrets().Delete(ctx, path); err != nil {
			log.Printf("  Warning: Failed to delete %s: %v", path, err)
		} else {
			fmt.Printf("  Deleted: %s\n", path)
		}
	}

	// Step 10: Verify deletion
	fmt.Println("\nStep 10: Verifying deletion...")
	listResp, err = client.Security().Secrets().List(ctx, "secrets/")
	if err != nil {
		log.Fatalf("Failed to list secrets: %v", err)
	}
	if len(listResp.Paths) == 0 {
		fmt.Println("  All secrets successfully deleted")
	} else {
		fmt.Printf("  Remaining secrets: %d\n", len(listResp.Paths))
	}

	fmt.Println("\n=== Example Complete ===")
	fmt.Println("\nKey Takeaways:")
	fmt.Println("  1. Secrets are stored at hierarchical paths (like a filesystem)")
	fmt.Println("  2. Each update creates a new version (automatic versioning)")
	fmt.Println("  3. Specific versions can be retrieved for rollback scenarios")
	fmt.Println("  4. Metadata provides additional context without affecting the secret")
	fmt.Println("  5. Prefix-based listing enables organized secret management")
	fmt.Println("  6. The SDK supports multiple transports (gRPC, REST, QUIC)")
}
