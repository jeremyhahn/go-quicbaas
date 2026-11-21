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

package gitsign

import (
	"errors"
	"testing"
)

func TestParseInvocationAcceptsWhatGitPasses(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want Invocation
	}{
		{
			name: "literal key with -U, as git 2.37+ runs it",
			args: []string{"-Y", "sign", "-n", "git", "-f", "/tmp/key.pub", "-U", "/tmp/buffer"},
			want: Invocation{Namespace: "git", KeyFile: "/tmp/key.pub", DataFile: "/tmp/buffer"},
		},
		{
			name: "without -U",
			args: []string{"-Y", "sign", "-n", "git", "-f", "/tmp/key.pub", "/tmp/buffer"},
			want: Invocation{Namespace: "git", KeyFile: "/tmp/key.pub", DataFile: "/tmp/buffer"},
		},
		{
			name: "data on stdin",
			args: []string{"-Y", "sign", "-n", "git", "-f", "/tmp/key.pub"},
			want: Invocation{Namespace: "git", KeyFile: "/tmp/key.pub"},
		},
		{
			name: "dash means stdin",
			args: []string{"-q", "-Y", "sign", "-f", "/tmp/key.pub", "-n", "git", "-"},
			want: Invocation{Namespace: "git", KeyFile: "/tmp/key.pub"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseInvocation(tc.args)
			if err != nil {
				t.Fatalf("ParseInvocation: %v", err)
			}
			if *got != tc.want {
				t.Fatalf("got %+v, want %+v", *got, tc.want)
			}
		})
	}
}

func TestParseInvocationRefusesMalformedCalls(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantArg string
	}{
		{name: "no arguments", args: nil},
		{name: "missing -Y", args: []string{"-n", "git", "-f", "k", "d"}},
		{name: "verify is not performed", args: []string{"-Y", "verify", "-n", "git", "-f", "k", "d"}, wantArg: "verify"},
		{name: "unknown verb", args: []string{"-Y", "bogus", "-n", "git", "-f", "k", "d"}, wantArg: "bogus"},
		{name: "missing -f", args: []string{"-Y", "sign", "-n", "git", "d"}},
		{name: "missing namespace", args: []string{"-Y", "sign", "-f", "k", "d"}},
		{name: "foreign namespace", args: []string{"-Y", "sign", "-n", "file", "-f", "k", "d"}, wantArg: "file"},
		{name: "unknown flag", args: []string{"-Y", "sign", "-n", "git", "-f", "k", "-O", "hashalg=sha256", "d"}, wantArg: "-O"},
		{name: "repeated flag", args: []string{"-Y", "sign", "-n", "git", "-f", "k", "-f", "other", "d"}, wantArg: "-f"},
		{name: "flag without value", args: []string{"-Y", "sign", "-n", "git", "-f"}, wantArg: "-f"},
		{name: "two data files", args: []string{"-Y", "sign", "-n", "git", "-f", "k", "d1", "d2"}, wantArg: "d2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseInvocation(tc.args)
			if got != nil {
				t.Fatalf("got invocation %+v, want none", *got)
			}
			var invalid *InvalidInvocationError
			if !errors.As(err, &invalid) {
				t.Fatalf("error %v is not an *InvalidInvocationError", err)
			}
			if !errors.Is(err, ErrInvalidInvocation) {
				t.Fatalf("error %v does not unwrap to ErrInvalidInvocation", err)
			}
			if invalid.Arg != tc.wantArg {
				t.Fatalf("Arg = %q, want %q", invalid.Arg, tc.wantArg)
			}
		})
	}
}
