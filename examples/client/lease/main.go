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

// Example: Lease Management
//
// This example demonstrates comprehensive lease management in QuicBaaS:
//   - Creating leases with configurable TTL
//   - Renewing leases to extend their lifetime
//   - Revoking leases to immediately invalidate them
//   - Handling lease expiration gracefully
//   - Attaching leases to keys/resources
//   - Lease callbacks and notifications for lifecycle events
//
// Leases provide time-limited access to resources, enabling automatic cleanup
// of ephemeral data, session management, and distributed locking patterns.
//
// Prerequisites:
//   - A running QuicBaaS server (default: localhost:50051)
//   - Server must be initialized and unsealed
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
	"sync"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// LeaseState represents the current state of a lease.
type LeaseState string

const (
	// LeaseStateActive indicates the lease is currently active and valid.
	LeaseStateActive LeaseState = "active"

	// LeaseStateExpired indicates the lease has exceeded its TTL.
	LeaseStateExpired LeaseState = "expired"

	// LeaseStateRevoked indicates the lease was manually revoked.
	LeaseStateRevoked LeaseState = "revoked"
)

// LeaseInfo represents lease information returned by the API.
type LeaseInfo struct {
	LeaseID     string
	Path        string
	TTL         time.Duration
	ExpiresAt   time.Time
	CreatedAt   time.Time
	RenewCount  int
	MaxRenewals int
	Renewable   bool
	State       LeaseState
	Metadata    map[string]string
}

// LeaseEvent represents a lease lifecycle event for callbacks.
type LeaseEvent struct {
	Type    LeaseEventType
	LeaseID string
	Path    string
	Time    time.Time
}

// LeaseEventType indicates the type of lease event.
type LeaseEventType string

const (
	// LeaseEventCreated indicates a new lease was created.
	LeaseEventCreated LeaseEventType = "created"

	// LeaseEventRenewed indicates a lease was renewed.
	LeaseEventRenewed LeaseEventType = "renewed"

	// LeaseEventExpiring indicates a lease is about to expire.
	LeaseEventExpiring LeaseEventType = "expiring"

	// LeaseEventExpired indicates a lease has expired.
	LeaseEventExpired LeaseEventType = "expired"

	// LeaseEventRevoked indicates a lease was revoked.
	LeaseEventRevoked LeaseEventType = "revoked"
)

func main() {
	// Parse command-line flags
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	flag.Parse()

	fmt.Println("=== QuicBaaS Lease Management Example ===")
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

	// Demonstrate all lease management features
	demonstrateLeaseCreation(ctx, client)
	demonstrateLeaseRenewal(ctx, client)
	demonstrateLeaseRevocation(ctx, client)
	demonstrateLeaseExpiration(ctx, client)
	demonstrateLeaseAttachment(ctx, client)
	demonstrateLeaseCallbacks(ctx, client)
	demonstrateBulkOperations(ctx, client)

	fmt.Println("\n=== Example Complete ===")
	printKeyTakeaways()
}

// demonstrateLeaseCreation shows how to create leases with various configurations.
func demonstrateLeaseCreation(ctx context.Context, client quicbaas.Client) {
	fmt.Println("\n--- Step 1: Creating Leases with TTL ---")
	fmt.Println()

	// Example 1: Create a short-lived lease for a session token
	fmt.Println("Creating short-lived session lease (30 seconds TTL)...")
	sessionLease := createMockLease(
		"lease_session_001",
		"sessions/user/alice",
		30*time.Second,
		true, // renewable
		5,    // max renewals
		map[string]string{
			"user_id":    "alice",
			"session_id": "sess_abc123",
			"type":       "web_session",
		},
	)
	printLeaseInfo(sessionLease)

	// Example 2: Create a longer lease for database credentials
	fmt.Println("\nCreating database credential lease (1 hour TTL)...")
	dbLease := createMockLease(
		"lease_db_001",
		"secrets/database/production/postgres",
		1*time.Hour,
		true, // renewable
		0,    // unlimited renewals
		map[string]string{
			"database": "postgres",
			"role":     "readonly",
			"app":      "api-server",
		},
	)
	printLeaseInfo(dbLease)

	// Example 3: Create a non-renewable lease for one-time access
	fmt.Println("\nCreating non-renewable one-time access lease (5 minutes TTL)...")
	onetimeLease := createMockLease(
		"lease_onetime_001",
		"tokens/download/report_q4_2024",
		5*time.Minute,
		false, // not renewable
		0,     // max renewals (ignored since not renewable)
		map[string]string{
			"resource_type": "download_token",
			"file_id":       "report_q4_2024.pdf",
			"requester":     "finance@example.com",
		},
	)
	printLeaseInfo(onetimeLease)

	fmt.Println("\n  [Lease Creation Best Practices]")
	fmt.Println("  - Use short TTLs for sensitive resources (sessions, tokens)")
	fmt.Println("  - Enable renewable leases for long-running processes")
	fmt.Println("  - Set max_renewals to limit total lease lifetime")
	fmt.Println("  - Use non-renewable leases for one-time access tokens")
}

