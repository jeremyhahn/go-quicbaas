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

package deploy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet"
)

// Preflight answers "will a deploy to this node work?" before anything on it
// changes. Every check is read-only: it runs probes over the deploy's own SSH
// connection and reports. It exists because the remote fleet node's deploy failures — an
// unpinned host key, no linger, a missing worker image, an image built for
// another uid, a deploy key nobody authorized — surfaced one deploy at a time.
// Preflight reports all of them at once, each with the command that fixes it.
//
// The SSH-level checks (reachability, host key, deploy key) are the dial
// itself and are recorded by the caller that dials; RunPreflight runs the
// checks that need a session.

// Minimum free space preflight requires. Worker images are several GiB each
// and are layered on the runtime's data root; workspaces hold full checkouts
// plus build caches for every job the node runs concurrently.
const (
	MinFreeImageBytes     uint64 = 20 << 30
	MinFreeWorkspaceBytes uint64 = 10 << 30
)

// Check names, in the order a report lists them.
const (
	CheckSSH              = "ssh connection"
	CheckHostKey          = "pinned host key"
	CheckDeployKey        = "deploy key authorized"
	CheckRemoteUser       = "remote user"
	CheckSystemd          = "systemd --user"
	CheckLinger           = "linger"
	CheckContainerRuntime = "container runtime"
	CheckImageDisk        = "disk for worker images"
	CheckWorkspaceDisk    = "disk for workspaces"
	CheckWorkerImage      = "worker image"
	CheckControlPlane     = "control plane reachable from node"
)

const purposeWorkspaces = "workspaces"

