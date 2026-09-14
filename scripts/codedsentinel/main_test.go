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

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// analyze runs the scanner over a set of in-memory files keyed by filename and
// returns the findings.
func analyze(t *testing.T, sources map[string]string) []finding {
	t.Helper()
	fset := token.NewFileSet()
	parsed := make(map[string]*ast.File, len(sources))
	for name, src := range sources {
		f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed[name] = f
	}
	sentinels := resolveSentinels(parsed)

	var out []finding
	for name, f := range parsed {
		out = append(out, scanFile(fset, name, f, sentinels)...)
	}
	return out
}

const sentinelDecls = `package iam

import dberrors "x/errors"

var ErrUserNotFound = &dberrors.QBError{Code: dberrors.ErrNotFound}
var ErrPlain = New("plain sentinel")
var ErrAliased = ErrUserNotFound
`

// TestScannerFindsACodedSentinelTarget is the case the guard exists for.
func TestScannerFindsACodedSentinelTarget(t *testing.T) {
	found := analyze(t, map[string]string{
		"iam/errors.go": sentinelDecls,
		"rest/h.go": `package rest

import (
	"errors"

	"x/iam"
)

func h(err error) bool { return errors.Is(err, iam.ErrUserNotFound) }
`,
	})
	if len(found) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(found), found)
	}
	if found[0].target != "iam.ErrUserNotFound" {
		t.Fatalf("target = %q, want iam.ErrUserNotFound", found[0].target)
	}
}

// TestScannerFollowsSentinelAliases proves `var A = B` does not hide a coded
// sentinel behind one indirection.
func TestScannerFollowsSentinelAliases(t *testing.T) {
	found := analyze(t, map[string]string{
		"iam/errors.go": sentinelDecls,
		"rest/h.go": `package rest

import (
	"errors"

	"x/iam"
)

func h(err error) bool { return errors.Is(err, iam.ErrAliased) }
`,
	})
	if len(found) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(found), found)
	}
}

// TestScannerIgnoresPlainSentinels proves the guard does not fire on an
// errors.New sentinel, where errors.Is means exactly what it reads.
func TestScannerIgnoresPlainSentinels(t *testing.T) {
	found := analyze(t, map[string]string{
		"iam/errors.go": sentinelDecls,
		"rest/h.go": `package rest

import (
	"errors"

	"x/iam"
)

func h(err error) bool { return errors.Is(err, iam.ErrPlain) }
`,
	})
	if len(found) != 0 {
		t.Fatalf("findings = %d, want 0: %+v", len(found), found)
	}
}

// TestScannerHonoursAReasonedWaiver proves a site that means the code class can
// say so.
func TestScannerHonoursAReasonedWaiver(t *testing.T) {
	found := analyze(t, map[string]string{
		"iam/errors.go": sentinelDecls,
		"rest/h.go": `package rest

import (
	"errors"

	"x/iam"
)

func h(err error) bool {
	// coded-sentinel-ok: any not-found is a 404 here.
	return errors.Is(err, iam.ErrUserNotFound)
}
`,
	})
	if len(found) != 0 {
		t.Fatalf("findings = %d, want 0: %+v", len(found), found)
	}
}

// TestScannerRejectsAnEmptyWaiver proves the waiver has to carry a reason. A
// bare marker would let the next person silence the guard without stating why,
// which is the failure mode a waiver mechanism has.
func TestScannerRejectsAnEmptyWaiver(t *testing.T) {
	found := analyze(t, map[string]string{
		"iam/errors.go": sentinelDecls,
		"rest/h.go": `package rest

import (
	"errors"

	"x/iam"
)

func h(err error) bool {
	// coded-sentinel-ok:
	return errors.Is(err, iam.ErrUserNotFound)
}
`,
	})
	if len(found) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(found), found)
	}
}

// TestScannerDoesNotWaiveTheWholeFunction proves a waiver's reach is bounded,
// so one justified site does not silence an unrelated one further down.
func TestScannerDoesNotWaiveTheWholeFunction(t *testing.T) {
	found := analyze(t, map[string]string{
		"iam/errors.go": sentinelDecls,
		"rest/h.go": `package rest

import (
	"errors"

	"x/iam"
)

func h(err error) bool {
	// coded-sentinel-ok: the first one is a 404.
	if errors.Is(err, iam.ErrUserNotFound) {
		return true
	}
	//
	//
	//
	//
	//
	//
	//
	return errors.Is(err, iam.ErrUserNotFound)
}
`,
	})
	if len(found) != 1 {
		t.Fatalf("findings = %d, want 1 (the unwaived second site): %+v", len(found), found)
	}
}

// TestScannerFindsTestifyAssertions proves the -include-tests audit mode sees
// assert.ErrorIs, whose target argument sits one position later.
func TestScannerFindsTestifyAssertions(t *testing.T) {
	found := analyze(t, map[string]string{
		"iam/errors.go": sentinelDecls,
		"rest/h_test.go": `package rest

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"x/iam"
)

func TestX(t *testing.T) {
	var err error
	assert.ErrorIs(t, err, iam.ErrUserNotFound)
	assert.NotErrorIs(t, err, iam.ErrPlain)
}
`,
	})
	if len(found) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(found), found)
	}
}

// TestResolveSentinelsIgnoresNonErrorComposites proves the syntactic type match
// is not "any composite literal": only QBError/QRDBError count.
func TestResolveSentinelsIgnoresNonErrorComposites(t *testing.T) {
	found := analyze(t, map[string]string{
		"iam/errors.go": `package iam

var NotAnError = &Config{Name: "x"}
`,
		"rest/h.go": `package rest

import (
	"errors"

	"x/iam"
)

func h(err error) bool { return errors.Is(err, iam.NotAnError) }
`,
	})
	if len(found) != 0 {
		t.Fatalf("findings = %d, want 0: %+v", len(found), found)
	}
}
