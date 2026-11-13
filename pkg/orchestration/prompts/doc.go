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

// Package prompts holds every instruction the Software Factory sends an
// agent, as markdown a person can edit, and the machinery that keeps an
// edited prompt honest.
//
// # The shipped content is the system tenant's set
//
// A package that sends prompts owns a set: it writes the markdown under its
// own promptdefaults/<set>/ directory, embeds it, and registers a SetSpec
// from init(). THOSE EMBEDDED FILES ARE THE SYSTEM TENANT'S DEFAULTS. They
// are what the boot reconciler writes into the system tenant's prompt
// records and what every new tenant is seeded with, so the tree says where
// a running node's prompts came from:
//
//	pkg/orchestration/promptdefaults/workflow/*.md   set "workflow", owned by the engine
//	pkg/orchestration/sdlc/promptdefaults/sdlc/*.md                set "sdlc",     owned by the SDLC module
//
// Nothing in this package reads a file at runtime. A prompt's Default is
// compiled in; a tenant's own copy lives in the store that is layered over
// these defaults.
//
// # Templating is substitution, not a language
//
// A prompt's body may carry ${name} holes, and NOTHING ELSE. Every hole must
// be declared by the prompt's spec, and a declared-required hole must be
// present. There are no conditionals and no loops: a template language in
// human-edited markdown is a template language nothing can validate, and an
// unvalidatable prompt is the failure the whole store exists to prevent.
// Loops, plurals, fenced logs, redaction, renumbered comments and every JSON
// assembly stay in Go, as thin assemblers over a set.
//
// # Validation happens twice
//
// Once at registration, where an embedded default that fails its own spec
// panics the process in init like an unregisterable job runner, and once at
// save, where a tenant's edit is refused by name. The rules are the same
// both times: the body is not blank, every ${x} in it is declared, every
// required variable appears, and every marker the engine parses is still
// there. Markers are declared by the owning package from its own parser's
// constants, so the validator learns what SDLC-STOP: is by registration
// rather than by having it retyped here.
//
// # Files
//
// A default file ends with exactly one trailing newline, as every text file
// should, and that newline is NOT part of the prompt. Structural whitespace
// between assembled pieces belongs to the assembler, never to the file, so a
// prompt stays something an editor can save without changing what an agent
// is told.
package prompts
