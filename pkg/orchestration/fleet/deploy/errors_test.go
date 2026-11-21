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

package deploy

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// TestErrorFormatting verifies each typed error renders a useful
// message and preserves the underlying cause via errors.Is/As.
func TestErrorFormatting(t *testing.T) {
	cause := io.ErrUnexpectedEOF

	cases := []struct {
		name string
		err  error
		want []string
	}{
		{
			name: "SSHDialError",
			err:  &SSHDialError{Host: "h:22", User: "u", Cause: cause},
			want: []string{"ssh dial failed", "h:22", "u", cause.Error()},
		},
		{
			name: "SFTPUploadError",
			err:  &SFTPUploadError{LocalPath: "/a", RemotePath: "/b", Cause: cause},
			want: []string{"sftp upload failed", "/a", "/b", cause.Error()},
		},
		{
			name: "RemoteExecError",
			err:  &RemoteExecError{Command: "x", Stderr: "boom", Cause: cause},
			want: []string{"remote command failed", "x", "boom", cause.Error()},
		},
		{
			name: "SystemdInstallError",
			err:  &SystemdInstallError{Unit: "u.service", Stage: "start", Cause: cause},
			want: []string{"systemd install failed", "start", "u.service", cause.Error()},
		},
		{
			name: "LingerNotEnabledError",
			err:  &LingerNotEnabledError{User: "operator", Command: "sudo loginctl enable-linger operator", Cause: cause},
			want: []string{"lingering is not enabled", "operator", "ssh session closes", "sudo loginctl enable-linger operator", cause.Error()},
		},
		{
			name: "FleetNodeUnreachableError",
			err:  &FleetNodeUnreachableError{Addr: "10.0.0.1:9091", Cause: cause},
			want: []string{"runtime port is not reachable", "10.0.0.1:9091", cause.Error()},
		},
		{
			name: "StartTimeoutError",
			err:  &StartTimeoutError{Host: "h", Tenant: "t", Elapsed: "10s", Cause: cause},
			want: []string{"did not appear online", "h", "t", "10s", cause.Error()},
		},
		{
			name: "InvalidSpecError",
			err:  &InvalidSpecError{Field: "x", Reason: "y"},
			want: []string{"invalid spec", "x", "y"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.err.Error()
			for _, w := range tc.want {
				if !strings.Contains(msg, w) {
					t.Fatalf("want substring %q in %q", w, msg)
				}
			}
		})
	}
}

// TestErrorUnwrap exercises Unwrap on every typed error that preserves
// a cause. errors.Is must see through the wrapper.
func TestErrorUnwrap(t *testing.T) {
	cause := io.ErrClosedPipe
	errs := []error{
		&SSHDialError{Cause: cause},
		&SFTPUploadError{Cause: cause},
		&RemoteExecError{Cause: cause},
		&SystemdInstallError{Cause: cause},
		&LingerNotEnabledError{Cause: cause},
		&FleetNodeUnreachableError{Cause: cause},
		&StartTimeoutError{Cause: cause},
	}
	for _, e := range errs {
		if !errors.Is(e, cause) {
			t.Fatalf("errors.Is failed for %T", e)
		}
	}
}

// TestErrorMinimal ensures the errors render sensibly when their
// optional fields are blank.
func TestErrorMinimal(t *testing.T) {
	errs := []error{
		&SSHDialError{},
		&SFTPUploadError{},
		&RemoteExecError{},
		&SystemdInstallError{},
		&LingerNotEnabledError{},
		&FleetNodeUnreachableError{},
		&StartTimeoutError{},
		&InvalidSpecError{},
	}
	for _, e := range errs {
		if e.Error() == "" {
			t.Fatalf("empty error message for %T", e)
		}
	}
}
