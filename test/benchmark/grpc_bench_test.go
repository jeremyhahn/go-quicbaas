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

//go:build integration
// +build integration

package benchmark

import (
	"context"
	"crypto/tls"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

func benchEndpoint() string {
	// Prefer explicit GRPC endpoints if set by compose.
	if v := os.Getenv("QUICBAAS_NODE1_GRPC"); v != "" {
		return v
	}
	if v := os.Getenv("QUICBAAS_NODE1"); v != "" { // compat
		return v
	}
	// Default for local runs.
	return "localhost:50051"
}

func setupBenchClient(b *testing.B) (pb.QuicBaaSClient, func()) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	conn, err := grpc.DialContext(ctx, benchEndpoint(),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})),
		grpc.WithBlock(),
	)
	if err != nil {
		cancel()
		b.Fatalf("failed to dial: %v", err)
	}
	client := pb.NewQuicBaaSClient(conn)
	cleanup := func() {
		conn.Close()
		cancel()
	}
	return client, cleanup
}

// BenchmarkPut_GRPC measures gRPC Put operation throughput.
func BenchmarkPut_GRPC(b *testing.B) {
	client, cleanup := setupBenchClient(b)
	defer cleanup()

	ctx := context.Background()
	data := []byte(`{"name":"bench","v":1}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.Put(ctx, &pb.PutRequest{EntityType: "bench", Id: uint64(i + 1), Data: data})
		if err != nil {
			b.Fatalf("put failed: %v", err)
		}
	}
}

// BenchmarkGet_GRPC measures gRPC Get operation throughput.
func BenchmarkGet_GRPC(b *testing.B) {
	client, cleanup := setupBenchClient(b)
	defer cleanup()

	ctx := context.Background()
	// Seed one key to read repeatedly.
	_, err := client.Put(ctx, &pb.PutRequest{EntityType: "bench", Id: 42, Data: []byte(`{"name":"seed"}`)})
	if err != nil {
		b.Fatalf("seed put failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.Get(ctx, &pb.GetRequest{EntityType: "bench", Id: 42})
		if err != nil {
			b.Fatalf("get failed: %v", err)
		}
	}
}
