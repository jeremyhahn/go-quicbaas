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

package buildgates

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The reaper (scripts/reap-stale-ci-stacks.sh) removes integration images
// from a Docker daemon that concurrent CI jobs share, and ci.yml's lease step
// holds an image against it. These tests run both, unchanged, against a
// stand-in docker that keeps the daemon's state in a temp directory, so no
// daemon is touched.

const (
	testPrefix = "qbtest-"
	testImage  = testPrefix + "it:abc"
	testLock   = testPrefix + "image-lock"
	reaperPath = repoRoot + "/scripts/reap-stale-ci-stacks.sh"
)

// fakeDockerScript answers the docker calls the reaper and the lease step
// make from the state directory $FAKE_DOCKER, and appends each call to
// $FAKE_DOCKER/calls. tags/<ref> holds a tag's image id, images/<id> an
// image's created and last-tag times, ctrs/<id> a container's
// "name image state created finished". A file under fail/ makes one kind of
// call fail. When hook-on holds a glob that a call matches, the hook script
// runs once, before the call. Like the daemon, `image rm` of the last tag of
// an image a container uses fails, and of any other tag only untags.
const fakeDockerScript = `#!/usr/bin/env bash
set -u
s=$FAKE_DOCKER
printf '%s\n' "$*" >>"$s/calls"
if [ -f "$s/hook-on" ]; then
  pattern=$(<"$s/hook-on")
  # shellcheck disable=SC2254 # the hook pattern is a glob
  case "$*" in $pattern) rm -f "$s/hook-on"; "$s/hook" >>"$s/hook.out" 2>&1 ;; esac
fi

next_id() {
  local n=0
  [ ! -f "$s/n" ] || n=$(<"$s/n")
  n=$((n + 1))
  echo "$n" >"$s/n"
  echo "$1$n"
}
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
container() {
  local c name rest
  if [ -f "$s/ctrs/$1" ]; then echo "$1"; return 0; fi
  for c in "$s"/ctrs/*; do
    [ -f "$c" ] || continue
    read -r name rest <"$c"
    if [ "$name" = "$1" ]; then echo "${c##*/}"; return 0; fi
  done
  echo "Error: No such container: $1" >&2
  return 1
}
image() {
  if [ -f "$s/tags/$1" ]; then echo "$(<"$s/tags/$1")"; return 0; fi
  if [ -d "$s/images/$1" ]; then echo "$1"; return 0; fi
  echo "Error: No such image: $1" >&2
  return 1
}
users_of() {
  local c id
  for c in "$s"/ctrs/*; do
    [ -f "$c" ] || continue
    read -r _ id _ <"$c"
    [ "$id" != "$1" ] || echo "${c##*/}"
  done
}
tag_new_image() {
  local id
  id=$(next_id sha256:built)
  mkdir -p "$s/images/$id"
  now >"$s/images/$id/created"
  date -u '+%Y-%m-%d %H:%M:%S.000000000 +0000 UTC' >"$s/images/$id/tagged"
  echo "$id" >"$s/tags/$1"
}
filter() {
  while [ $# -gt 1 ]; do
    if [ "$1" = --filter ]; then echo "$2"; return; fi
    shift
  done
}

case "$1" in
image)
  case "$2" in
  inspect)
    shift 2
    format=
    if [ "$1" = -f ]; then format=$2; shift 2; fi
    if [ -e "$s/fail/image-inspect" ]; then echo "template: can't evaluate field" >&2; exit 1; fi
    id=$(image "$1") || exit 1
    case $format in
    '') echo "[{\"Id\": \"$id\"}]" ;;
    '{{.Id}}') echo "$id" ;;
    '{{.Id}} {{.Created}}') echo "$id $(<"$s/images/$id/created")" ;;
    '{{.Metadata.LastTagTime}}') cat "$s/images/$id/tagged" ;;
    *) echo "fake docker: image inspect format $format" >&2; exit 1 ;;
    esac ;;
  rm)
    ref=$3
    id=$(image "$ref") || exit 1
    others=0
    for t in "$s"/tags/*; do
      if [ -f "$t" ] && [ "${t##*/}" != "$ref" ] && [ "$(<"$t")" = "$id" ]; then others=$((others + 1)); fi
    done
    if [ "$others" = 0 ] && [ -n "$(users_of "$id")" ]; then
      echo "Error response from daemon: conflict: image is being used by a container" >&2
      exit 1
    fi
    rm -f "$s/tags/$ref"
    [ "$others" != 0 ] || rm -rf "$s/images/$id"
    echo "Untagged: $ref" ;;
  *) echo "fake docker: unhandled $*" >&2; exit 1 ;;
  esac ;;
images)
  repo=$(filter "$@")
  repo=${repo#reference=}
  for t in "$s"/tags/*; do
    [ -f "$t" ] || continue
    case ${t##*/} in "$repo":*) echo "${t##*/}" ;; esac
  done ;;
import)
  cat >/dev/null
  tag_new_image "$3" ;;
create)
  name=$5 ref=$6
  if container "$name" >/dev/null 2>&1; then
    echo "Error response from daemon: Conflict. The container name \"/$name\" is already in use" >&2
    exit 1
  fi
  id=$(image "$ref") || exit 1
  cid=$(next_id cid)
  echo "$name $id created $(now) 0001-01-01T00:00:00Z" >"$s/ctrs/$cid"
  echo "$cid" ;;
inspect)
  cid=$(container "$4") || exit 1
  read -r _ _ state created finished <"$s/ctrs/$cid"
  case $3 in
  '{{.Id}} {{.Created}}') echo "$cid $created" ;;
  '{{.State.Status}} {{.State.FinishedAt}} {{.Created}}') echo "$state $finished $created" ;;
  *) echo "fake docker: inspect format $3" >&2; exit 1 ;;
  esac ;;
rm)
  shift
  rc=0
  for c; do
    [ "$c" != -v ] || continue
    if cid=$(container "$c"); then rm -f "$s/ctrs/$cid"; else rc=1; fi
  done
  exit "$rc" ;;
ps)
  f=$(filter "$@")
  case $f in
  ancestor=*)
    if [ -e "$s/fail/ancestor" ]; then echo "Cannot connect to the Docker daemon" >&2; exit 1; fi
    users_of "${f#ancestor=}" ;;
  name=^*)
    for c in "$s"/ctrs/*; do
      [ -f "$c" ] || continue
      read -r name _ <"$c"
      case $name in "${f#name=^}"*) echo "${c##*/} $name " ;; esac
    done ;;
  esac ;;
network | volume) ;;
_build) tag_new_image "$2" ;;
*) echo "fake docker: unhandled $*" >&2; exit 1 ;;
esac
`

