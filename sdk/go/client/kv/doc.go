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

// Package kv provides the KV client for the QuicBaaS SDK.
//
// The KV client supports CRUD operations and watch functionality
// for key change notifications.
//
// # Basic Usage
//
//	client := kv.NewClient(transport)
//
//	// Store a value
//	resp, err := client.Put(ctx, &kv.PutRequest{
//	    EntityType: "users",
//	    Key:        "user-123",
//	    Value:      []byte(`{"name": "Alice"}`),
//	})
//
//	// Retrieve a value
//	resp, err := client.Get(ctx, &kv.GetRequest{
//	    EntityType: "users",
//	    Key:        "user-123",
//	})
//
// # Watch for Changes
//
// The KV client supports watches for real-time change notifications:
//
//	watcher, err := client.WatchPrefix(ctx, "users/",
//	    kv.WithPrevKV(),           // Include previous value
//	    kv.WithStartRevision(100), // Start from revision 100
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer watcher.Close()
//
//	for event := range watcher.Events() {
//	    switch event.Type {
//	    case kv.EventPut:
//	        log.Printf("PUT %s: %s", event.Key, string(event.Value))
//	    case kv.EventDelete:
//	        log.Printf("DELETE %s", event.Key)
//	    }
//	}
//
// # Watch Options
//
// Available watch options:
//
//   - WithStartRevision(rev): Start watching from a specific MVCC revision
//   - WithPrevKV(): Include the previous value in watch events
//   - WithProgressNotify(): Receive periodic progress notifications
//   - WithFilterPut(): Only receive DELETE events
//   - WithFilterDelete(): Only receive PUT events
package kv
