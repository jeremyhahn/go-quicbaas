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

//go:build integration

// SIGKILL durability: a write the server acknowledged must survive the
// process being killed without a chance to flush. The parent test re-execs
// its own test binary as a child that boots the server, writes, prints one
// ACK line per acknowledged write and blocks; the parent SIGKILLs it after
// the last ACK and reboots over the same data dir in-process.

package durability

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const (
	sigkillChildEnv    = "QUICBAAS_SIGKILL_CHILD"
	sigkillEntityType  = "sigkill_durability"
	sigkillEntityCount = 200
	sigkillUserCount   = 5
	sigkillTenantID    = "sigkill-durability"
	sigkillChildWait   = 5 * time.Minute
)

// sigkillEntityValue is the payload written for entity i; the parent
// recomputes it to check the value that read back.
func sigkillEntityValue(i int) []byte {
	return []byte(fmt.Sprintf("sigkill-entity-%03d", i))
}

func sigkillUserEmail(i int) string {
	return fmt.Sprintf("sigkill-user-%d@example.com", i)
}

// sigkillAck prints the child's "ACK <kind> <id>" line on stdout, which the
// parent reads; it is called only after the write returned success.
func sigkillAck(kind string, id uint64) {
	_, _ = fmt.Fprintf(os.Stdout, "ACK %s %d\n", kind, id)
}

// TestSIGKILLChild is the child half of TestSIGKILLAcknowledgedWritesSurvive.
// It runs only when the parent sets QUICBAAS_SIGKILL_CHILD to a data dir.
func TestSIGKILLChild(t *testing.T) {
	dataDir := os.Getenv(sigkillChildEnv)
	if dataDir == "" {
		t.Skip(sigkillChildEnv + " not set: this is the child of TestSIGKILLAcknowledgedWritesSurvive")
	}
	ctx := context.Background()

	srv := bootServer(t, dataDir)
	svc := srv.Service()
	iamSvc := iamService(t, srv)

	_, err := iamSvc.CreateTenant(ctx, &service.CreateTenantRequest{ID: sigkillTenantID, Name: "SIGKILL Tenant"})
	require.NoError(t, err, "create tenant")
	kvCtx := tenantctx.WithTenant(ctx, sigkillTenantID)

	for i := 0; i < sigkillEntityCount; i++ {
		resp, err := svc.Put(kvCtx, &service.PutRequest{
			EntityType: sigkillEntityType,
			ID:         uint64(i + 1),
			Data:       sigkillEntityValue(i),
		})
		require.NoError(t, err, "put entity %d", i)
		require.NotNil(t, resp)
		sigkillAck("entity", uint64(i))
	}

	for i := 0; i < sigkillUserCount; i++ {
		resp, err := iamSvc.CreateUser(ctx, &service.IAMCreateUserRequest{
			TenantID: sigkillTenantID,
			Email:    sigkillUserEmail(i),
			Username: fmt.Sprintf("sigkilluser%d", i),
			Name:     fmt.Sprintf("SIGKILL User %d", i),
		})
		require.NoError(t, err, "create user %d", i)
		require.NotNil(t, resp.User)
		sigkillAck("user", resp.User.ID)
	}
	sigkillAck("done", 0)

	select {}
}

// TestSIGKILLAcknowledgedWritesSurvive kills a server child with SIGKILL after
// it acknowledged its writes and checks that every one of them survives.
func TestSIGKILLAcknowledgedWritesSurvive(t *testing.T) {
	if os.Getenv(sigkillChildEnv) != "" {
		t.Skip("running as the SIGKILL child")
	}
	dataDir := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=^TestSIGKILLChild$", "-test.v")
	cmd.Env = append(os.Environ(), sigkillChildEnv+"="+dataDir)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start(), "start child")
	exited := false
	t.Cleanup(func() {
		if !exited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	var entities []int
	var users []uint64
	done := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) != 3 || fields[0] != "ACK" {
				// The child's own test output (a failure in it, above all).
				fmt.Fprintln(os.Stderr, "child:", sc.Text())
				continue
			}
			id, perr := strconv.ParseUint(fields[2], 10, 64)
			if perr != nil {
				done <- fmt.Errorf("bad ACK line %q: %w", sc.Text(), perr)
				return
			}
			switch fields[1] {
			case "entity":
				entities = append(entities, int(id))
			case "user":
				users = append(users, id)
			case "done":
				done <- nil
				return
			}
		}
		done <- fmt.Errorf("child stdout closed before the last ACK (scan error: %v)", sc.Err())
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(sigkillChildWait):
		t.Fatalf("child did not acknowledge its writes within %s", sigkillChildWait)
	}
	require.Len(t, entities, sigkillEntityCount, "child must acknowledge every entity")
	require.Len(t, users, sigkillUserCount, "child must acknowledge every user")

	require.NoError(t, cmd.Process.Signal(syscall.SIGKILL), "SIGKILL child")
	waitErr := cmd.Wait()
	exited = true
	require.Error(t, waitErr, "child must die by the signal")

	srv := bootServer(t, dataDir)
	defer shutdownServer(t, srv)
	ctx := context.Background()
	svc := srv.Service()

	for _, i := range entities {
		got, err := svc.Get(tenantctx.WithTenant(ctx, sigkillTenantID), &service.GetRequest{EntityType: sigkillEntityType, ID: uint64(i + 1)})
		if !assert.NoError(t, err, "acknowledged entity %d must survive SIGKILL", i) {
			continue
		}
		assert.True(t, got.Found, "acknowledged entity %d must be found after SIGKILL", i)
		assert.Equal(t, sigkillEntityValue(i), got.Data, "entity %d value must survive SIGKILL", i)
	}

	iamSvc := iamService(t, srv)
	for _, id := range users {
		got, err := iamSvc.GetUser(ctx, &service.IAMGetUserRequest{TenantID: sigkillTenantID, UserID: id})
		if assert.NoError(t, err, "acknowledged user %d must survive SIGKILL", id) {
			assert.NotNil(t, got.User, "acknowledged user %d must be found after SIGKILL", id)
		}
	}
}
