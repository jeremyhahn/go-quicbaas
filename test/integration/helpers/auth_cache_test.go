// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//go:build integration

package helpers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPivCachedTokenUsable_RefusesTokenInsideMargin(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	tok := pivCachedToken{Bearer: "b", ExpiresAt: now.Add(90 * time.Second)}
	require.False(t, pivCachedTokenUsable(tok, now),
		"a bearer with less than the reuse margin left would be dropped by the CLI")

	atMargin := pivCachedToken{Bearer: "b", ExpiresAt: now.Add(pivReuseMargin)}
	require.False(t, pivCachedTokenUsable(atMargin, now))
}

func TestPivCachedTokenUsable_ReusesTokenWithAmpleLife(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	tok := pivCachedToken{Bearer: "b", ExpiresAt: now.Add(10 * time.Minute)}
	require.True(t, pivCachedTokenUsable(tok, now))
}

func TestPivCachedTokenUsable_RefusesEmptyBearer(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	require.False(t, pivCachedTokenUsable(pivCachedToken{ExpiresAt: now.Add(time.Hour)}, now))
}

// TestPivCachedAuthPIV_SkipsTokenInsideMargin seeds the cache with a bearer
// about to expire and checks authPIV does not return it: it falls through
// to a fresh login, which fails here because the certificate paths do not
// exist.
func TestPivCachedAuthPIV_SkipsTokenInsideMargin(t *testing.T) {
	const base = "https://cache-margin.invalid:1"
	dir := t.TempDir()
	cert, key, ca := dir+"/c.crt", dir+"/c.key", dir+"/ca.crt"
	cacheKey := base + "|" + cert + "|" + key + "|" + ca

	pivAuthCache.Lock()
	pivAuthCache.tokens[cacheKey] = pivCachedToken{Bearer: "stale", ExpiresAt: time.Now().Add(30 * time.Second)}
	pivAuthCache.Unlock()
	t.Cleanup(func() {
		pivAuthCache.Lock()
		delete(pivAuthCache.tokens, cacheKey)
		pivAuthCache.Unlock()
	})

	bearer, _, err := authPIV(context.Background(), base, cert, key, ca)
	require.Error(t, err, "a bearer inside the margin must not be reused")
	require.Empty(t, bearer)

	pivAuthCache.Lock()
	pivAuthCache.tokens[cacheKey] = pivCachedToken{Bearer: "fresh", ExpiresAt: time.Now().Add(10 * time.Minute)}
	pivAuthCache.Unlock()

	bearer, _, err = authPIV(context.Background(), base, cert, key, ca)
	require.NoError(t, err)
	require.Equal(t, "fresh", bearer, "a bearer with ample life is reused")
}
