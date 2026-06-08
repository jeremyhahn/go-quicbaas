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

// Example: Seal/Unseal Operations
//
// This example demonstrates comprehensive seal and unseal operations:
// - Checking seal status (initialization state, seal state)
// - Initializing a QuicBaaS server (generating unseal keys and root token)
// - Sealing the server (protecting data at rest)
// - Unsealing the server (using Shamir's Secret Sharing)
// - Resetting the unseal progress
// - Complete seal/unseal workflow
//
// Security Concepts:
//
// QuicBaaS uses Shamir's Secret Sharing to protect the master encryption key.
// During initialization, the master key is split into N shares, where any T
// shares (threshold) can reconstruct the key. This provides:
//
//   - Defense in Depth: No single key can unlock the database
//   - Operational Security: Keys can be distributed to multiple operators
//   - Disaster Recovery: Losing some keys does not prevent access
//
// Typical configurations:
//   - Development: 1 share, threshold 1 (single key)
//   - Production: 5 shares, threshold 3 (any 3 of 5 operators can unseal)
//   - High Security: 7 shares, threshold 4 (requires 4 operators)
//
// Prerequisites:
// - A running QuicBaaS server (default: localhost:50051)
// - For fresh servers: server must NOT be initialized yet
// - For existing servers: have unseal keys available
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
//
//	# Custom Shamir parameters (5 shares, require 3 to unseal)
//	go run main.go -shares 5 -threshold 3
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
	// Parse command-line flags
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	shares := flag.Int("shares", 3, "Number of unseal key shares to generate")
	threshold := flag.Int("threshold", 2, "Number of shares required to unseal")
	skipInit := flag.Bool("skip-init", false, "Skip initialization (for already initialized servers)")
	flag.Parse()

	fmt.Println("=== QuicBaaS Seal/Unseal Example ===")
	fmt.Printf("Transport: %s\n", *transportType)
	fmt.Printf("Address:   %s\n", *address)
	fmt.Printf("Shares:    %d\n", *shares)
	fmt.Printf("Threshold: %d\n", *threshold)
	fmt.Println()

	// Validate Shamir parameters
	if *threshold > *shares {
		log.Fatalf("Error: threshold (%d) cannot exceed shares (%d)", *threshold, *shares)
	}
	if *threshold < 1 {
		log.Fatalf("Error: threshold must be at least 1")
	}
	if *shares < 1 {
		log.Fatalf("Error: shares must be at least 1")
	}

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

	// Step 1: Check initial seal status
	fmt.Println("Step 1: Checking initial seal status...")
	status, err := checkSealStatus(ctx, client)
	if err != nil {
		log.Fatalf("Failed to get seal status: %v", err)
	}
	fmt.Println()

	// Store unseal keys for later use
	var unsealKeys []string
	var rootToken string

	// Step 2: Initialize the server if not already initialized
	if !status.Initialized && !*skipInit {
		fmt.Println("Step 2: Initializing server with Shamir's Secret Sharing...")
		unsealKeys, rootToken, err = initializeServer(ctx, client, *shares, *threshold)
		if err != nil {
			log.Fatalf("Failed to initialize server: %v", err)
		}
		fmt.Println()

		// Important: Display keys for operator to save
		printSecurityWarning(unsealKeys, rootToken)
	} else if status.Initialized {
		fmt.Println("Step 2: Server already initialized, skipping initialization")
		fmt.Println("  Note: You will need existing unseal keys to proceed")
		fmt.Println()
	} else {
		fmt.Println("Step 2: Skipping initialization as requested")
		fmt.Println()
	}

	// Step 3: Check status after initialization
	fmt.Println("Step 3: Checking status after initialization...")
	_, err = checkSealStatus(ctx, client)
	if err != nil {
		log.Fatalf("Failed to get seal status: %v", err)
	}
	fmt.Println()

	// Step 4: Demonstrate unsealing (if we have keys)
	if len(unsealKeys) > 0 {
		fmt.Println("Step 4: Unsealing server with generated keys...")
		if err := unsealServer(ctx, client, unsealKeys, *threshold); err != nil {
			log.Fatalf("Failed to unseal server: %v", err)
		}
		fmt.Println()

		// Step 5: Verify unsealed status
		fmt.Println("Step 5: Verifying unsealed status...")
		status, err = checkSealStatus(ctx, client)
		if err != nil {
			log.Fatalf("Failed to get seal status: %v", err)
		}
		if status.Sealed {
			log.Println("  Warning: Server still reports as sealed")
		} else {
			fmt.Println("  Server is now unsealed and operational")
		}
		fmt.Println()

		// Step 6: Demonstrate sealing
		fmt.Println("Step 6: Demonstrating seal operation...")
		if err := sealServer(ctx, client); err != nil {
			log.Fatalf("Failed to seal server: %v", err)
		}
		fmt.Println()

		// Step 7: Verify sealed status
		fmt.Println("Step 7: Verifying sealed status...")
		status, err = checkSealStatus(ctx, client)
		if err != nil {
			log.Fatalf("Failed to get seal status: %v", err)
		}
		if !status.Sealed {
			log.Println("  Warning: Server should be sealed but reports as unsealed")
		} else {
			fmt.Println("  Server is now sealed")
		}
		fmt.Println()

		// Step 8: Demonstrate partial unseal and reset
		fmt.Println("Step 8: Demonstrating partial unseal and reset...")
		if err := demonstratePartialUnsealAndReset(ctx, client, unsealKeys); err != nil {
			log.Printf("Warning: Partial unseal demonstration failed: %v", err)
		}
		fmt.Println()

		// Step 9: Final unseal to leave server operational
		fmt.Println("Step 9: Final unseal to leave server operational...")
		if err := unsealServer(ctx, client, unsealKeys, *threshold); err != nil {
			log.Fatalf("Failed to unseal server: %v", err)
		}
		fmt.Println()

		// Step 10: Final status check
		fmt.Println("Step 10: Final status check...")
		_, err = checkSealStatus(ctx, client)
		if err != nil {
			log.Fatalf("Failed to get seal status: %v", err)
		}
		fmt.Println()
	} else {
		fmt.Println("Step 4-10: Skipped (no unseal keys available)")
		fmt.Println("  To complete the demonstration, provide unseal keys or run on a fresh server")
		fmt.Println()
	}

	// Print summary
	printSummary(rootToken)
}