// fakeMakeScript stands in for `make docker-build`: it tags a new image as
// IT_IMAGE, or under fail/build fails, or under fail/build-empty builds
// nothing and succeeds.
const fakeMakeScript = `#!/usr/bin/env bash
printf 'make %s\n' "$*" >>"$FAKE_DOCKER/calls"
[ ! -e "$FAKE_DOCKER/fail/build" ] || exit 2
[ ! -e "$FAKE_DOCKER/fail/build-empty" ] || exit 0
for a; do
  case $a in IT_IMAGE=*) docker _build "${a#IT_IMAGE=}" ;; esac
done
`

// fakeSleepScript logs the lock waits and returns at once.
const fakeSleepScript = `#!/usr/bin/env bash
printf 'sleep %s\n' "$*" >>"$FAKE_DOCKER/calls"
`

// fakeDocker is one stand-in daemon: bin holds the docker, make and sleep
// stand-ins, state the daemon's state.
type fakeDocker struct {
	t     *testing.T
	bin   string
	state string
}

func newFakeDocker(t *testing.T) *fakeDocker {
	t.Helper()
	root := t.TempDir()
	f := &fakeDocker{t: t, bin: filepath.Join(root, "bin"), state: filepath.Join(root, "state")}
	for _, dir := range []string{f.bin, f.path("tags"), f.path("images"), f.path("ctrs"), f.path("fail")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	scripts := map[string]string{"docker": fakeDockerScript, "make": fakeMakeScript, "sleep": fakeSleepScript}
	for name, body := range scripts {
		f.write(filepath.Join(f.bin, name), body, 0o755)
	}
	return f
}

func (f *fakeDocker) path(parts ...string) string {
	return filepath.Join(append([]string{f.state}, parts...)...)
}

func (f *fakeDocker) write(path, body string, mode os.FileMode) {
	f.t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		f.t.Fatal(err)
	}
}

