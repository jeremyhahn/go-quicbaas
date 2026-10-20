// Package nsswrapper gives a test's child processes a passwd entry for the
// running uid.
//
// The toolchain image runs tests as any non-root uid with HOME=/tmp, and that
// uid has no passwd entry (handbook devcontainer.md section 5). Go code does
// not need one, but the stock ssh-keygen does: it exits with "No user exists
// for uid N" before it reads its arguments. A test that proves interop with
// that binary -- directly, or through git's ssh signing and verify-commit --
// runs it under nss_wrapper, which answers the passwd and group lookups from
// files the test writes.
package nsswrapper

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// libraryPatterns are where the distributions install libnss_wrapper.so:
// Debian's multiarch directories first, then the flat layouts.
var libraryPatterns = []string{
	"/usr/lib/*-linux-gnu/libnss_wrapper.so",
	"/usr/lib64/libnss_wrapper.so",
	"/usr/lib/libnss_wrapper.so",
}

// lookupUID resolves a uid; tests replace it to stand in for a uid with no
// passwd entry.
var lookupUID = user.LookupId

// LibraryMissingError reports that the running uid has no passwd entry and
// libnss_wrapper is not installed to give it one.
type LibraryMissingError struct {
	UID      int
	Patterns []string
}

func (e *LibraryMissingError) Error() string {
	return fmt.Sprintf("uid %d has no passwd entry and libnss_wrapper.so is not installed (looked in %s): "+
		"install libnss-wrapper, as the toolchain image does", e.UID, strings.Join(e.Patterns, ", "))
}

// Env returns the variables a child process needs to resolve the running uid,
// for appending to its environment. It is empty when the uid already has a
// passwd entry. Otherwise it preloads libnss_wrapper with passwd and group
// files, written under a test temp dir, that name the uid with home as its
// home directory. A missing library fails the test: it never skips.
func Env(t testing.TB, home string) []string {
	t.Helper()
	env, err := environment(os.Getuid(), os.Getgid(), home, t.TempDir())
	if err != nil {
		t.Fatalf("nsswrapper: %v", err)
	}
	return env
}

// environment builds Env's variables for uid and gid, writing the wrapper's
// files into dir.
func environment(uid, gid int, home, dir string) ([]string, error) {
	if _, err := lookupUID(strconv.Itoa(uid)); err == nil {
		return nil, nil
	}
	library, err := findLibrary(uid)
	if err != nil {
		return nil, err
	}

	name := "uid" + strconv.Itoa(uid)
	passwd := filepath.Join(dir, "passwd")
	entry := fmt.Sprintf("%s:x:%d:%d:%s:%s:/bin/sh\n", name, uid, gid, name, home)
	if err := os.WriteFile(passwd, []byte(entry), 0o600); err != nil {
		return nil, err
	}
	group := filepath.Join(dir, "group")
	if err := os.WriteFile(group, []byte(fmt.Sprintf("%s:x:%d:\n", name, gid)), 0o600); err != nil {
		return nil, err
	}
	return []string{
		"LD_PRELOAD=" + library,
		"NSS_WRAPPER_PASSWD=" + passwd,
		"NSS_WRAPPER_GROUP=" + group,
	}, nil
}

// findLibrary returns the first installed libnss_wrapper.so.
func findLibrary(uid int) (string, error) {
	for _, pattern := range libraryPatterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return "", err
		}
		if len(matches) > 0 {
			return matches[0], nil
		}
	}
	return "", &LibraryMissingError{UID: uid, Patterns: libraryPatterns}
}