// checkSealStatus retrieves and displays the current seal status.
func checkSealStatus(ctx context.Context, client quicbaas.Client) (*quicbaas.SealStatus, error) {
	status, err := client.Security().Seal().Status(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Printf("  Initialized:     %t\n", status.Initialized)
	fmt.Printf("  Sealed:          %t\n", status.Sealed)
	fmt.Printf("  Progress:        %d/%d shares provided\n", status.Progress, status.Required)
	if status.ClusterName != "" {
		fmt.Printf("  Cluster Name:    %s\n", status.ClusterName)
	}
	if status.ClusterVersion != "" {
		fmt.Printf("  Cluster Version: %s\n", status.ClusterVersion)
	}

	return status, nil
}

// initializeServer initializes the seal with Shamir's Secret Sharing.
func initializeServer(ctx context.Context, client quicbaas.Client, shares, threshold int) ([]string, string, error) {
	fmt.Printf("  Generating %d unseal key shares (threshold: %d)...\n", shares, threshold)

	resp, err := client.Security().Seal().Init(ctx, &quicbaas.InitRequest{
		SecretShares:    shares,
		SecretThreshold: threshold,
	})
	if err != nil {
		return nil, "", err
	}

	fmt.Printf("  Generated %d unseal keys\n", len(resp.Keys))
	fmt.Printf("  Root token generated: %s...\n", truncateToken(resp.RootToken, 16))

	return resp.Keys, resp.RootToken, nil
}

// unsealServer provides unseal keys to unseal the server.
func unsealServer(ctx context.Context, client quicbaas.Client, keys []string, threshold int) error {
	fmt.Printf("  Providing %d unseal keys (threshold: %d)...\n", threshold, threshold)

	for i := 0; i < threshold && i < len(keys); i++ {
		resp, err := client.Security().Seal().Unseal(ctx, &quicbaas.UnsealRequest{
			Key:   keys[i],
			Reset: false,
		})
		if err != nil {
			return &qberrors.WrappedMessageError{Message: "failed to provide key " + strconv.Itoa(i+1), Cause: err}
		}

		fmt.Printf("  Key %d: progress %d/%d", i+1, resp.Progress, resp.Required)
		if !resp.Sealed {
			fmt.Printf(" - UNSEALED")
		}
		fmt.Println()

		if !resp.Sealed {
			break
		}
	}

	return nil
}

// sealServer seals the server, protecting data at rest.
func sealServer(ctx context.Context, client quicbaas.Client) error {
	fmt.Println("  Sending seal command...")

	if err := client.Security().Seal().Seal(ctx); err != nil {
		return err
	}

	fmt.Println("  Server sealed successfully")
	return nil
}

// demonstratePartialUnsealAndReset shows the unseal reset functionality.
func demonstratePartialUnsealAndReset(ctx context.Context, client quicbaas.Client, keys []string) error {
	if len(keys) < 1 {
		return &qberrors.MessageError{Message: "need at least 1 key to demonstrate"}
	}

	// Provide one key
	fmt.Println("  Providing first unseal key...")
	resp, err := client.Security().Seal().Unseal(ctx, &quicbaas.UnsealRequest{
		Key:   keys[0],
		Reset: false,
	})
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to provide key", Cause: err}
	}
	fmt.Printf("  Progress after first key: %d/%d\n", resp.Progress, resp.Required)

	// Now reset the progress
	fmt.Println("  Resetting unseal progress...")
	resp, err = client.Security().Seal().Unseal(ctx, &quicbaas.UnsealRequest{
		Key:   "",
		Reset: true,
	})
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to reset", Cause: err}
	}
	fmt.Printf("  Progress after reset: %d/%d\n", resp.Progress, resp.Required)

	return nil
}

