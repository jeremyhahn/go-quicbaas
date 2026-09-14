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

package certfixture

import (
	"errors"
	"strings"
	"testing"
)

// cause is the sentinel each typed error is asked to carry and surrender.
var cause = errors.New("underlying failure")

// Every typed error names what failed and where, and unwraps to its cause.
//
// The message content is asserted rather than just the type: these errors
// surface in a container's compose log, where the only debugging material is
// the line itself, so "which fixture, which path, which step" has to be in it.
func TestTypedErrors_NameTheFailureAndUnwrap(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		wants []string
	}{
		{
			name:  "issue",
			err:   &IssueError{CommonName: "test-node1", Cause: cause},
			wants: []string{"issue", "test-node1", cause.Error()},
		},
		{
			name:  "authority with a directory",
			err:   &AuthorityError{Op: "open ca", Dir: "/certs/ca-keystore", Cause: cause},
			wants: []string{"open ca", "/certs/ca-keystore", cause.Error()},
		},
		{
			name:  "authority without a directory",
			err:   &AuthorityError{Op: "ca certificate", Cause: cause},
			wants: []string{"ca certificate", cause.Error()},
		},
		{
			name:  "file",
			err:   &FileError{Op: "write", Path: "/certs/node1.key", Cause: cause},
			wants: []string{"write", "/certs/node1.key", cause.Error()},
		},
		{
			name:  "decode with a cause",
			err:   &DecodeError{What: "private key", Cause: cause},
			wants: []string{"private key", cause.Error()},
		},
		{
			name:  "decode with no PEM at all",
			err:   &DecodeError{What: "certificate"},
			wants: []string{"certificate", "no PEM block"},
		},
		{
			name:  "invalid ip",
			err:   &InvalidIPError{Address: "127.0.0.300"},
			wants: []string{"127.0.0.300"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			if !strings.HasPrefix(msg, "certfixture: ") {
				t.Errorf("message %q is not attributed to this package", msg)
			}
			for _, want := range tc.wants {
				if !strings.Contains(msg, want) {
					t.Errorf("message %q is missing %q", msg, want)
				}
			}
		})
	}
}

// The wrapping errors surrender their cause so a caller can match on it; the
// ones that carry no cause report nothing to unwrap.
func TestTypedErrors_Unwrap(t *testing.T) {
	wrapping := []error{
		&IssueError{CommonName: "x", Cause: cause},
		&AuthorityError{Op: "open ca", Cause: cause},
		&FileError{Op: "write", Path: "/x", Cause: cause},
		&DecodeError{What: "certificate", Cause: cause},
	}
	for _, err := range wrapping {
		if !errors.Is(err, cause) {
			t.Errorf("%T did not unwrap to its cause", err)
		}
	}

	if errors.Unwrap(&DecodeError{What: "certificate"}) != nil {
		t.Error("a DecodeError with no cause unwrapped to something")
	}
	if errors.Unwrap(&InvalidIPError{Address: "x"}) != nil {
		t.Error("InvalidIPError unwrapped to something")
	}
}

// A sentinel a caller can act on keeps its identity through the wrappers the
// package builds around it — the CA-is-not-up and no-private-key conditions are
// both reported from inside issue().
func TestSentinels_SurviveWrapping(t *testing.T) {
	err := error(&IssueError{CommonName: "admin", Cause: ErrNoPrivateKey})
	if !errors.Is(err, ErrNoPrivateKey) {
		t.Error("ErrNoPrivateKey did not survive an IssueError")
	}
	err = &FileError{Op: "validate", Path: "topology.json", Cause: ErrNoTopologyNodes}
	if !errors.Is(err, ErrNoTopologyNodes) {
		t.Error("ErrNoTopologyNodes did not survive a FileError")
	}
}
