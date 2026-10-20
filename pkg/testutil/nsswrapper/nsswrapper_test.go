package nsswrapper

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

// stubLookup makes every uid resolve, or none, for one test.
func stubLookup(t *testing.T, resolves bool) {
	t.Helper()
	original := lookupUID
	lookupUID = func(uid string) (*user.User, error) {
		if resolves {
			return &user.User{Uid: uid, Username: "builder"}, nil
		}
		return nil, user.UnknownUserIdError(10001)
	}
	t.Cleanup(func() { lookupUID = original })
}

// stubLibrary points the library search at patterns for one test.
func stubLibrary(t *testing.T, patterns ...string) {
	t.Helper()
	original := libraryPatterns
	libraryPatterns = patterns
	t.Cleanup(func() { libraryPatterns = original })
}

func TestEnvironment_UidWithAPasswdEntryNeedsNothing(t *testing.T) {
	stubLookup(t, true)
	stubLibrary(t, filepath.Join(t.TempDir(), "absent.so"))

	env, err := environment(10001, 10001, "/tmp", t.TempDir())
	if err != nil || env != nil {
		t.Fatalf("environment = %v, %v; want nothing for a uid that resolves", env, err)
	}
}

func TestEnvironment_UidWithoutAPasswdEntryIsWrapped(t *testing.T) {
	stubLookup(t, false)
	library := filepath.Join(t.TempDir(), "libnss_wrapper.so")
	if err := os.WriteFile(library, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stubLibrary(t, filepath.Join(t.TempDir(), "absent.so"), library)
	dir := t.TempDir()

	env, err := environment(10001, 10002, "/tmp", dir)
	if err != nil {
		t.Fatalf("environment: %v", err)
	}

	passwd, group := filepath.Join(dir, "passwd"), filepath.Join(dir, "group")
	want := []string{"LD_PRELOAD=" + library, "NSS_WRAPPER_PASSWD=" + passwd, "NSS_WRAPPER_GROUP=" + group}
	if len(env) != len(want) {
		t.Fatalf("env = %v, want %v", env, want)
	}
	for i := range want {
		if env[i] != want[i] {
			t.Fatalf("env = %v, want %v", env, want)
		}
	}
	for path, content := range map[string]string{
		passwd: "uid10001:x:10001:10002:uid10001:/tmp:/bin/sh\n",
		group:  "uid10001:x:10002:\n",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != content {
			t.Fatalf("%s = %q, %v; want %q", path, got, err, content)
		}
	}
}

func TestEnvironment_MissingLibraryIsATypedError(t *testing.T) {
	stubLookup(t, false)
	stubLibrary(t, filepath.Join(t.TempDir(), "absent.so"))

	_, err := environment(10001, 10001, "/tmp", t.TempDir())

	var missing *LibraryMissingError
	if !errors.As(err, &missing) || missing.UID != 10001 {
		t.Fatalf("want LibraryMissingError for uid 10001, got %v", err)
	}
}

func TestEnvironment_UnwritableDirIsAnError(t *testing.T) {
	stubLookup(t, false)
	library := filepath.Join(t.TempDir(), "libnss_wrapper.so")
	if err := os.WriteFile(library, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stubLibrary(t, library)

	_, err := environment(10001, 10001, "/tmp", filepath.Join(t.TempDir(), "absent"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want a not-exist error writing the passwd file, got %v", err)
	}
}

func TestFindLibrary_BadPatternIsAnError(t *testing.T) {
	stubLibrary(t, "[")

	if _, err := findLibrary(10001); !errors.Is(err, filepath.ErrBadPattern) {
		t.Fatalf("want ErrBadPattern, got %v", err)
	}
}

func TestEnv_RunningUidResolvesHere(t *testing.T) {
	stubLookup(t, true)

	if env := Env(t, t.TempDir()); env != nil {
		t.Fatalf("Env = %v, want nothing for a uid that resolves", env)
	}
}
