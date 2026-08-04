// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import dpop "github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"

// ---------------------------------------------------------------------------
// DPoP (RFC 9449) proof-of-possession primitives
//
// Re-exported so platform consumers that validate platform tokens in-process can
// build the validator's replay cache through the quicbaas SDK rather than
// importing go-quicbaas/pkg/iam/dpop directly (the Layer-5 SDK-only boundary).
// ---------------------------------------------------------------------------

type (
	// DPoPClock is the time source a replay cache uses for jti expiry.
	DPoPClock = dpop.Clock
	// DPoPSystemClock is the production wall-clock implementation of DPoPClock.
	DPoPSystemClock = dpop.SystemClock
	// DPoPReplayCache deduplicates DPoP proof jti values to prevent replay.
	DPoPReplayCache = dpop.ReplayCache
	// DPoPMemoryReplayCache is the in-memory DPoPReplayCache implementation.
	DPoPMemoryReplayCache = dpop.MemoryReplayCache
	// DPoPMemoryReplayCacheOption configures a DPoPMemoryReplayCache.
	DPoPMemoryReplayCacheOption = dpop.MemoryReplayCacheOption
)

// NewDPoPMemoryReplayCache constructs an in-memory DPoP replay cache.
var NewDPoPMemoryReplayCache = dpop.NewMemoryReplayCache

// NewDPoPProof builds a signed DPoP proof JWT (RFC 9449) bound to the given
// HTTP method (htm) and target URI (htu). Re-exported so platform consumers can
// construct sender-constrained proofs through the quicbaas SDK rather than
// importing go-quicbaas/pkg/iam/dpop directly (the Layer-5 SDK-only boundary).
var NewDPoPProof = dpop.NewProof

// NewDPoPProofWithClock is NewDPoPProof with an injectable clock, for tests that
// need a fixed issuance time.
var NewDPoPProofWithClock = dpop.NewProofWithClock

// DPoPThumbprint returns the RFC 7638 JWK SHA-256 thumbprint (jkt) of a public
// key, used to bind an access token to a DPoP key.
var DPoPThumbprint = dpop.Thumbprint
