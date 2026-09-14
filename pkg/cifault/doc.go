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

// Package cifault classifies why a continuous-integration check went red, so
// the engine knows WHO has to act on it before it wakes anybody.
//
// A red check is three different events wearing one colour: the change's own
// test failure, a runner that died holding the job, and a host that could not
// start the job at all. Answering them the same way is what costs money — a
// settled red was decisive, the job went to re-delegation, and a coding agent
// was paid to author another attempt at a change that was never the problem.
// Measured on 2026-09-12: one CI host provisioned without its config file
// cost three lanes roughly 1.5M tokens re-deriving the same symptom.
//
// # The verdicts
//
// Classify answers a Verdict, and the verdict names the actor:
//
//   - VerdictGenuine reaches the coding agent, as a red check always has.
//   - VerdictVoid is not a verdict on the change at all. The check is re-run
//     automatically and no agent is woken. The caller bounds the re-runs; a
//     host that is permanently broken must escalate, not loop.
//   - VerdictHostDefect is the machine. An operator is told; no agent is woken,
//     because no change the agent could author would fix it.
//
// # The asymmetry that shapes every rule here
//
// The two misclassifications are not equally expensive. Calling a genuine
// failure "void" auto-retries a broken change — the engine pays for CI over
// and over and the defect never reaches anyone who could fix it. Calling a
// void run "genuine" costs one wasted agent attempt, which is what happens
// today anyway.
//
// So GENUINE IS THE DEFAULT AND EVERY OTHER VERDICT MUST BE EARNED by a
// signature that only infrastructure produces. A log this package does not
// recognise is the change's own failure. A log it cannot read at all is the
// change's own failure. Nothing is void by absence of evidence.
//
// # Testable without a forge
//
// Classify is a pure function of (conclusion, log text, runner state). It
// talks to no forge, reads no clock and holds no state, so the fixture corpus
// under testdata is real job logs and the tests are table-driven over them.
package cifault
