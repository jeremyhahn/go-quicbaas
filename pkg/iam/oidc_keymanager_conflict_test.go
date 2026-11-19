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

package iam

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// errKeyNotFound is the miss every current closure returns.
var errKeyNotFound = errors.New("not found")

// conflictErr wraps the backend sentinel as the PKCS8 backend's errs.Detail
// and the quicbaas wrapper carry it up.
func conflictErr() error {
	return fmt.Errorf("store: %w", xkmssdk.ErrBackendKeyAlreadyExists)
}

func newWinnerKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate winner key: %v", err)
	}
	return key
}

// currentAfter returns a current closure that misses on its first misses
// calls and returns key afterwards, counting calls in *calls.
func currentAfter(key crypto.Signer, misses int, calls *int) func(context.Context, *xkmssdk.KeyAttributes) (crypto.Signer, error) {
	return func(context.Context, *xkmssdk.KeyAttributes) (crypto.Signer, error) {
		*calls++
		if key == nil || *calls <= misses {
			return nil, errKeyNotFound
		}
		return key, nil
	}
}

func assertWinner(t *testing.T, got crypto.Signer, winner *ecdsa.PrivateKey) {
	t.Helper()
	pub, ok := got.Public().(*ecdsa.PublicKey)
	if !ok || !pub.Equal(&winner.PublicKey) {
		t.Fatalf("signer public key is not the winner's")
	}
}

func TestLoadSigner_CreateConflictLoadsWinnersKey(t *testing.T) {
	winner := newWinnerKey(t)
	var currentCalls, generateCalls int
	src := signerSource{
		current: currentAfter(winner, 1, &currentCalls),
		generate: func(context.Context, *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
			generateCalls++
			return nil, conflictErr()
		},
		signer: func(context.Context, *xkmssdk.KeyAttributes) (crypto.Signer, error) {
			t.Fatal("signer must not be called after a create conflict")
			return nil, nil
		},
		reloadAttempts: 3,
		reloadBackoff:  time.Millisecond,
	}

	got, err := src.load(context.Background(), &xkmssdk.KeyAttributes{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertWinner(t, got, winner)
	if generateCalls != 1 {
		t.Fatalf("generate calls = %d, want 1", generateCalls)
	}
	if currentCalls != 2 {
		t.Fatalf("current calls = %d, want 2", currentCalls)
	}
}

func TestLoadSigner_CreateConflictRetriesUntilVisible(t *testing.T) {
	winner := newWinnerKey(t)
	var currentCalls int
	src := signerSource{
		current: currentAfter(winner, 3, &currentCalls),
		generate: func(context.Context, *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
			return nil, conflictErr()
		},
		signer: func(context.Context, *xkmssdk.KeyAttributes) (crypto.Signer, error) {
			t.Fatal("signer must not be called after a create conflict")
			return nil, nil
		},
		reloadAttempts: 3,
		reloadBackoff:  time.Millisecond,
	}

	got, err := src.load(context.Background(), &xkmssdk.KeyAttributes{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertWinner(t, got, winner)
	if currentCalls != 4 {
		t.Fatalf("current calls = %d, want 4", currentCalls)
	}
}

func TestLoadSigner_CreateConflictGivesUpAfterAttempts(t *testing.T) {
	var currentCalls int
	src := signerSource{
		current: currentAfter(nil, 0, &currentCalls),
		generate: func(context.Context, *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
			return nil, conflictErr()
		},
		signer: func(context.Context, *xkmssdk.KeyAttributes) (crypto.Signer, error) {
			t.Fatal("signer must not be called after a create conflict")
			return nil, nil
		},
		reloadAttempts: 3,
		reloadBackoff:  time.Millisecond,
	}

	_, err := src.load(context.Background(), &xkmssdk.KeyAttributes{})
	var reloadErr *SigningKeyReloadError
	if !errors.As(err, &reloadErr) {
		t.Fatalf("error = %v, want *SigningKeyReloadError", err)
	}
	if reloadErr.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", reloadErr.Attempts)
	}
	if !errors.Is(err, xkmssdk.ErrBackendKeyAlreadyExists) {
		t.Fatalf("error does not carry ErrBackendKeyAlreadyExists: %v", err)
	}
	if currentCalls != 4 {
		t.Fatalf("current calls = %d, want 4", currentCalls)
	}
}

func TestLoadSigner_OtherCreateErrorPropagates(t *testing.T) {
	hsmOffline := errors.New("hsm offline")
	var currentCalls int
	src := signerSource{
		current: currentAfter(nil, 0, &currentCalls),
		generate: func(context.Context, *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
			return nil, hsmOffline
		},
		signer: func(context.Context, *xkmssdk.KeyAttributes) (crypto.Signer, error) {
			t.Fatal("signer must not be called after a failed create")
			return nil, nil
		},
		reloadAttempts: 3,
		reloadBackoff:  time.Millisecond,
	}

	_, err := src.load(context.Background(), &xkmssdk.KeyAttributes{})
	if !errors.Is(err, hsmOffline) {
		t.Fatalf("error = %v, want %v", err, hsmOffline)
	}
	if currentCalls != 1 {
		t.Fatalf("current calls = %d, want 1", currentCalls)
	}
}

func TestLoadSigner_FirstCreateUsesSigner(t *testing.T) {
	created := newWinnerKey(t)
	var currentCalls int
	src := signerSource{
		current: currentAfter(nil, 0, &currentCalls),
		generate: func(context.Context, *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
			return created, nil
		},
		signer: func(context.Context, *xkmssdk.KeyAttributes) (crypto.Signer, error) {
			return created, nil
		},
		reloadAttempts: 3,
		reloadBackoff:  time.Millisecond,
	}

	got, err := src.load(context.Background(), &xkmssdk.KeyAttributes{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertWinner(t, got, created)
}
