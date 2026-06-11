// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration && rest

package login

import (
	"context"
	"sync"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/stretchr/testify/require"
)

// TestUnifiedLogin_ConcurrentLoginsMintDistinctBearers is the end-to-end analog
// of SECURITY assertion (d): concurrent same-tenant logins must resolve to
// DISTINCT sessions (distinct bearers), never a shared/aliased session. The
// session-issuance code path (pkg/server/login_session_issuer.go) is the SAME
// one WebAuthn uses, so this asserts the distinct-session property for the
// whole unified ceremony without needing a virtual authenticator.
//
// We fire N concurrent mTLS logins for the SAME admin identity and assert:
//   - every login succeeds and resolves to the SAME subject (correctness), and
//   - every issued bearer is UNIQUE (distinct sessions, no aliasing), and
//   - every bearer independently validates.
//
// Goroutines are coordinated with a sync.WaitGroup and a start barrier
// (close-of-channel) so they race the server concurrently — no time.Sleep.
func TestUnifiedLogin_ConcurrentLoginsMintDistinctBearers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	adminLeaf := loadAdminLeaf(t)

	// Ensure the admin mtls factor exists.
	enrollClient, err := newLoginClient(t, transportREST, mtlsTLSConfig(t))
	require.NoError(t, err)
	defer enrollClient.Close()
	if eerr := enrollMTLSFactor(ctx, enrollClient, adminSubject, adminLeaf); eerr != nil {
		require.True(t, isAlreadyEnrolled(eerr), "enroll admin mtls factor: %v", eerr)
	}

	const concurrency = 8

	type outcome struct {
		bearer  string
		subject string
		err     error
	}

	results := make([]outcome, concurrency)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		i := i
		go func() {
			defer wg.Done()
			c, cerr := newLoginClient(t, transportREST, mtlsTLSConfig(t))
			if cerr != nil {
				results[i] = outcome{err: cerr}
				return
			}
			defer c.Close()

			<-start // race the server together
			res, lerr := runSDKLogin(ctx, c,
				qrdbtransport.FactorTypeMTLS, adminSubject,
				certSHA256(adminLeaf), nil, nil)
			if lerr != nil {
				results[i] = outcome{err: lerr}
				return
			}
			results[i] = outcome{bearer: res.bearer, subject: res.subject}
		}()
	}

	close(start)
	wg.Wait()

	seenBearers := make(map[string]int, concurrency)
	var firstSubject string
	for i, r := range results {
		require.NoError(t, r.err, "concurrent login #%d must succeed", i)
		require.NotEmpty(t, r.bearer, "concurrent login #%d must mint a bearer", i)
		require.NotEmpty(t, r.subject, "concurrent login #%d must resolve a subject", i)

		if firstSubject == "" {
			firstSubject = r.subject
		} else {
			require.Equal(t, firstSubject, r.subject,
				"all concurrent logins for the same identity must resolve to the same subject")
		}

		if prev, dup := seenBearers[r.bearer]; dup {
			t.Fatalf("concurrent logins #%d and #%d minted the SAME bearer — sessions were aliased, not distinct", prev, i)
		}
		seenBearers[r.bearer] = i
	}

	require.Len(t, seenBearers, concurrency,
		"each concurrent login must mint a DISTINCT session bearer")

	// Every distinct bearer must independently validate.
	for bearer := range seenBearers {
		require.NoError(t, validateBearerOverREST(ctx, bearer),
			"each concurrently-issued bearer must validate independently")
	}
}