// demonstrateLeaseRenewal shows how to renew leases before expiration.
func demonstrateLeaseRenewal(ctx context.Context, client quicbaas.Client) {
	fmt.Println("\n--- Step 2: Renewing Leases ---")
	fmt.Println()

	// Create a lease to demonstrate renewal
	lease := createMockLease(
		"lease_renew_001",
		"secrets/api/external/payment-gateway",
		30*time.Second,
		true,
		3, // max 3 renewals
		map[string]string{
			"service": "payment-gateway",
		},
	)

	fmt.Println("Initial lease state:")
	printLeaseInfo(lease)

	// Simulate renewal operations
	fmt.Println("\nRenewing lease (renewal 1 of 3)...")
	lease.RenewCount = 1
	lease.ExpiresAt = time.Now().Add(30 * time.Second)
	fmt.Printf("  Renewed! New expiration: %s\n", lease.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("  Remaining renewals: %d\n", lease.MaxRenewals-lease.RenewCount)

	fmt.Println("\nRenewing lease (renewal 2 of 3)...")
	lease.RenewCount = 2
	lease.ExpiresAt = time.Now().Add(30 * time.Second)
	fmt.Printf("  Renewed! New expiration: %s\n", lease.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("  Remaining renewals: %d\n", lease.MaxRenewals-lease.RenewCount)

	fmt.Println("\nRenewing lease with extended TTL (renewal 3 of 3)...")
	lease.RenewCount = 3
	lease.ExpiresAt = time.Now().Add(60 * time.Second) // Extended TTL
	fmt.Printf("  Renewed with extended TTL! New expiration: %s\n", lease.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("  Remaining renewals: %d (no more renewals allowed)\n", lease.MaxRenewals-lease.RenewCount)

	fmt.Println("\nAttempting renewal beyond max renewals...")
	fmt.Println("  Error: lease cannot be renewed (max renewals exceeded)")

	fmt.Println("\n  [Lease Renewal Best Practices]")
	fmt.Println("  - Renew leases proactively before expiration")
	fmt.Println("  - Use a renewal interval of 50-75% of TTL")
	fmt.Println("  - Handle renewal failures gracefully")
	fmt.Println("  - Set appropriate max_renewals for security")
}

// demonstrateLeaseRevocation shows how to revoke leases.
func demonstrateLeaseRevocation(ctx context.Context, client quicbaas.Client) {
	fmt.Println("\n--- Step 3: Revoking Leases ---")
	fmt.Println()

	// Create leases to demonstrate revocation
	sessionLease := createMockLease(
		"lease_revoke_001",
		"sessions/user/bob",
		1*time.Hour,
		true,
		0,
		map[string]string{"user_id": "bob"},
	)

	secretLease := createMockLease(
		"lease_revoke_002",
		"secrets/api/compromised-key",
		24*time.Hour,
		true,
		0,
		map[string]string{"api_name": "legacy-service"},
	)

	fmt.Println("Active leases before revocation:")
	fmt.Printf("  - %s (path: %s)\n", sessionLease.LeaseID, sessionLease.Path)
	fmt.Printf("  - %s (path: %s)\n", secretLease.LeaseID, secretLease.Path)

	// Single lease revocation
	fmt.Println("\nRevoking single lease (user logged out)...")
	sessionLease.State = LeaseStateRevoked
	fmt.Printf("  Revoked lease: %s\n", sessionLease.LeaseID)
	fmt.Printf("  Associated resources are now invalid\n")

	// Revoke by prefix (security incident response)
	fmt.Println("\nRevoking all leases matching prefix 'secrets/api/' (security incident)...")
	fmt.Printf("  Revoked 1 lease(s) matching prefix\n")
	secretLease.State = LeaseStateRevoked
	fmt.Printf("  - Revoked: %s\n", secretLease.LeaseID)

	fmt.Println("\n  [Lease Revocation Use Cases]")
	fmt.Println("  - User logout or session termination")
	fmt.Println("  - Security incident response")
	fmt.Println("  - Credential rotation")
	fmt.Println("  - Application shutdown")
	fmt.Println("  - Resource cleanup")
}

// demonstrateLeaseExpiration shows lease expiration handling.
func demonstrateLeaseExpiration(ctx context.Context, client quicbaas.Client) {
	fmt.Println("\n--- Step 4: Lease Expiration Handling ---")
	fmt.Println()

	// Create a short-lived lease for demonstration
	shortLease := createMockLease(
		"lease_expire_001",
		"temp/processing/job_123",
		5*time.Second,
		false,
		0,
		map[string]string{"job_id": "job_123"},
	)

	fmt.Println("Created short-lived lease (5 second TTL):")
	printLeaseInfo(shortLease)

	// Simulate time passing
	fmt.Println("\nMonitoring lease expiration...")
	fmt.Printf("  T+0s: Lease state: %s, TTL remaining: 5s\n", shortLease.State)
	fmt.Printf("  T+2s: Lease state: %s, TTL remaining: 3s\n", shortLease.State)
	fmt.Printf("  T+4s: Lease state: %s, TTL remaining: 1s (WARNING: expiring soon!)\n", shortLease.State)

	// Lease expires
	shortLease.State = LeaseStateExpired
	fmt.Printf("  T+5s: Lease state: %s, TTL remaining: 0s\n", shortLease.State)

	fmt.Println("\nLease expired! Associated resources:")
	fmt.Println("  - Temporary data automatically cleaned up")
	fmt.Println("  - Access tokens invalidated")
	fmt.Println("  - Callbacks triggered for dependent services")

	fmt.Println("\n  [Lease Expiration Handling Patterns]")
	fmt.Println("  - Use lease watchers for proactive renewal")
	fmt.Println("  - Implement graceful degradation on expiration")
	fmt.Println("  - Clean up local state when leases expire")
	fmt.Println("  - Log expiration events for debugging")
}

// demonstrateLeaseAttachment shows attaching leases to keys/resources.
func demonstrateLeaseAttachment(ctx context.Context, client quicbaas.Client) {
	fmt.Println("\n--- Step 5: Attaching Leases to Keys/Resources ---")
	fmt.Println()

	// Create a lease for ephemeral data
	ephemeralLease := createMockLease(
		"lease_attach_001",
		"ephemeral/locks/distributed_job",
		30*time.Second,
		true,
		10,
		map[string]string{"job_type": "batch_processing"},
	)

	fmt.Println("Step 5a: Creating lease for ephemeral key-value data...")
	printLeaseInfo(ephemeralLease)

	fmt.Println("\nStep 5b: Storing key-value data with lease attachment...")
	fmt.Println("  Storing key: ephemeral/locks/distributed_job")
	fmt.Println("  Value: {\"worker_id\": \"worker-1\", \"status\": \"processing\"}")
	fmt.Printf("  Attached lease: %s (TTL: %v)\n", ephemeralLease.LeaseID, ephemeralLease.TTL)

	fmt.Println("\nStep 5c: Storing multiple keys with same lease...")
	attachedKeys := []string{
		"ephemeral/locks/distributed_job/progress",
		"ephemeral/locks/distributed_job/checkpoint",
		"ephemeral/locks/distributed_job/metrics",
	}
	for _, key := range attachedKeys {
		fmt.Printf("  - %s (lease: %s)\n", key, ephemeralLease.LeaseID)
	}

	fmt.Println("\nStep 5d: Lease expiration cleanup behavior...")
	fmt.Println("  When lease expires, ALL attached keys are automatically deleted:")
	for _, key := range attachedKeys {
		fmt.Printf("  - Deleted: %s\n", key)
	}

	fmt.Println("\n  [Lease Attachment Use Cases]")
	fmt.Println("  - Distributed locks with automatic release")
	fmt.Println("  - Service discovery with TTL")
	fmt.Println("  - Session data that expires with the session")
	fmt.Println("  - Temporary processing state")
	fmt.Println("  - Leader election with lease-based heartbeat")
}

// demonstrateLeaseCallbacks shows lease lifecycle callbacks.
func demonstrateLeaseCallbacks(ctx context.Context, client quicbaas.Client) {
	fmt.Println("\n--- Step 6: Lease Callbacks and Notifications ---")
	fmt.Println()

	// Create a channel to receive lease events
	eventChan := make(chan LeaseEvent, 10)
	var wg sync.WaitGroup

	// Start event listener
	wg.Add(1)
	go func() {
		defer wg.Done()
		fmt.Println("Lease event listener started...")
		for event := range eventChan {
			handleLeaseEvent(event)
		}
	}()

	// Create a lease and simulate lifecycle events
	callbackLease := createMockLease(
		"lease_callback_001",
		"secrets/dynamic/aws-credentials",
		30*time.Second,
		true,
		3,
		map[string]string{
			"cloud_provider": "aws",
			"role":           "sts-assume-role",
		},
	)

	fmt.Println("\nSimulating lease lifecycle events...")

	// Simulate lease creation
	eventChan <- LeaseEvent{
		Type:    LeaseEventCreated,
		LeaseID: callbackLease.LeaseID,
		Path:    callbackLease.Path,
		Time:    time.Now(),
	}

	// Simulate renewal
	time.Sleep(100 * time.Millisecond)
	eventChan <- LeaseEvent{
		Type:    LeaseEventRenewed,
		LeaseID: callbackLease.LeaseID,
		Path:    callbackLease.Path,
		Time:    time.Now(),
	}

	// Simulate expiring warning
	time.Sleep(100 * time.Millisecond)
	eventChan <- LeaseEvent{
		Type:    LeaseEventExpiring,
		LeaseID: callbackLease.LeaseID,
		Path:    callbackLease.Path,
		Time:    time.Now(),
	}

	// Simulate revocation
	time.Sleep(100 * time.Millisecond)
	eventChan <- LeaseEvent{
		Type:    LeaseEventRevoked,
		LeaseID: callbackLease.LeaseID,
		Path:    callbackLease.Path,
		Time:    time.Now(),
	}

	// Close event channel and wait for listener
	time.Sleep(100 * time.Millisecond)
	close(eventChan)
	wg.Wait()

	fmt.Println("\n  [Lease Callback Patterns]")
	fmt.Println("  - LeaseEventCreated: Initialize dependent resources")
	fmt.Println("  - LeaseEventRenewed: Log for audit trail")
	fmt.Println("  - LeaseEventExpiring: Trigger proactive renewal or cleanup")
	fmt.Println("  - LeaseEventExpired: Clean up local state, notify dependent services")
	fmt.Println("  - LeaseEventRevoked: Immediate invalidation, security logging")
}

// demonstrateBulkOperations shows bulk lease operations.
func demonstrateBulkOperations(ctx context.Context, client quicbaas.Client) {
	fmt.Println("\n--- Step 7: Bulk Lease Operations ---")
	fmt.Println()

	// Create multiple leases for bulk operations
	leases := []*LeaseInfo{
		createMockLease("lease_bulk_001", "sessions/user/alice", 1*time.Hour, true, 5, nil),
		createMockLease("lease_bulk_002", "sessions/user/bob", 1*time.Hour, true, 5, nil),
		createMockLease("lease_bulk_003", "sessions/user/charlie", 1*time.Hour, true, 5, nil),
		createMockLease("lease_bulk_004", "secrets/api/service-a", 24*time.Hour, true, 0, nil),
		createMockLease("lease_bulk_005", "secrets/api/service-b", 24*time.Hour, true, 0, nil),
	}

	fmt.Println("Step 7a: Listing all leases...")
	fmt.Printf("  Found %d active leases:\n", len(leases))
	for _, l := range leases {
		fmt.Printf("    - %s: %s (TTL: %v)\n", l.LeaseID, l.Path, l.TTL)
	}

	fmt.Println("\nStep 7b: Listing leases by prefix 'sessions/'...")
	sessionCount := 0
	for _, l := range leases {
		if len(l.Path) >= 9 && l.Path[:9] == "sessions/" {
			fmt.Printf("    - %s: %s\n", l.LeaseID, l.Path)
			sessionCount++
		}
	}
	fmt.Printf("  Found %d session leases\n", sessionCount)

	fmt.Println("\nStep 7c: Bulk revocation by prefix 'sessions/'...")
	revokedCount := 0
	for _, l := range leases {
		if len(l.Path) >= 9 && l.Path[:9] == "sessions/" {
			l.State = LeaseStateRevoked
			fmt.Printf("    - Revoked: %s\n", l.LeaseID)
			revokedCount++
		}
	}
	fmt.Printf("  Revoked %d leases\n", revokedCount)

	fmt.Println("\nStep 7d: Listing remaining active leases...")
	activeCount := 0
	for _, l := range leases {
		if l.State == LeaseStateActive {
			fmt.Printf("    - %s: %s\n", l.LeaseID, l.Path)
			activeCount++
		}
	}
	fmt.Printf("  %d active leases remaining\n", activeCount)

	fmt.Println("\n  [Bulk Operation Use Cases]")
	fmt.Println("  - Application shutdown: Revoke all app's leases")
	fmt.Println("  - Security audit: List all active leases")
	fmt.Println("  - Tenant isolation: Revoke prefix 'tenant/{id}/'")
	fmt.Println("  - Maintenance: List leases expiring within time window")
}

// handleLeaseEvent processes lease lifecycle events.
func handleLeaseEvent(event LeaseEvent) {
	timestamp := event.Time.Format("15:04:05.000")
	switch event.Type {
	case LeaseEventCreated:
		fmt.Printf("  [%s] CREATED: Lease %s for path %s\n", timestamp, event.LeaseID, event.Path)
		fmt.Println("           -> Initialize dependent resources")

	case LeaseEventRenewed:
		fmt.Printf("  [%s] RENEWED: Lease %s extended\n", timestamp, event.LeaseID)
		fmt.Println("           -> Log renewal for audit trail")

	case LeaseEventExpiring:
		fmt.Printf("  [%s] EXPIRING: Lease %s will expire soon!\n", timestamp, event.LeaseID)
		fmt.Println("           -> Trigger proactive renewal or cleanup")

	case LeaseEventExpired:
		fmt.Printf("  [%s] EXPIRED: Lease %s has expired\n", timestamp, event.LeaseID)
		fmt.Println("           -> Clean up local state, notify dependents")

	case LeaseEventRevoked:
		fmt.Printf("  [%s] REVOKED: Lease %s was revoked\n", timestamp, event.LeaseID)
		fmt.Println("           -> Immediate invalidation, security logging")
	}
}

// createMockLease creates a mock lease for demonstration purposes.
func createMockLease(id, path string, ttl time.Duration, renewable bool, maxRenewals int, metadata map[string]string) *LeaseInfo {
	now := time.Now()
	return &LeaseInfo{
		LeaseID:     id,
		Path:        path,
		TTL:         ttl,
		ExpiresAt:   now.Add(ttl),
		CreatedAt:   now,
		RenewCount:  0,
		MaxRenewals: maxRenewals,
		Renewable:   renewable,
		State:       LeaseStateActive,
		Metadata:    metadata,
	}
}

// printLeaseInfo displays detailed lease information.
func printLeaseInfo(lease *LeaseInfo) {
	fmt.Printf("  Lease ID:     %s\n", lease.LeaseID)
	fmt.Printf("  Path:         %s\n", lease.Path)
	fmt.Printf("  State:        %s\n", lease.State)
	fmt.Printf("  TTL:          %v\n", lease.TTL)
	fmt.Printf("  Expires At:   %s\n", lease.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("  Created At:   %s\n", lease.CreatedAt.Format(time.RFC3339))
	fmt.Printf("  Renewable:    %v\n", lease.Renewable)
	if lease.Renewable {
		if lease.MaxRenewals == 0 {
			fmt.Printf("  Max Renewals: unlimited\n")
		} else {
			fmt.Printf("  Max Renewals: %d (used: %d)\n", lease.MaxRenewals, lease.RenewCount)
		}
	}
	if len(lease.Metadata) > 0 {
		fmt.Printf("  Metadata:\n")
		for k, v := range lease.Metadata {
			fmt.Printf("    %s: %s\n", k, v)
		}
	}
}

// printKeyTakeaways displays the summary of key concepts.
func printKeyTakeaways() {
	fmt.Println("\nKey Takeaways:")
	fmt.Println("  1. Leases provide time-limited access to resources with automatic expiration")
	fmt.Println("  2. Renewable leases can be extended before expiration (with optional limits)")
	fmt.Println("  3. Leases can be revoked immediately for security or cleanup purposes")
	fmt.Println("  4. Attaching leases to keys enables automatic cleanup of ephemeral data")
	fmt.Println("  5. Lease callbacks enable reactive patterns for lifecycle events")
	fmt.Println("  6. Bulk operations support efficient management at scale")
	fmt.Println("  7. The SDK supports multiple transports (gRPC, REST, QUIC)")

	fmt.Println("\nCommon Use Cases:")
	fmt.Println("  - Session management with automatic expiration")
	fmt.Println("  - Dynamic secret rotation with credential leases")
	fmt.Println("  - Distributed locking with automatic release on failure")
	fmt.Println("  - Service discovery with TTL-based health checks")
	fmt.Println("  - Temporary access tokens for downloads or API calls")
	fmt.Println("  - Leader election with lease-based heartbeat")
}
