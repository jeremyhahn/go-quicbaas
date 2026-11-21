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

package sdlc

import "errors"

// =============================================================================
// Triage errors
// =============================================================================
//
// Triage's failures divide into two classes that MUST NOT be collapsed, because
// the module's response to them is opposite:
//
//   - The condition cannot be READ (absent, malformed, a kind nothing handles).
//     The item stays blocked and something is wrong with the record.
//   - The condition can be read and cannot be DECIDED (the store failed, the
//     subject vanished). The item stays blocked and something is wrong with the
//     world.
//
// Both leave the item blocked, which is why folding them together is tempting
// and wrong: an operator repairs the first by fixing a record and the second by
// fixing an outage, and a single "could not unblock" error sends them to the
// wrong one.

var (
	// ErrUnblockConditionAbsent indicates a blocked item whose reason carries
	// no typed condition.
	//
	// NOT a malformed condition — the ordinary shape of an item blocked by
	// something that wrote prose. It is reported rather than tolerated silently
	// because an item that cannot say what would unblock it is the invisible
	// work the Blocked state exists to surface: nothing can check it, so
	// nothing ever will, and it waits for somebody to notice it by eye.
	ErrUnblockConditionAbsent = errors.New("sdlc: blocked item carries no typed unblock condition")

	// ErrUnblockConditionMalformed indicates a marker that is present and
	// unreadable.
	//
	// Distinct from absent because the two say different things about the
	// writer: absent means nobody recorded a condition, malformed means
	// something recorded one this package cannot parse — a version skew or a
	// hand-edited record, and a bug rather than a gap.
	ErrUnblockConditionMalformed = errors.New("sdlc: unblock condition could not be parsed")

	// ErrUnblockKindUnknown indicates a condition naming a kind the domain does
	// not define.
	ErrUnblockKindUnknown = errors.New("sdlc: unknown unblock condition kind")

	// ErrUnblockKindUnhandled indicates a kind the vocabulary DEFINES and
	// nothing decides — neither a check nor a named person.
	//
	// Distinct from unknown because the two name different defects: unknown
	// is a record naming a kind that was never written, unhandled is a kind
	// somebody started writing and did not finish. Folding them together
	// sends whoever reads the finding to the record instead of to the code.
	ErrUnblockKindUnhandled = errors.New("sdlc: unblock condition kind has no decider")

	// ErrUnblockNotCheckable indicates a condition only a person can clear.
	//
	// It is an ERROR rather than an unsatisfied condition so a caller cannot
	// receive it as an ordinary "not yet" and wait forever on something no
	// check will ever clear.
	ErrUnblockNotCheckable = errors.New("sdlc: unblock condition is decided by a person, not by a check")
)

// UnblockConditionError reports a condition that could not be read.
//
// It carries the raw reason text, because diagnosing a malformed marker means
// seeing what was actually stored — and a triage report that said only "could
// not parse" would send an operator to the record anyway.
type UnblockConditionError struct {
	// TaskID is the task whose reason carries the condition, when the caller
	// knew it. Parsing is a pure function of the text, so it does not — the
	// FINDING names the task, and this stays empty there rather than
	// carrying a placeholder that reads like a record id.
	TaskID string

	// Reason is the stored text, quoted verbatim.
	Reason string

	// Detail names what was wrong with it.
	Detail string

	// Sentinel is what this unwraps to — absent or malformed. Held rather than
	// derived so one type serves both without a second type that differs only
	// in its Unwrap.
	Sentinel error
}

// Error implements the error interface.
func (e *UnblockConditionError) Error() string {
	msg := "sdlc: "
	if e.TaskID != "" {
		msg += "task " + e.TaskID + ": "
	}
	msg += e.Detail
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}

// Unwrap returns the sentinel for errors.Is support.
func (e *UnblockConditionError) Unwrap() error { return e.Sentinel }

// UnblockKindError reports a condition kind the vocabulary cannot use: one
// it does not define, or one it defines and nothing decides.
type UnblockKindError struct {
	// Kind is the kind named by the condition.
	Kind string

	// Sentinel is the error class the caller matches on — unknown or
	// unhandled. Held rather than derived so one type serves both without a
	// second type that differs only in its Unwrap.
	Sentinel error
}

// Error implements the error interface.
//
// The two classes render DIFFERENTLY on purpose. Both leave the task blocked
// and they differ in who has to act: an unknown kind is a bad record, an
// unhandled one is an unfinished kind in this package, and one shared
// sentence would send an operator to the wrong one.
func (e *UnblockKindError) Error() string {
	if errors.Is(e.Sentinel, ErrUnblockKindUnhandled) {
		return "sdlc: unblock condition kind " + e.Kind + " is defined but nothing decides it"
	}
	return "sdlc: unblock condition names unknown kind " + e.Kind
}

// Unwrap returns the sentinel for errors.Is support.
func (e *UnblockKindError) Unwrap() error { return e.Sentinel }

// UnblockNotCheckableError reports a condition a person clears rather than a
// check, and names who.
//
// Naming the party is the whole point: "waiting on the owner for a ruling"
// and "waiting on an operator to repair the fleet" summon different people,
// and a caller told only that the condition is unsatisfied summons neither.
type UnblockNotCheckableError struct {
	// Kind is the condition's kind.
	Kind UnblockKind

	// Awaiting names the party who clears it.
	Awaiting string
}

// Error implements the error interface.
func (e *UnblockNotCheckableError) Error() string {
	return "sdlc: an unblock condition of kind " + string(e.Kind) +
		" is cleared by " + e.Awaiting + ", not by a check"
}

// Unwrap returns the sentinel for errors.Is support.
func (e *UnblockNotCheckableError) Unwrap() error { return ErrUnblockNotCheckable }
