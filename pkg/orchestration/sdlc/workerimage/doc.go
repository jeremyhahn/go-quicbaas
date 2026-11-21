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

// Package workerimage keeps the Software Factory worker image current: a
// scheduled, gated refresh that rebuilds the image when the npm registry
// publishes a newer agent CLI than the bound image carries, proves the new
// image, publishes it and only then rebinds projects to it.
//
// WHY IT EXISTS. The worker image installs Claude Code and Codex at build time
// and nothing else ever changes them, so a model release a newer CLI is needed
// for (claude-opus-5-5 needs Claude Code 2.1.280) strands every project until
// somebody rebuilds by hand — and every remote node then needs the new image
// copied to it by hand as well.
//
// WHY IT IS NOT AN UPDATE AT CONTAINER START. A worker that upgrades its own
// CLI as it starts runs a version nobody tested, pays for the download on
// every spawn, and fails for the network rather than for the work. The owner's
// ruling (2026-09-22) is a refresh that happens ONCE, off the job path, and
// that promotes only what has passed the gate.
//
// ONE PASS, FIVE STEPS (Refresher.Refresh):
//
//  1. Detect — read the latest @anthropic-ai/claude-code and @openai/codex
//     from the registry, and the versions every bound image of the managed
//     repository carries from its labels. Nothing behind means nothing to do.
//  2. Build — the embedded Dockerfile with this binary's toolchain pins and
//     the newer CLIs, under the content tag "<base>-cc<claude>-cx<codex>".
//  3. Gate — the image's probes against exactly the built image ID, then a
//     real session per agent the promotion would run in it, through the
//     orchestrator's own spawn path on the pooled credential.
//  4. Publish — when a registry is configured, push and record the manifest
//     digest; the reference Agents get is digest-pinned. The push logs in
//     with a token read from a QuicBaaS tenant secret at that moment
//     (SecretCredential), under a private engine configuration.
//  5. Promote — repoint every Agent naming a stale image, through the agent
//     catalog. Previous tags stay on the host for rollback, and every
//     promotion is audited.
//
// LOCAL MODE NEVER STRANDS A FLEET. With no registry, the promoted reference
// is a tag only this host's engine holds, so a pass refuses to promote — before
// it builds — in any tenant whose fleet has a node other than this one
// (LocalPromotionRefusedError).
//
// A REGISTRY ADOPTS WHAT IS ALREADY BOUND. With a registry, an Agent naming a
// current image by a local tag is published as it is — the same image, not
// rebuilt — and repointed to "<registry>:<tag>@sha256:…", so turning a
// registry on moves the fleet onto it without anyone repointing by hand.
//
// A failed pass changes no binding, is logged as an error, counted on the
// quicbaas_sdlc_worker_image_refresh_total metric and recorded on the audit
// trail — there is no failure mode that is silent.
package workerimage
