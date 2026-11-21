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

// Package sdlc is the SDLC module of the Software Factory: git-based delivery
// automation against a forge (Gitea today), plugged into the generic
// orchestration engine through its four seams.
//
// The engine owns the loop. Tenant → Project → Workflow → Job → Task →
// Session is orchestration's model, and the durable job lease, the attempt
// budget, the reaper and the blocked/readmit lifecycle are all its. This
// module owns nothing that loops: it supplies work (a TaskSource that turns
// labelled forge issues into Tasks), judges published work (a forge_review
// validator that reads approvals and checks on the current head), and acts at
// the five lifecycle moments of a Job (worktree and branch on Prepare, signed
// commit + push + pull request on Reviewable, head-guarded merge on Complete,
// worktree teardown on Discard, a comment on the issue on Blocked).
//
// Its two records both REFERENCE orchestration ids and never duplicate a
// Project or a Task: a ProjectBinding attaches a forge repository and its
// review policy to a Project, and a Delivery records what one Task's attempt
// published — branch, head, pull request, reviews and checks.
//
// pkg/orchestration never imports this package; this package imports it.
package sdlc
