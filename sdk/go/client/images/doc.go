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

// Package images is the client for the QuicBaaS OCI registry: the OCI
// distribution API served at /v2/ on the node's REST listener.
//
// The registry authenticates the caller by its TLS client certificate, which
// the *http.Client given to WithHTTPClient carries. When the registry also
// requires attestation, the caller fetches a nonce with Challenge, produces
// evidence over it, and presents the evidence once with Attest; the client
// then keeps the session the registry returns and sends it on every later
// request.
//
// The client computes the digest of every blob it pushes and checks the
// digest of everything it pulls by digest.
//
// # Pushing and pulling
//
//	c, err := images.NewClient("https://node.example:8443",
//	    images.WithHTTPClient(mtlsClient))
//	nonce, err := c.Challenge(ctx)
//	err = c.Attest(ctx, evidenceOver(nonce))
//	cfg, err := c.PushBlob(ctx, "team/app", configJSON)
//	layer, err := c.PushBlob(ctx, "team/app", layerTar)
//	pushed, err := c.PutManifest(ctx, "team/app", "v1",
//	    images.MediaTypeOCIManifest, manifestJSON)
//	m, err := c.GetManifest(ctx, "team/app", pushed.Digest)
//	data, err := c.GetBlob(ctx, "team/app", layer)
package images