// printSecurityWarning displays important security information about the generated keys.
func printSecurityWarning(keys []string, rootToken string) {
	fmt.Println()
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("IMPORTANT: SAVE THESE VALUES SECURELY")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println()
	fmt.Println("The following unseal keys and root token have been generated.")
	fmt.Println("These values are shown ONLY ONCE and cannot be retrieved later.")
	fmt.Println()
	fmt.Println("Store each key in a separate, secure location:")
	fmt.Println("  - Hardware security modules (HSM)")
	fmt.Println("  - Password managers")
	fmt.Println("  - Offline secure storage")
	fmt.Println("  - Distribute to different trusted operators")
	fmt.Println()
	fmt.Println("UNSEAL KEYS:")
	for i, key := range keys {
		fmt.Printf("  Key %d: %s\n", i+1, key)
	}
	fmt.Println()
	fmt.Printf("ROOT TOKEN: %s\n", rootToken)
	fmt.Println()
	fmt.Println("The root token provides full administrative access.")
	fmt.Println("Consider generating limited tokens for day-to-day operations.")
	fmt.Println()
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println()

	// Give user time to read and save
	fmt.Print("Press Enter after saving these values to continue...")
	fmt.Scanln()
}

// printSummary displays a summary of the seal/unseal operations.
func printSummary(rootToken string) {
	fmt.Println("=== Example Complete ===")
	fmt.Println()
	fmt.Println("Key Takeaways:")
	fmt.Println()
	fmt.Println("  1. INITIALIZATION")
	fmt.Println("     - Generates master encryption key")
	fmt.Println("     - Splits key using Shamir's Secret Sharing")
	fmt.Println("     - Creates root token for admin access")
	fmt.Println("     - Can only be done once per cluster")
	fmt.Println()
	fmt.Println("  2. SEAL STATUS")
	fmt.Println("     - Initialized: whether Init() has been called")
	fmt.Println("     - Sealed: whether data is currently protected")
	fmt.Println("     - Progress: unseal keys provided vs required")
	fmt.Println()
	fmt.Println("  3. UNSEALING")
	fmt.Println("     - Requires threshold number of keys")
	fmt.Println("     - Keys can be provided incrementally")
	fmt.Println("     - Progress can be reset if needed")
	fmt.Println("     - Once unsealed, server is operational")
	fmt.Println()
	fmt.Println("  4. SEALING")
	fmt.Println("     - Protects data at rest")
	fmt.Println("     - Requires unseal keys to access again")
	fmt.Println("     - Use for maintenance or security response")
	fmt.Println()
	fmt.Println("  5. BEST PRACTICES")
	fmt.Println("     - Never store all unseal keys together")
	fmt.Println("     - Distribute keys to different operators")
	fmt.Println("     - Use key rotation periodically")
	fmt.Println("     - Monitor seal status in production")
	fmt.Println("     - Have recovery procedures documented")
	fmt.Println()

	if rootToken != "" {
		fmt.Println("  NEXT STEPS:")
		fmt.Println("     - Use root token to create limited access tokens")
		fmt.Println("     - Configure authentication methods (OIDC, LDAP)")
		fmt.Println("     - Set up audit logging")
		fmt.Println("     - Create tenant and IAM policies")
		fmt.Println()
	}

	fmt.Println("  TRANSPORT OPTIONS:")
	fmt.Println("     - gRPC (default): High performance, bidirectional streaming")
	fmt.Println("     - REST: HTTP/JSON for broad compatibility")
	fmt.Println("     - QUIC: Low latency over UDP, connection migration")
	fmt.Println()
}

// truncateToken truncates a token for display purposes.
func truncateToken(token string, length int) string {
	if len(token) <= length {
		return token
	}
	return token[:length]
}

// environmentExample demonstrates using environment variables for automation.
func environmentExample() {
	// Example of how operators might automate unsealing
	// This is for documentation purposes only

	fmt.Println("Environment Variable Example:")
	fmt.Println()
	fmt.Println("For automated unsealing in production, operators might use:")
	fmt.Println()
	fmt.Println("  export QUICBAAS_UNSEAL_KEY_1='...'")
	fmt.Println("  export QUICBAAS_UNSEAL_KEY_2='...'")
	fmt.Println("  export QUICBAAS_UNSEAL_KEY_3='...'")
	fmt.Println()
	fmt.Println("Then read keys in your automation:")
	fmt.Println()
	fmt.Println("  keys := []string{")
	fmt.Println("      os.Getenv(\"QUICBAAS_UNSEAL_KEY_1\"),")
	fmt.Println("      os.Getenv(\"QUICBAAS_UNSEAL_KEY_2\"),")
	fmt.Println("      os.Getenv(\"QUICBAAS_UNSEAL_KEY_3\"),")
	fmt.Println("  }")
	fmt.Println()

	// This shows how to read keys from environment (for reference)
	_ = os.Getenv("QUICBAAS_UNSEAL_KEY_1")
	_ = os.Getenv("QUICBAAS_UNSEAL_KEY_2")
	_ = os.Getenv("QUICBAAS_UNSEAL_KEY_3")
}
