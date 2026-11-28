// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package iam

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"
)

// importUsersN builds n distinct users to import.
func importUsersN(n int) []*UserImport {
	users := make([]*UserImport, n)
	for i := range users {
		users[i] = &UserImport{
			Email:    fmt.Sprintf("leak%d@example.com", i),
			Username: fmt.Sprintf("leak%d", i),
		}
	}
	return users
}

// settledGoroutines waits for the goroutine count to fall to at most want.
func settledGoroutines(want int) int {
	deadline := time.Now().Add(2 * time.Second)
	n := runtime.NumGoroutine()
	for n > want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		n = runtime.NumGoroutine()
	}
	return n
}

// An import far larger than the worker pool's buffers finishes: results
// are drained while jobs are still being submitted (#108).
func TestImportUsers_LargerThanPoolBuffersFinishes(t *testing.T) {
	bm, _, _ := newImportEnabledFixture(t)
	const n = 40

	done := make(chan *BatchImportResult, 1)
	errc := make(chan error, 1)
	go func() {
		res, err := bm.ImportUsers(context.Background(), "acme", importUsersN(n), BatchImportOptions{Parallelism: 1, BatchSize: n})
		errc <- err
		done <- res
	}()

	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("ImportUsers: %v", err)
		}
		if res := <-done; res.SuccessCount != n {
			t.Fatalf("SuccessCount = %d, want %d (errors %v)", res.SuccessCount, n, res.Errors)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ImportUsers of 40 users with one worker did not return")
	}
}

// A canceled import leaves no goroutine behind, even with jobs still
// queued when the pool stops (#108).
func TestImportUsers_CanceledLeavesNoGoroutine(t *testing.T) {
	bm, _, _ := newImportEnabledFixture(t)
	before := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		opts := BatchImportOptions{Parallelism: 2, BatchSize: 30}
		roles := make([]*RoleImport, 30)
		policies := make([]*PolicyImport, 30)
		for k := range roles {
			roles[k] = &RoleImport{ID: fmt.Sprintf("role-%d", k), Name: fmt.Sprintf("role-%d", k)}
			policies[k] = &PolicyImport{Name: fmt.Sprintf("policy-%d", k)}
		}
		if _, err := bm.ImportUsers(ctx, "acme", importUsersN(30), opts); err != nil && !errors.Is(err, ErrBatchCanceled) {
			t.Fatalf("ImportUsers: %v", err)
		}
		if _, err := bm.ImportRoles(ctx, "acme", roles, opts); err != nil && !errors.Is(err, ErrBatchCanceled) {
			t.Fatalf("ImportRoles: %v", err)
		}
		if _, err := bm.ImportPolicies(ctx, "acme", policies, opts); err != nil && !errors.Is(err, ErrBatchCanceled) {
			t.Fatalf("ImportPolicies: %v", err)
		}
	}

	if after := settledGoroutines(before); after > before {
		t.Fatalf("goroutines: %d before, %d after 20 rounds of canceled imports", before, after)
	}
}
