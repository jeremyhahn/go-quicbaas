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

// Command registry demonstrates QuicBaaS service discovery: registering a
// service instance under a lease, resolving instances by service name,
// watching for membership changes, and deregistering. The registry is built
// entirely on the QRDB lease + watch + KV primitives.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	service := flag.String("service", "payments", "Service name to register under")
	flag.Parse()

	fmt.Println("=== QuicBaaS Service Discovery Example ===")
	fmt.Printf("Transport: %s\nAddress:   %s\nService:   %s\n\n", *transportType, *address, *service)

	opts := []quicbaas.Option{quicbaas.WithTimeout(30 * time.Second)}
	switch *transportType {
	case "rest":
		opts = append(opts, quicbaas.WithREST())
	case "quic":
		opts = append(opts, quicbaas.WithQUIC())
	}

	client, err := quicbaas.NewClient(*address, opts...)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	reg, err := client.Discovery()
	if err != nil {
		log.Fatalf("Failed to obtain discovery registry: %v", err)
	}

	ctx := context.Background()

	// Watch for membership changes before registering.
	events, err := reg.Watch(ctx, *service)
	if err != nil {
		log.Fatalf("Watch failed: %v", err)
	}
	go func() {
		for ev := range events {
			switch ev.Type {
			case registry.Put:
				fmt.Printf("  [watch] instance up:   %s @ %s\n", ev.Instance.ID, ev.Instance.Address)
			case registry.Delete:
				fmt.Printf("  [watch] instance down: %s\n", ev.Instance.ID)
			}
		}
	}()

	// Register this instance under a 10s lease.
	inst := registry.ServiceInstance{
		ID:       "instance-1",
		Service:  *service,
		Address:  "10.0.0.5:8080",
		Metadata: map[string]string{"zone": "a"},
	}
	fmt.Println("Step 1: Registering instance...")
	leaseID, err := reg.Register(ctx, inst, 10*time.Second)
	if err != nil {
		log.Fatalf("Register failed: %v", err)
	}
	fmt.Printf("  registered %s under lease %d\n\n", inst.ID, leaseID)

	// Resolve the service.
	fmt.Println("Step 2: Resolving service...")
	instances, err := reg.Resolve(ctx, *service)
	if err != nil {
		log.Fatalf("Resolve failed: %v", err)
	}
	for _, i := range instances {
		fmt.Printf("  resolved %s @ %s %v\n", i.ID, i.Address, i.Metadata)
	}
	fmt.Println()

	// Heartbeat once, then deregister.
	fmt.Println("Step 3: Renewing lease (heartbeat)...")
	if err := reg.Renew(ctx, leaseID); err != nil {
		log.Fatalf("Renew failed: %v", err)
	}

	fmt.Println("Step 4: Deregistering instance...")
	if err := reg.Deregister(ctx, leaseID); err != nil {
		log.Fatalf("Deregister failed: %v", err)
	}

	// Give the watch goroutine a moment to observe the delete.
	time.Sleep(500 * time.Millisecond)
	fmt.Println("\nDone.")
}
