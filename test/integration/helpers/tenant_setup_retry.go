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

package helpers

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// tenantSetupRetryWindow is how long tenant setup keeps retrying after a
// leader-loss error. The qrdb test cluster can lose its raft leader on a
// loaded runner, and a new leader is normally elected well inside this
// window. It is a var so the unit test can shrink it.
var tenantSetupRetryWindow = 90 * time.Second

// tenantSetupRetryPause is the wait between two tenant setup attempts. It is
// a var so the unit test can shrink it.
var tenantSetupRetryPause = 2 * time.Second

// leaderLossError reports whether err is what a tenant provision or delete
// returns while the qrdb test cluster has no leader (a leader change): a 503,
// a 401 or 500 that says "not leader", a 500 with the generic internal body,
// or a client timeout. Any other error is not a leader loss and must still
// fail the setup at once.
func leaderLossError(err error) bool {
	if err == nil {
		return false
	}
	var bad *AuthBadStatusError
	if errors.As(err, &bad) {
		switch bad.Status {
		case http.StatusServiceUnavailable:
			return true
		case http.StatusUnauthorized:
			return strings.Contains(bad.Body, "not leader")
		case http.StatusInternalServerError:
			return strings.Contains(bad.Body, "not leader") ||
				strings.Contains(bad.Body, "internal: the request could not be completed")
		}
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// retryTenantSetup runs call and, while it fails with a leaderLossError,
// runs it again every tenantSetupRetryPause until tenantSetupRetryWindow has
// passed since the first call or ctx is done. A leader change on the test
// cluster makes tenant setup fail for a few seconds; this rides it out and
// logs one line per retry so a run shows how often it happened. Any other
// error is returned at once, and when the window or ctx ends the last error
// is returned unchanged.
func retryTenantSetup(ctx context.Context, op, tenantID string, call func() error) error {
	start := time.Now()
	err := call()
	for attempt := 1; err != nil && leaderLossError(err); attempt++ {
		if time.Since(start) >= tenantSetupRetryWindow || ctx.Err() != nil {
			return err
		}
		log.Printf("tenant setup retry after leader loss: op=%s tenant=%s attempt=%d: %v", op, tenantID, attempt, err)
		timer := time.NewTimer(tenantSetupRetryPause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
		err = call()
	}
	return err
}
