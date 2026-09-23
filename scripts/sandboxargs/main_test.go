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
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

const productionCapFlags = "--cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER " +
	"--cap-add SETGID --cap-add SETUID --cap-drop ALL"

func TestRun_PrintsTheProductionFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	want := "--pids-limit " + strconv.FormatInt(orchestration.DefaultSandboxPidsLimit, 10) + " " + productionCapFlags
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("flags = %q, want %q", got, want)
	}
}

func TestRun_PidsLimitOverridesOnlyTheBound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-pids-limit", "64"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d, stderr %q", code, stderr.String())
	}
	if got, want := strings.TrimSpace(stdout.String()), "--pids-limit 64 "+productionCapFlags; got != want {
		t.Fatalf("flags = %q, want %q", got, want)
	}
}

func TestRun_RefusesABadInvocation(t *testing.T) {
	for name, args := range map[string][]string{
		"NegativeLimit":  {"-pids-limit", "-1"},
		"UnknownFlag":    {"-privileged"},
		"StrayArguments": {"extra"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("a refused invocation printed flags: %q", stdout.String())
			}
		})
	}
}