// image tags ref onto image id, created and last tagged the given times ago.
func (f *fakeDocker) image(ref, id string, created, tagged time.Duration) {
	f.t.Helper()
	dir := f.path("images", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	now := time.Now().UTC()
	f.write(filepath.Join(dir, "created"), now.Add(-created).Format(time.RFC3339)+"\n", 0o644)
	// The daemon prints LastTagTime as Go's time.String does.
	f.write(filepath.Join(dir, "tagged"), now.Add(-tagged).Format("2006-01-02 15:04:05.000000000 -0700 MST")+"\n", 0o644)
	f.write(f.path("tags", ref), id+"\n", 0o644)
}

// container records a created, never-started container on image id, created
// age ago, as a lease or a lock is.
func (f *fakeDocker) container(name, id string, age time.Duration) {
	f.t.Helper()
	created := time.Now().UTC().Add(-age).Format(time.RFC3339)
	f.write(f.path("ctrs", "c-"+name), fmt.Sprintf("%s %s created %s 0001-01-01T00:00:00Z\n", name, id, created), 0o644)
}

// fail makes one kind of call fail: image-inspect, ancestor, build or
// build-empty (see the stand-in scripts).
func (f *fakeDocker) fail(what string) {
	f.write(f.path("fail", what), "", 0o644)
}

// onCall runs script once, before the first docker call matching the glob.
func (f *fakeDocker) onCall(glob, script string) {
	f.write(f.path("hook-on"), glob, 0o644)
	f.write(f.path("hook"), "#!/usr/bin/env bash\n"+script, 0o755)
}

func (f *fakeDocker) env(extra ...string) []string {
	env := append(os.Environ(), "PATH="+f.bin+":"+os.Getenv("PATH"), "FAKE_DOCKER="+f.state,
		"REAP_DRY_RUN=0", "REAP_IMAGE_MIN_AGE=", "REAP_LOCK_WAIT=")
	return append(env, extra...)
}

// calls returns every docker, make and sleep call, in order.
func (f *fakeDocker) calls() []string {
	f.t.Helper()
	raw, err := os.ReadFile(f.path("calls"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		f.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// callIndex is the index of the first call starting with prefix, or -1.
func (f *fakeDocker) callIndex(prefix string) int {
	for i, call := range f.calls() {
		if strings.HasPrefix(call, prefix) {
			return i
		}
	}
	return -1
}

func (f *fakeDocker) callCount(prefix string) int {
	n := 0
	for _, call := range f.calls() {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}

// tag returns the image id ref names, or "" when the ref is gone.
func (f *fakeDocker) tag(ref string) string {
	raw, err := os.ReadFile(f.path("tags", ref))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// containers maps each container's name to its image id.
func (f *fakeDocker) containers() map[string]string {
	f.t.Helper()
	entries, err := os.ReadDir(f.path("ctrs"))
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]string{}
	for _, entry := range entries {
		raw, err := os.ReadFile(f.path("ctrs", entry.Name()))
		if err != nil {
			f.t.Fatal(err)
		}
		fields := strings.Fields(string(raw))
		out[fields[0]] = fields[1]
	}
	return out
}

// runReaper runs the reaper for testPrefix; it always exits 0.
func runReaper(t *testing.T, f *fakeDocker, env ...string) string {
	t.Helper()
	cmd := exec.Command("bash", reaperPath, testPrefix)
	cmd.Env = f.env(env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("reaper: %v\n%s", err, out)
	}
	return string(out)
}

// assertLockReleased fails when the image lock is left behind.
func assertLockReleased(t *testing.T, f *fakeDocker) {
	t.Helper()
	if _, held := f.containers()[testLock]; held {
		t.Errorf("the image lock %s is still held after the run", testLock)
	}
}

func TestReaperKeepsEveryTagOfALeasedImage(t *testing.T) {
	f := newFakeDocker(t)
	// Two trees the build sees as one share an image id under two tags.
	f.image(testPrefix+"it:one", "sha256:shared", 3*time.Hour, 3*time.Hour)
	f.image(testPrefix+"it:two", "sha256:shared", 3*time.Hour, 3*time.Hour)
	f.container(testPrefix+"lease-7-1-ab", "sha256:shared", 10*time.Minute)

	out := runReaper(t, f)

	for _, ref := range []string{testPrefix + "it:one", testPrefix + "it:two"} {
		if f.tag(ref) != "sha256:shared" {
			t.Errorf("the reaper removed %s, a tag of a leased image id\n%s", ref, out)
		}
	}
	if n := f.callCount("image rm"); n != 0 {
		t.Errorf("the reaper ran image rm %d time(s) on a leased image\n%s", n, out)
	}
	if _, ok := f.containers()[testPrefix+"lease-7-1-ab"]; !ok {
		t.Errorf("the reaper removed a live lease\n%s", out)
	}
	assertLockReleased(t, f)
}

func TestReaperKeepsAnImageItCannotLookUp(t *testing.T) {
	cases := map[string]struct {
		fail string
		want string
	}{
		"image inspect fails":     {fail: "image-inspect", want: "keep  image " + testImage + " (docker image inspect gave no id)"},
		"container listing fails": {fail: "ancestor", want: "keep  image " + testImage + " (could not list the containers"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeDocker(t)
			f.image(testImage, "sha256:old", 3*time.Hour, 3*time.Hour)
			f.fail(tc.fail)

			out := runReaper(t, f)

			if f.tag(testImage) == "" {
				t.Errorf("the reaper removed %s after a failed lookup\n%s", testImage, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the reaper did not report %q\n%s", tc.want, out)
			}
			assertLockReleased(t, f)
		})
	}
}

func TestReaperRemovesOnlyStaleUnleasedImages(t *testing.T) {
	f := newFakeDocker(t)
	f.image(testPrefix+"it:stale", "sha256:stale", 3*time.Hour, 3*time.Hour)
	// A rebuild from a full cache keeps the old Created and re-tags (v3).
	f.image(testPrefix+"it:retagged", "sha256:retagged", 3*time.Hour, 10*time.Minute)
	f.image(testPrefix+"it:fresh", "sha256:fresh", 10*time.Minute, 10*time.Minute)
	f.image("other-it:abc", "sha256:other", 3*time.Hour, 3*time.Hour)

	out := runReaper(t, f)

	if f.tag(testPrefix+"it:stale") != "" {
		t.Errorf("the reaper kept a stale, unleased image\n%s", out)
	}
	for _, ref := range []string{testPrefix + "it:retagged", testPrefix + "it:fresh", "other-it:abc"} {
		if f.tag(ref) == "" {
			t.Errorf("the reaper removed %s\n%s", ref, out)
		}
	}
	if !strings.Contains(out, "1 image(s) removed") {
		t.Errorf("the reaper did not count one removal\n%s", out)
	}
	// The removal runs under the lock: taken before the listing, dropped after.
	lock := f.callIndex("create --pull never --name " + testLock)
	list := f.callIndex("images --filter reference=" + testPrefix + "it")
	remove := f.callIndex("image rm " + testPrefix + "it:stale")
	unlock := f.callIndex("rm cid")
	if lock < 0 || !(lock < list && list < remove && remove < unlock) {
		t.Errorf("lock %d, list %d, image rm %d, unlock %d: the removal is not inside the image lock\n%s",
			lock, list, remove, unlock, strings.Join(f.calls(), "\n"))
	}
	assertLockReleased(t, f)
}

func TestReaperRemovesNoImageWithoutTheLock(t *testing.T) {
	t.Run("a live holder keeps every image", func(t *testing.T) {
		f := newFakeDocker(t)
		f.image(testLock+":base", "sha256:base", time.Hour, time.Hour)
		f.container(testLock, "sha256:base", time.Minute)
		f.image(testImage, "sha256:old", 3*time.Hour, 3*time.Hour)

		out := runReaper(t, f, "REAP_LOCK_WAIT=3")

		if f.tag(testImage) == "" {
			t.Errorf("the reaper removed an image while another job held the lock\n%s", out)
		}
		if !strings.Contains(out, "keep  every "+testPrefix+"it image") {
			t.Errorf("the reaper did not report the held lock\n%s", out)
		}
		if f.callIndex("images ") >= 0 {
			t.Errorf("the reaper listed images without the lock\n%s", strings.Join(f.calls(), "\n"))
		}
		if n := f.callCount("sleep "); n != 3 {
			t.Errorf("the reaper waited %d time(s) for the lock, want REAP_LOCK_WAIT=3", n)
		}
		if _, held := f.containers()[testLock]; !held {
			t.Errorf("the reaper removed another job's live lock\n%s", out)
		}
	})
	t.Run("a dead holder's lock is broken", func(t *testing.T) {
		f := newFakeDocker(t)
		f.image(testLock+":base", "sha256:base", time.Hour, time.Hour)
		f.container(testLock, "sha256:base", 700*time.Second)
		f.image(testImage, "sha256:old", 3*time.Hour, 3*time.Hour)

		out := runReaper(t, f, "REAP_LOCK_WAIT=3")

		if !strings.Contains(out, "lock  removing the stale "+testLock) {
			t.Errorf("the reaper did not break a 700 s old lock\n%s", out)
		}
		if f.tag(testImage) != "" {
			t.Errorf("the reaper kept a stale image after breaking a dead lock\n%s", out)
		}
		assertLockReleased(t, f)
	})
}

// leaseSteps returns the run script of each job's "Lease the integration
// image" step in ci.yml, by job.
func leaseSteps(t *testing.T) map[string]string {
	t.Helper()
	order, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	scripts := map[string]string{}
	for _, job := range order {
		for _, step := range ciSteps(jobs[job]) {
			if stepName(step) == "Lease the integration image" {
				scripts[job] = stepRun(t, step)
			}
		}
	}
	if len(scripts) == 0 {
		t.Fatalf("no Lease the integration image step in %s", ciWorkflowPath)
	}
	return scripts
}

// stepRun returns a step's `run: |` block without its indentation.
func stepRun(t *testing.T, step string) string {
	t.Helper()
	lines := strings.Split(step, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "run: |" {
			continue
		}
		indent := ""
		var body []string
		for _, l := range lines[i+1:] {
			if strings.TrimSpace(l) == "" {
				body = append(body, "")
				continue
			}
			if indent == "" {
				indent = l[:len(l)-len(strings.TrimLeft(l, " "))]
			}
			if !strings.HasPrefix(l, indent) {
				break
			}
			body = append(body, strings.TrimPrefix(l, indent))
		}
		return strings.Join(body, "\n") + "\n"
	}
	t.Fatalf("step %q has no run: | block", stepName(step))
	return ""
}

// runLease runs a lease step script as the runner does (bash -eo pipefail)
// and returns its output, exit code and the GITHUB_ENV it wrote.
func runLease(t *testing.T, f *fakeDocker, script string) (out string, rc int, githubEnv string) {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "lease.sh")
	f.write(path, script, 0o644)
	cmd := exec.Command("bash", "--noprofile", "--norc", "-eo", "pipefail", path)
	cmd.Env = f.env("CI_STACK_PREFIX="+testPrefix, "IT_IMAGE="+testImage, "GITHUB_RUN_ID=7",
		"GITHUB_RUN_ATTEMPT=1", "RUNNER_TEMP="+tmp, "GITHUB_ENV="+filepath.Join(tmp, "github.env"))
	raw, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		rc = exit.ExitCode()
	case err != nil:
		t.Fatalf("lease step: %v", err)
	}
	env, err := os.ReadFile(filepath.Join(tmp, "github.env"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(raw), rc, string(env)
}

// leases returns the image id of each lease container.
func leases(f *fakeDocker) []string {
	var ids []string
	for name, id := range f.containers() {
		if strings.HasPrefix(name, testPrefix+"lease-") {
			ids = append(ids, id)
		}
	}
	return ids
}

func TestLeaseStepLeasesTheImageOrRebuildsItOnce(t *testing.T) {
	cases := map[string]struct {
		setup   func(f *fakeDocker)
		rc      int
		builds  int
		message string
	}{
		"image present": {
			setup:  func(f *fakeDocker) { f.image(testImage, "sha256:old", 3*time.Hour, 3*time.Hour) },
			builds: 0,
		},
		"image missing": {
			setup:  func(*fakeDocker) {},
			builds: 1,
		},
		"tag moved before the second inspect": {
			setup: func(f *fakeDocker) {
				f.image(testImage, "sha256:old", 3*time.Hour, 3*time.Hour)
				f.onCall("create --pull never --name "+testPrefix+"lease-*", "docker _build "+testImage)
			},
			builds: 1,
		},
		"image missing after the rebuild": {
			setup:   func(f *fakeDocker) { f.fail("build-empty") },
			rc:      1,
			builds:  1,
			message: "::error::the integration image " + testImage + " is missing after one rebuild",
		},
		"rebuild fails": {
			setup:   func(f *fakeDocker) { f.fail("build") },
			rc:      2,
			builds:  1,
			message: "::error::building the integration image " + testImage + " failed (exit 2)",
		},
	}
	for job, script := range leaseSteps(t) {
		for name, tc := range cases {
			t.Run(job+"/"+name, func(t *testing.T) {
				f := newFakeDocker(t)
				tc.setup(f)

				out, rc, githubEnv := runLease(t, f, script)

				if rc != tc.rc {
					t.Fatalf("lease step exit %d, want %d\n%s", rc, tc.rc, out)
				}
				if n := f.callCount("make docker-build"); n != tc.builds {
					t.Errorf("lease step built the image %d time(s), want %d\n%s", n, tc.builds, out)
				}
				if tc.message != "" && !strings.Contains(out, tc.message) {
					t.Errorf("lease step did not report %q\n%s", tc.message, out)
				}
				assertLockReleased(t, f)
				held := leases(f)
				if tc.rc != 0 {
					if len(held) != 0 || strings.Contains(githubEnv, "IT_LEASE") {
						t.Errorf("a failed lease step left leases %v and GITHUB_ENV %q", held, githubEnv)
					}
					return
				}
				id := f.tag(testImage)
				if len(held) != 1 || held[0] != id {
					t.Errorf("leases %v, want one on %s, the id %s names\n%s", held, id, testImage, out)
				}
				if !strings.Contains(githubEnv, "IT_LEASE_ID="+id+"\n") {
					t.Errorf("GITHUB_ENV %q does not export IT_LEASE_ID=%s", githubEnv, id)
				}
			})
		}
	}
}

// TestLeaseStepHoldsTheImageLockAgainstTheReaper runs a reaper between the
// lease step's first inspect and its lease, the window in which a reaper
// without the lock removes the stale image the step just found.
func TestLeaseStepHoldsTheImageLockAgainstTheReaper(t *testing.T) {
	reaper, err := filepath.Abs(reaperPath)
	if err != nil {
		t.Fatal(err)
	}
	for job, script := range leaseSteps(t) {
		t.Run(job, func(t *testing.T) {
			f := newFakeDocker(t)
			f.image(testImage, "sha256:old", 3*time.Hour, 3*time.Hour)
			f.onCall("create --pull never --name "+testPrefix+"lease-*",
				"REAP_LOCK_WAIT=2 bash "+reaper+" "+testPrefix)

			out, rc, _ := runLease(t, f, script)

			if rc != 0 {
				t.Fatalf("lease step exit %d\n%s", rc, out)
			}
			reaped, err := os.ReadFile(f.path("hook.out"))
			if err != nil {
				t.Fatalf("the reaper did not run inside the lease window: %v", err)
			}
			if !strings.Contains(string(reaped), "keep  every "+testPrefix+"it image") {
				t.Errorf("the reaper did not wait on the lease step's lock\n%s", reaped)
			}
			if f.tag(testImage) != "sha256:old" || f.callCount("make docker-build") != 0 {
				t.Errorf("the reaper removed %s inside the lease window\n%s\n%s", testImage, reaped, out)
			}
			if held := leases(f); len(held) != 1 || held[0] != "sha256:old" {
				t.Errorf("leases %v, want one on sha256:old", held)
			}
			assertLockReleased(t, f)
		})
	}
}