// PreflightCheck is one check's outcome. A failed check carries its typed
// error and the fix that error names.
type PreflightCheck struct {
	Name   string `json:"name" yaml:"name"`
	Passed bool   `json:"passed" yaml:"passed"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
	Error  string `json:"error,omitempty" yaml:"error,omitempty"`
	Fix    string `json:"fix,omitempty" yaml:"fix,omitempty"`

	err error
}

// Err returns the check's typed error, nil when it passed.
func (c PreflightCheck) Err() error { return c.err }

// PassedCheck builds a passing check.
func PassedCheck(name, detail string) PreflightCheck {
	return PreflightCheck{Name: name, Passed: true, Detail: detail}
}

// FailedCheck builds a failing check from its typed error.
func FailedCheck(name string, err error) PreflightCheck {
	return PreflightCheck{Name: name, Error: err.Error(), Fix: FixFor(err), err: err}
}

// PreflightReport is the full preflight outcome for one node.
type PreflightReport struct {
	Host   string           `json:"host" yaml:"host"`
	Checks []PreflightCheck `json:"checks" yaml:"checks"`
}

// Add appends checks to the report.
func (r *PreflightReport) Add(checks ...PreflightCheck) {
	r.Checks = append(r.Checks, checks...)
}

// Passed reports whether every check passed.
func (r *PreflightReport) Passed() bool {
	return r.Err() == nil
}

// Err returns a *PreflightFailedError listing every failed check, or nil.
func (r *PreflightReport) Err() error {
	var failures []error
	for _, check := range r.Checks {
		if check.err != nil {
			failures = append(failures, check.err)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return &PreflightFailedError{Host: r.Host, Failures: failures}
}

// PreflightInput is what the session checks need beyond the connection.
type PreflightInput struct {
	// Spec is the node spec being checked.
	Spec *Spec

	// Layout is the spec's layout resolved against the remote home.
	Layout Layout

	// ServerCAPEM is the CA bundle the deploy installs as server-ca.crt —
	// the one the daemon will verify the control plane against. The
	// control-plane check verifies with exactly this bundle and never skips
	// verification.
	ServerCAPEM []byte
}

// remoteIdentity is who the daemon will run as on the node.
type remoteIdentity struct {
	user   string
	uid    string
	gid    string
	groups []string
}

// RunPreflight runs every session-level check against the node and returns
// their outcomes in report order. Nothing on the node is changed.
func RunPreflight(ctx context.Context, c Target, in PreflightInput) []PreflightCheck {
	var checks []PreflightCheck

	identity, err := probeIdentity(ctx, c)
	if err != nil {
		checks = append(checks, FailedCheck(CheckRemoteUser, err))
	} else {
		checks = append(checks, PassedCheck(CheckRemoteUser,
			identity.user+" uid="+identity.uid+" gid="+identity.gid))
	}

	checks = append(checks, checkSupervisor(ctx, c, in.Spec, identity.user)...)

	runtime, runtimeCheck := checkContainerRuntime(ctx, c, in.Spec, identity)
	checks = append(checks, runtimeCheck)

	if runtime.root != "" {
		checks = append(checks, checkDisk(ctx, c, CheckImageDisk, runtime.root, "worker images", MinFreeImageBytes))
	}
	checks = append(checks, checkDisk(ctx, c, CheckWorkspaceDisk, in.Layout.ProjectsRoot, purposeWorkspaces, MinFreeWorkspaceBytes))

	for _, image := range in.Spec.WorkerImages {
		checks = append(checks, checkWorkerImage(ctx, c, runtime.binary, image, identity))
	}

	checks = append(checks, checkControlPlane(ctx, c, in.Spec.Server, in.ServerCAPEM))
	return checks
}

// identityCommand prints the uid, the gid and the group names, one per line.
const identityCommand = "id -un && id -u && id -g && id -nG"

func probeIdentity(ctx context.Context, c Target) (remoteIdentity, error) {
	stdout, _, err := c.Exec(ctx, identityCommand)
	if err != nil {
		return remoteIdentity{user: c.User()}, err
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) < 4 {
		return remoteIdentity{user: c.User()}, &RemoteExecError{Command: identityCommand, Stderr: "unexpected output: " + stdout}
	}
	return remoteIdentity{
		user:   strings.TrimSpace(lines[0]),
		uid:    strings.TrimSpace(lines[1]),
		gid:    strings.TrimSpace(lines[2]),
		groups: strings.Fields(lines[3]),
	}, nil
}

// sudoProbeCommand succeeds only when sudo runs without a password prompt.
const sudoProbeCommand = "sudo -n true"

// checkSupervisor checks systemd --user and, when the deploy will use it,
// lingering. A spec that leaves systemd on auto passes without it: the deploy
// falls back to the nohup launcher, and the report says so.
func checkSupervisor(ctx context.Context, c Target, spec *Spec, user string) []PreflightCheck {
	if spec.SystemdExplicit() && !spec.UseSystemd() {
		return []PreflightCheck{PassedCheck(CheckSystemd, "not used: the spec asks for the nohup launcher")}
	}
	if !DetectSystemdUser(ctx, c) {
		if spec.SystemdExplicit() {
			return []PreflightCheck{FailedCheck(CheckSystemd, &SystemdUserUnavailableError{Host: c.Host()})}
		}
		return []PreflightCheck{PassedCheck(CheckSystemd, "unavailable; the deploy will fall back to the nohup launcher")}
	}

	checks := []PreflightCheck{PassedCheck(CheckSystemd, "available")}
	if user == "" {
		user = c.User()
	}
	switch {
	case lingerEnabled(ctx, c, user):
		checks = append(checks, PassedCheck(CheckLinger, "enabled for "+user))
	case sudoAvailable(ctx, c):
		checks = append(checks, PassedCheck(CheckLinger, "off; the deploy will enable it with sudo -n"))
	default:
		checks = append(checks, FailedCheck(CheckLinger, &LingerNotEnabledError{
			User:    user,
			Command: "sudo loginctl enable-linger " + user,
		}))
	}
	return checks
}

func sudoAvailable(ctx context.Context, c Target) bool {
	_, _, err := c.Exec(ctx, sudoProbeCommand)
	return err == nil
}

// containerRuntime is the runtime preflight found: its binary and the
// directory its images live under.
type containerRuntime struct {
	binary string
	root   string
}

// runtimeRootFormat is the `info` template that prints each runtime's image
// storage root.
var runtimeRootFormat = map[string]string{
	"docker": "{{.DockerRootDir}}",
	"podman": "{{.Store.GraphRoot}}",
}

// runtimeCandidates lists the runtimes a selector accepts, in probe order.
var runtimeCandidates = map[fleet.ContainerRuntimeSelector][]string{
	fleet.ContainerRuntimeAuto:   {"docker", "podman"},
	fleet.ContainerRuntimeDocker: {"docker"},
	fleet.ContainerRuntimePodman: {"podman"},
}

func runtimeLookupCommand(binary string) string {
	return "command -v " + binary
}

func runtimeInfoCommand(binary string) string {
	return binary + " info --format " + shellQuote(runtimeRootFormat[binary])
}

func checkContainerRuntime(ctx context.Context, c Target, spec *Spec, identity remoteIdentity) (containerRuntime, PreflightCheck) {
	selector := spec.Node.EffectiveContainerRuntime()
	candidates, ok := runtimeCandidates[selector]
	if !ok {
		return containerRuntime{}, PassedCheck(CheckContainerRuntime, "not used: container_runtime is "+string(selector))
	}

	var failure error
	for _, binary := range candidates {
		if _, _, err := c.Exec(ctx, runtimeLookupCommand(binary)); err != nil {
			if failure == nil {
				failure = &ContainerRuntimeUnavailableError{Runtime: binary, User: identity.user, Reason: RuntimeNotInstalled}
			}
			continue
		}
		stdout, _, err := c.Exec(ctx, runtimeInfoCommand(binary))
		if err != nil {
			reason := RuntimeNotResponding
			if binary == "docker" && !containsString(identity.groups, "docker") && identity.uid != "0" {
				reason = RuntimeNotInGroup
			}
			// An installed runtime that fails outranks one that is absent:
			// it is the one the operator meant.
			failure = &ContainerRuntimeUnavailableError{Runtime: binary, User: identity.user, Reason: reason, Cause: err}
			continue
		}
		return containerRuntime{binary: binary, root: strings.TrimSpace(stdout)},
			PassedCheck(CheckContainerRuntime, binary+" (data root "+strings.TrimSpace(stdout)+")")
	}
	return containerRuntime{}, FailedCheck(CheckContainerRuntime, failure)
}

// diskFreeCommand prints df's POSIX report for dir, or for its nearest
// existing ancestor when dir has not been created yet — a projects root the
// daemon will create on first use is measured on the filesystem it will land
// on.
func diskFreeCommand(dir string) string {
	return "p=" + shellQuotePath(dir) + `; while [ ! -e "$p" ]; do p=$(dirname "$p"); done; df -Pk "$p"`
}

func checkDisk(ctx context.Context, c Target, name, dir, purpose string, required uint64) PreflightCheck {
	stdout, _, err := c.Exec(ctx, diskFreeCommand(dir))
	if err != nil {
		return FailedCheck(name, err)
	}
	free, err := parseDFAvailable(stdout)
	if err != nil {
		return FailedCheck(name, &RemoteExecError{Command: diskFreeCommand(dir), Stderr: stdout, Cause: err})
	}
	if free < required {
		return FailedCheck(name, &InsufficientDiskError{Path: dir, Purpose: purpose, FreeBytes: free, RequiredBytes: required})
	}
	return PassedCheck(name, formatGiB(free)+" free under "+dir)
}

// parseDFAvailable reads the Available column (1024-byte blocks) of the last
// line of `df -Pk` output and returns it in bytes.
func parseDFAvailable(out string) (uint64, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	fields := strings.Fields(lines[len(lines)-1])
	if len(lines) < 2 || len(fields) < 4 {
		return 0, &stringError{msg: "unexpected df output"}
	}
	kib, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return 0, err
	}
	return kib * 1024, nil
}

func imageUserCommand(binary, image string) string {
	return binary + " image inspect --format '{{.Config.User}}' " + shellQuote(image)
}

func imagePullableCommand(binary, image string) string {
	return binary + " manifest inspect " + shellQuote(image) + " >/dev/null"
}

// checkWorkerImage checks one worker image is on the node or pullable, and,
// when it is on the node, that it was built for the uid the daemon runs as.
// An image that is only pullable cannot have its uid read without pulling it,
// which preflight does not do; the report says the uid is unverified.
func checkWorkerImage(ctx context.Context, c Target, binary, image string, identity remoteIdentity) PreflightCheck {
	name := CheckWorkerImage + " " + image
	if binary == "" {
		return FailedCheck(name, &WorkerImageUnavailableError{Image: image})
	}
	stdout, _, err := c.Exec(ctx, imageUserCommand(binary, image))
	if err != nil {
		if _, _, pullErr := c.Exec(ctx, imagePullableCommand(binary, image)); pullErr != nil {
			return FailedCheck(name, &WorkerImageUnavailableError{Image: image, Runtime: binary, Cause: pullErr})
		}
		return PassedCheck(name, "not on the node; pullable (AGENT_UID unverified until pulled)")
	}
	imageUser := strings.TrimSpace(stdout)
	if imageUID(imageUser) != identity.uid {
		return FailedCheck(name, &WorkerImageUIDMismatchError{
			Image:     image,
			ImageUser: imageUser,
			RemoteUID: identity.uid,
			RemoteGID: identity.gid,
		})
	}
	return PassedCheck(name, "present; runs as uid "+identity.uid)
}

// imageUID extracts the uid from an image's USER ("1001:1001", "1001", or
// empty for root). A USER given by name has no uid preflight can compare, so
// it is returned as the name and never matches a numeric uid.
func imageUID(user string) string {
	if user == "" {
		return "0"
	}
	uid, _, _ := strings.Cut(user, ":")
	return uid
}

// controlPlaneTimeout bounds the tunnelled dial and TLS handshake.
const controlPlaneTimeout = 15 * time.Second

// checkControlPlane opens a TLS connection to the control plane FROM THE
// NODE — tunnelled through the SSH connection, so routing and name resolution
// are the node's — and verifies the server's certificate against the CA the
// deploy installs. Verification is never skipped: a node that can only reach
// the control plane insecurely is a node whose daemon cannot enroll.
func checkControlPlane(ctx context.Context, c Target, server string, caPEM []byte) PreflightCheck {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return FailedCheck(CheckControlPlane, &ControlPlaneUnreachableError{
			Server: server, Stage: "url", Cause: &stringError{msg: "the control plane address must be an https:// URL"},
		})
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	addr := net.JoinHostPort(u.Hostname(), port)

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return FailedCheck(CheckControlPlane, &ControlPlaneUnreachableError{
			Server: server, Addr: addr, Stage: "ca", Cause: &stringError{msg: "no CA certificate to verify the control plane with"},
		})
	}

	ctx, cancel := context.WithTimeout(ctx, controlPlaneTimeout)
	defer cancel()
	conn, err := c.DialTCP(ctx, addr)
	if err != nil {
		return FailedCheck(CheckControlPlane, &ControlPlaneUnreachableError{Server: server, Addr: addr, Stage: "dial", Cause: err})
	}
	tlsConn := tls.Client(conn, &tls.Config{
		RootCAs:    pool,
		ServerName: u.Hostname(),
		MinVersion: tls.VersionTLS12,
	})
	defer tlsConn.Close()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return FailedCheck(CheckControlPlane, &ControlPlaneUnreachableError{Server: server, Addr: addr, Stage: "tls", Cause: err})
	}
	return PassedCheck(CheckControlPlane, addr+" verified against the deploy's CA")
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
