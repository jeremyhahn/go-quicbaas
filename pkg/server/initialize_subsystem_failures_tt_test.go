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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag && iam

// initialize_subsystem_failures_tt_test.go covers the per-subsystem error
// returns inside Server.initialize that the happy-path lifecycle tests do not
// reach. Each test enables a subsystem and injects a failure through its
// established function seam, then asserts initialize aborts. These exercise both
// the subsystem's own error path AND the corresponding `return err` line in
// initialize.

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// TestServer_Initialize_TT_MessagingFailurePropagates enables messaging and
// injects a push-service start failure so the startMessaging error return and
// its propagation through initialize both run.
func TestServer_Initialize_TT_MessagingFailurePropagates(t *testing.T) {
	saveAndRestoreSeams(t)
	srv := withTempServer(t)
	srv.config.Server.Embedded = true
	srv.config.Messaging.Enabled = true

	sentinel := errors.New("push service refused to start")
	pushServiceStartFn = func(*messaging.PushService, context.Context) error { return sentinel }

	err := srv.initialize()
	require.Error(t, err, "initialize must abort when the messaging subsystem fails to start")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
