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

// Command registry demonstrates the QuicBaaS service registry: registering a
// service instance under a lease, resolving the service, watching it from the
// resolved revision, renewing the lease and deregistering. The registry is
// served on every transport and scoped to the caller's tenant.
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
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	service := flag.String("service", "payments", "Service name to register under")
	flag.Parse()

	fmt.Println("=== QuicBaaS Service Registry Example ===")
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
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	reg := client.Registry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Println("Step 1: Resolving the service and watching it from that revision...")
	snapshot, err := reg.Resolve(ctx, *service)
	if err != nil {
		log.Fatalf("Resolve failed: %v", err)
	}
	fmt.Printf("  %d instance(s) at revision %d\n\n", len(snapshot.Instances), snapshot.Revision)
	events, err := reg.Watch(ctx, &quicbaas.RegistryWatchRequest{Service: *service, FromRevision: snapshot.Revision + 1})
	if err != nil {
		log.Fatalf("Watch failed: %v", err)
	}

	fmt.Println("Step 2: Registering an instance under a 10s lease...")
	lease, err := reg.Register(ctx, &quicbaas.RegistryRegisterRequest{
		Instance: quicbaas.RegistryServiceInstance{
			ID:       "instance-1",
			Service:  *service,
			Address:  "10.0.0.5:8080",
			Metadata: map[string]string{"zone": "a"},
		},
		TTLSeconds: 10,
	})
	if err != nil {
		log.Fatalf("Register failed: %v", err)
	}
	fmt.Printf("  registered under lease %d\n", lease.LeaseID)
	printEvent(<-events)

	fmt.Println("Step 3: Renewing the lease (heartbeat)...")
	renewed, err := reg.Renew(ctx, lease.LeaseID)
	if err != nil {
		log.Fatalf("Renew failed: %v", err)
	}
	fmt.Printf("  lease reset to %ds\n\n", renewed.TTLSeconds)

	fmt.Println("Step 4: Deregistering...")
	if err := reg.Deregister(ctx, lease.LeaseID); err != nil {
		log.Fatalf("Deregister failed: %v", err)
	}
	printEvent(<-events)
	fmt.Println("\nDone.")
}

// printEvent reports one watch event.
func printEvent(ev quicbaas.RegistryEvent) {
	if ev.Err != nil {
		log.Fatalf("watch ended: %v", ev.Err)
	}
	fmt.Printf("  [watch] %s %s @ %s (revision %d)\n\n", ev.Type, ev.Instance.ID, ev.Instance.Address, ev.Revision)
}
