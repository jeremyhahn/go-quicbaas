package buildgates

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The Kubernetes deployment of SPEC row `deployment` (go-quicbaas#1696):
// deployments/kubernetes is a kustomization of a 3-node Raft cluster. These
// tests hold the properties the row's acceptance names: every node runs one
// go-quicbaas tag, the three replicas can form a group, and the cluster
// keeps a quorum through the loss of one node.

var kubernetesDir = filepath.Join(repoRoot, "deployments", "kubernetes")

const quicbaasdImage = "ghcr.io/jeremyhahn/quicbaasd"

// testTrustDomain stands in for the operator's SPIFFE trust domain.
const testTrustDomain = "quicbaas.example.test"

// k8sObject is one decoded manifest document.
type k8sObject map[string]any

func (o k8sObject) kind() string { s, _ := o["kind"].(string); return s }

func (o k8sObject) name() string {
	s, _ := dig(o, "metadata", "name").(string)
	return s
}

// dig walks nested maps and lists: a string key indexes a map, an int a list.
func dig(v any, path ...any) any {
	for _, p := range path {
		switch key := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				if o, isObj := v.(k8sObject); isObj {
					m, ok = map[string]any(o), true
				}
			}
			if !ok {
				return nil
			}
			v = m[key]
		case int:
			l, ok := v.([]any)
			if !ok || key >= len(l) {
				return nil
			}
			v = l[key]
		}
	}
	return v
}

func decodeManifests(t *testing.T, raw []byte) []k8sObject {
	t.Helper()
	var objs []k8sObject
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var obj map[string]any
		err := dec.Decode(&obj)
		if errors.Is(err, io.EOF) {
			return objs
		}
		if err != nil {
			t.Fatalf("decode manifest: %v", err)
		}
		if obj != nil {
			objs = append(objs, k8sObject(obj))
		}
	}
}

// kubernetesManifests returns the kustomization and every object of the
// resources it lists, read from disk.
func kubernetesManifests(t *testing.T) (k8sObject, []k8sObject) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(kubernetesDir, "kustomization.yaml"))
	if err != nil {
		t.Fatalf("read kustomization: %v", err)
	}
	kust := decodeManifests(t, raw)
	if len(kust) != 1 {
		t.Fatalf("kustomization.yaml holds %d documents, want 1", len(kust))
	}
	resources, _ := kust[0]["resources"].([]any)
	if len(resources) == 0 {
		t.Fatal("kustomization.yaml lists no resources")
	}
	var objs []k8sObject
	for _, r := range resources {
		file, _ := r.(string)
		raw, err := os.ReadFile(filepath.Join(kubernetesDir, file))
		if err != nil {
			t.Fatalf("kustomization resource %q: %v", file, err)
		}
		objs = append(objs, decodeManifests(t, raw)...)
	}
	return kust[0], objs
}

func findObject(t *testing.T, objs []k8sObject, kind, name string) k8sObject {
	t.Helper()
	for _, o := range objs {
		if o.kind() == kind && o.name() == name {
			return o
		}
	}
	t.Fatalf("no %s %q in deployments/kubernetes", kind, name)
	return nil
}

func quicbaasdContainer(t *testing.T, sts k8sObject) map[string]any {
	t.Helper()
	containers, _ := dig(sts, "spec", "template", "spec", "containers").([]any)
	if len(containers) != 1 {
		t.Fatalf("StatefulSet has %d containers, want 1", len(containers))
	}
	c, _ := containers[0].(map[string]any)
	return c
}

// unsetReleaseTag is the placeholder the kustomization ships: it names no
// image, so an apply before the operator chooses a release fails visibly.
const unsetReleaseTag = "set-a-release-that-serves-rest"

// restlessReleases were built from a Dockerfile whose default BUILD_TAGS
// lacked `rest`: their images register no REST routes, so no probe passes.
var restlessReleases = []string{"0.16.0-alpha", "0.16.1-alpha"}

// imageTagProblems reports why the kustomization does not pin quicbaasd to
// one release tag that can pass the REST probes.
func imageTagProblems(kust k8sObject) []string {
	var problems []string
	images, _ := kust["images"].([]any)
	pinned := 0
	for _, img := range images {
		if dig(img, "name") != quicbaasdImage {
			continue
		}
		pinned++
		tag, _ := dig(img, "newTag").(string)
		switch {
		case tag == "" || tag == "latest":
			problems = append(problems, "quicbaasd image tag "+tag+" is not a release tag")
		case slices.Contains(restlessReleases, tag):
			problems = append(problems, "release "+tag+" registers no REST routes; its pods never pass a probe")
		}
	}
	if pinned != 1 {
		problems = append(problems, fmt.Sprintf("kustomization pins the quicbaasd image %d times, want exactly once", pinned))
	}
	return problems
}

func TestKubernetesEveryNodeRunsTheOneReleaseTag(t *testing.T) {
	kust, objs := kubernetesManifests(t)
	for _, p := range imageTagProblems(kust) {
		t.Error(p)
	}

	// The tag lives in the kustomization alone: an image reference that
	// carries its own tag or digest would escape the pin.
	for _, o := range objs {
		for _, field := range []string{"initContainers", "containers"} {
			containers, _ := dig(o, "spec", "template", "spec", field).([]any)
			for _, c := range containers {
				image, _ := dig(c, "image").(string)
				if image != quicbaasdImage {
					t.Errorf("%s %s: %s image %q, want the untagged %q the kustomization pins",
						o.kind(), o.name(), field, image, quicbaasdImage)
				}
			}
		}
	}
}

func TestImageTagProblemsRejectsLatestAndRestlessReleases(t *testing.T) {
	for tag, want := range map[string]int{
		"latest":        1,
		"0.16.0-alpha":  1,
		"0.16.1-alpha":  1,
		"0.17.0-alpha":  0,
		unsetReleaseTag: 0,
	} {
		kust := k8sObject{"images": []any{map[string]any{"name": quicbaasdImage, "newTag": tag}}}
		if got := len(imageTagProblems(kust)); got != want {
			t.Errorf("tag %q: %d problems, want %d", tag, got, want)
		}
	}
	if len(imageTagProblems(k8sObject{})) == 0 {
		t.Error("an unpinned image reports no problem")
	}
}

func TestKubernetesThreeNodeRaftClusterForms(t *testing.T) {
	_, objs := kubernetesManifests(t)
	sts := findObject(t, objs, "StatefulSet", "quicbaas")

	if r, _ := dig(sts, "spec", "replicas").(int); r != 3 {
		t.Errorf("replicas = %v, want 3", dig(sts, "spec", "replicas"))
	}
	// The three bootstrap members must start together: OrderedReady waits
	// for quicbaas-0 to be ready, which it never is without a quorum.
	if p := dig(sts, "spec", "podManagementPolicy"); p != "Parallel" {
		t.Errorf("podManagementPolicy = %v, want Parallel", p)
	}

	serviceName, _ := dig(sts, "spec", "serviceName").(string)
	headless := findObject(t, objs, "Service", serviceName)
	if dig(headless, "spec", "clusterIP") != "None" {
		t.Errorf("Service %s is not headless", serviceName)
	}
	if dig(headless, "spec", "publishNotReadyAddresses") != true {
		t.Errorf("Service %s does not publish unready pods: peers cannot resolve one another before the group forms", serviceName)
	}

	c := quicbaasdContainer(t, sts)
	script, _ := dig(c, "command", 2).(string)
	for _, want := range []string{
		"QUICBAAS_RAFT_NODE_ID=",
		"QUICBAAS_RAFT_RAFT_ADDRESS=",
		"QUICBAAS_RAFT_JOIN_ADDRESSES=",
		"QUICBAAS_RAFT_MEMBER_IDENTITIES=",
		"member-identities",
		"for i in 0 1 2",
		"." + serviceName + ".",
		"quicbaasd node --config /etc/quicbaas/quicbaas.yaml",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("start command lacks %q", want)
		}
	}

	// The seal passphrase comes from a Secret, never a literal.
	env, _ := dig(c, "env").([]any)
	sealed := false
	for _, e := range env {
		if dig(e, "name") == "QUICBAAS_SEAL_PASSPHRASE" {
			sealed = dig(e, "valueFrom", "secretKeyRef", "name") != nil && dig(e, "value") == nil
		}
	}
	if !sealed {
		t.Error("QUICBAAS_SEAL_PASSPHRASE is not read from a Secret")
	}
}

// runStartCommand runs the StatefulSet's start command as pod podName with
// the raft-tls Secret mounted from tlsDir and trustDomain as the operator's
// SPIFFE trust domain (unset when empty), and returns the environment it
// hands to quicbaasd (the exec is replaced by env).
func runStartCommand(t *testing.T, podName, tlsDir, trustDomain string) (map[string]string, string, error) {
	t.Helper()
	_, objs := kubernetesManifests(t)
	sts := findObject(t, objs, "StatefulSet", "quicbaas")
	script, _ := dig(quicbaasdContainer(t, sts), "command", 2).(string)

	const tlsLine = "tls=/etc/quicbaas/raft-tls\n"
	const execLine = "exec /usr/local/bin/quicbaasd node --config /etc/quicbaas/quicbaas.yaml"
	if !strings.Contains(script, tlsLine) || !strings.Contains(script, execLine) {
		t.Fatalf("start command no longer has %q and %q", tlsLine, execLine)
	}
	script = strings.Replace(script, tlsLine, "tls="+tlsDir+"\n", 1)
	script = strings.Replace(script, execLine, "exec env", 1)

	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"POD_NAME=" + podName,
		"POD_NAMESPACE=quicbaas",
		"POD_IP=10.0.0.2",
	}
	if trustDomain != "" {
		cmd.Env = append(cmd.Env, "QUICBAAS_IAM_SPIFFE_TRUST_DOMAIN="+trustDomain)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	env := map[string]string{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			env[k] = v
		}
	}
	return env, stderr.String(), err
}

func TestKubernetesStartCommandSuppliesTheFoundersIdentities(t *testing.T) {
	const (
		hex1 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		hex2 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		hex3 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		id1  = "1=spki:" + hex1
		id2  = "2=spki:" + hex2
		id3  = "3=spki:" + hex3
	)
	writeIDs := func(t *testing.T, body string) string {
		dir := t.TempDir()
		if body != "" {
			if err := os.WriteFile(filepath.Join(dir, "member-identities"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	t.Run("each replica gets the other two founders", func(t *testing.T) {
		dir := writeIDs(t, id1+","+id2+","+id3+"\n")
		for pod, want := range map[string]string{
			"quicbaas-0": id2 + "," + id3,
			"quicbaas-1": id1 + "," + id3,
			"quicbaas-2": id1 + "," + id2,
		} {
			env, stderr, err := runStartCommand(t, pod, dir, testTrustDomain)
			if err != nil {
				t.Fatalf("%s: start command failed: %v\n%s", pod, err, stderr)
			}
			if got := env["QUICBAAS_RAFT_MEMBER_IDENTITIES"]; got != want {
				t.Errorf("%s: QUICBAAS_RAFT_MEMBER_IDENTITIES = %q, want %q", pod, got, want)
			}
		}
	})

	t.Run("replica 1 exports its node and peers", func(t *testing.T) {
		const peer = ".quicbaas-raft.quicbaas.svc.cluster.local:63000"
		env, stderr, err := runStartCommand(t, "quicbaas-1", writeIDs(t, id1+"\n"+id2+"\n"+id3+"\n"), testTrustDomain)
		if err != nil {
			t.Fatalf("start command failed: %v\n%s", err, stderr)
		}
		for k, want := range map[string]string{
			"QUICBAAS_RAFT_NODE_ID":           "2",
			"QUICBAAS_RAFT_RAFT_ADDRESS":      "quicbaas-1" + peer,
			"QUICBAAS_RAFT_LISTEN_HOST":       "10.0.0.2",
			"QUICBAAS_RAFT_JOIN_ADDRESSES":    "1=quicbaas-0" + peer + ",2=quicbaas-1" + peer + ",3=quicbaas-2" + peer,
			"QUICBAAS_RAFT_MEMBER_IDENTITIES": id1 + "," + id3,
		} {
			if got := env[k]; got != want {
				t.Errorf("%s = %q, want %q", k, got, want)
			}
		}
		// The ConfigMap names the fixed paths the raft-key init container
		// fills; the start command must not point anywhere else.
		for _, k := range []string{"QUICBAAS_RAFT_TLS_CERT_FILE", "QUICBAAS_RAFT_TLS_KEY_FILE"} {
			if v, ok := env[k]; ok {
				t.Errorf("start command exports %s=%q; the ConfigMap names the pod's own pair", k, v)
			}
		}
	})

	t.Run("refuses to start: no trust domain", func(t *testing.T) {
		_, stderr, err := runStartCommand(t, "quicbaas-1", writeIDs(t, id1+","+id2+","+id3), "")
		if err == nil {
			t.Fatal("start command succeeded without a trust domain; want a refusal before quicbaasd starts")
		}
		if !strings.Contains(stderr, "spiffe-trust-domain") {
			t.Errorf("stderr = %q, want it to name spiffe-trust-domain", stderr)
		}
	})

	for _, tc := range []struct{ name, body, wantErr string }{
		{"no member-identities key", "", "no member-identities key"},
		{"a founder is missing", id1 + "," + id2, "no identity for Raft node 3"},
		{"a malformed entry", id1 + ",3=" + hex3, "is not <1-3>=spki:<64 hex digits>"},
		{"a digest with a non-hex digit", id1 + ",3=spki:xyz" + hex3[3:], "is not <1-3>=spki:<64 hex digits>"},
		{"a short digest", id1 + ",3=spki:" + hex3[1:], "is not <1-3>=spki:<64 hex digits>"},
		{"an uppercase digest", id1 + ",3=spki:" + strings.ToUpper(hex3), "is not <1-3>=spki:<64 hex digits>"},
	} {
		t.Run("refuses to start: "+tc.name, func(t *testing.T) {
			_, stderr, err := runStartCommand(t, "quicbaas-1", writeIDs(t, tc.body), testTrustDomain)
			if err == nil {
				t.Fatal("start command succeeded; want a refusal before quicbaasd starts")
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Errorf("stderr = %q, want it to name %q", stderr, tc.wantErr)
			}
		})
	}
}

func TestKubernetesClusterSurvivesTheLossOfOneNode(t *testing.T) {
	_, objs := kubernetesManifests(t)
	sts := findObject(t, objs, "StatefulSet", "quicbaas")

	terms, _ := dig(sts, "spec", "template", "spec", "affinity", "podAntiAffinity",
		"requiredDuringSchedulingIgnoredDuringExecution").([]any)
	spread := false
	for _, term := range terms {
		if dig(term, "topologyKey") == "kubernetes.io/hostname" {
			spread = true
		}
	}
	if !spread {
		t.Error("replicas are not required onto distinct nodes: one node failure could take a quorum")
	}

	pdb := findObject(t, objs, "PodDisruptionBudget", "quicbaas")
	if m, _ := dig(pdb, "spec", "minAvailable").(int); m != 2 {
		t.Errorf("PodDisruptionBudget minAvailable = %v, want 2 (a quorum of 3)", dig(pdb, "spec", "minAvailable"))
	}

	if dig(sts, "spec", "volumeClaimTemplates", 0, "metadata", "name") != "data" {
		t.Error("StatefulSet has no per-replica data claim: a replaced pod would rejoin empty")
	}
}

// TestKubernetesPortsMatchTheNodeConfig holds the container ports, the
// Services and the NetworkPolicy to the ports the shared node config serves.
func TestKubernetesPortsMatchTheNodeConfig(t *testing.T) {
	_, objs := kubernetesManifests(t)
	cm := findObject(t, objs, "ConfigMap", "quicbaas-config")
	nodeYAML, _ := dig(cm, "data", "quicbaas.yaml").(string)
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(nodeYAML), &cfg); err != nil {
		t.Fatalf("ConfigMap quicbaas.yaml: %v", err)
	}
	want := map[string]int{
		"rest": dig(cfg, "server", "rest", "port").(int),
		"grpc": dig(cfg, "server", "grpc", "port").(int),
		"raft": 63000,
	}

	c := quicbaasdContainer(t, findObject(t, objs, "StatefulSet", "quicbaas"))
	ports, _ := dig(c, "ports").([]any)
	got := map[string]int{}
	for _, p := range ports {
		name, _ := dig(p, "name").(string)
		got[name], _ = dig(p, "containerPort").(int)
	}
	for name, port := range want {
		if got[name] != port {
			t.Errorf("container port %s = %d, want %d", name, got[name], port)
		}
	}

	for _, probe := range []string{"startupProbe", "livenessProbe", "readinessProbe"} {
		if dig(c, probe, "httpGet", "port") != "rest" {
			t.Errorf("%s does not probe the rest port", probe)
		}
		// The node always serves REST over TLS (config.Loader forces
		// security.TLSEnabled), so a plain HTTP probe would fail every pod.
		if dig(c, probe, "httpGet", "scheme") != "HTTPS" {
			t.Errorf("%s probes over HTTP, but the node serves TLS", probe)
		}
	}
}

// The node serves its Unix socket whenever server.unix.socket is set, and the
// pods run with a read-only root filesystem, so the socket's directory must be
// a writable emptyDir mount of the quicbaasd container.
func TestKubernetesUnixSocketIsOnAWritableMount(t *testing.T) {
	_, objs := kubernetesManifests(t)
	cm := findObject(t, objs, "ConfigMap", "quicbaas-config")
	nodeYAML, _ := dig(cm, "data", "quicbaas.yaml").(string)
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(nodeYAML), &cfg); err != nil {
		t.Fatalf("ConfigMap quicbaas.yaml: %v", err)
	}
	socket, _ := dig(cfg, "server", "unix", "socket").(string)
	if socket == "" {
		t.Fatal("ConfigMap sets no server.unix.socket: the default is on the read-only root filesystem")
	}
	dir := filepath.Dir(socket)

	sts := findObject(t, objs, "StatefulSet", "quicbaas")
	c := quicbaasdContainer(t, sts)
	mounts, _ := dig(c, "volumeMounts").([]any)
	volume := ""
	for _, m := range mounts {
		if dig(m, "mountPath") != dir {
			continue
		}
		if ro, _ := dig(m, "readOnly").(bool); ro {
			t.Fatalf("socket directory %s is mounted read-only", dir)
		}
		volume, _ = dig(m, "name").(string)
	}
	if volume == "" {
		t.Fatalf("socket directory %s is not a volumeMount of quicbaasd", dir)
	}

	volumes, _ := dig(sts, "spec", "template", "spec", "volumes").([]any)
	for _, v := range volumes {
		if dig(v, "name") != volume {
			continue
		}
		if _, ok := dig(v, "emptyDir").(map[string]any); !ok {
			t.Errorf("volume %s holding the socket is not an emptyDir", volume)
		}
		return
	}
	t.Errorf("volume %s is not in the pod spec", volume)
}

// TestKubernetesGuideRenewsRaftCertificatesOnTheirKey holds the guide to the
// renewal procedure: go-quicraft refuses, on restart, a replica whose key
// differs from its committed identity, so a certificate is renewed on the
// existing private key. It also keeps the rekey-and-restart instruction from
// coming back.
func TestKubernetesGuideRenewsRaftCertificatesOnTheirKey(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "docs", "deployment", "kubernetes.md"))
	if err != nil {
		t.Fatalf("read kubernetes guide: %v", err)
	}
	guide := string(data)
	for _, want := range []string{
		"## Renewing a Raft certificate",
		"-key quicbaas-",
		"existing private key",
		"renew-raft-cert.sh",
	} {
		if !strings.Contains(guide, want) {
			t.Errorf("kubernetes guide lacks %q", want)
		}
	}
	// A direct pod deletion bypasses the PodDisruptionBudget; the renewal
	// evicts through the Eviction API instead.
	if strings.Contains(guide, "delete pod") {
		t.Error("kubernetes guide deletes a pod directly, bypassing the PodDisruptionBudget")
	}
	for i, line := range strings.Split(guide, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "restart") && strings.Contains(line, "Regenerate") &&
			!strings.Contains(lower, "do not") {
			t.Errorf("kubernetes.md:%d tells operators to regenerate and restart: %q", i+1, line)
		}
	}
}

// nodeConfig returns the ConfigMap's quicbaas.yaml, decoded.
func nodeConfig(t *testing.T, objs []k8sObject) map[string]any {
	t.Helper()
	cm := findObject(t, objs, "ConfigMap", "quicbaas-config")
	nodeYAML, _ := dig(cm, "data", "quicbaas.yaml").(string)
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(nodeYAML), &cfg); err != nil {
		t.Fatalf("ConfigMap quicbaas.yaml: %v", err)
	}
	return cfg
}

// TestKubernetesTrustDomainIsTheOperatorsChoice holds that the deployment
// ships no SPIFFE trust domain: it is stamped into every issued certificate,
// so the operator supplies it through ConfigMap quicbaas-identity.
func TestKubernetesTrustDomainIsTheOperatorsChoice(t *testing.T) {
	_, objs := kubernetesManifests(t)
	if td, _ := dig(nodeConfig(t, objs), "iam", "spiffe_trust_domain").(string); td != "" {
		t.Errorf("ConfigMap quicbaas.yaml sets iam.spiffe_trust_domain %q; it is the operator's choice", td)
	}

	c := quicbaasdContainer(t, findObject(t, objs, "StatefulSet", "quicbaas"))
	env, _ := dig(c, "env").([]any)
	found := false
	for _, e := range env {
		if dig(e, "name") != "QUICBAAS_IAM_SPIFFE_TRUST_DOMAIN" {
			continue
		}
		found = true
		if _, ok := dig(e, "value").(string); ok {
			t.Error("QUICBAAS_IAM_SPIFFE_TRUST_DOMAIN has a literal value; it must come from ConfigMap quicbaas-identity")
		}
		if dig(e, "valueFrom", "configMapKeyRef", "name") != "quicbaas-identity" ||
			dig(e, "valueFrom", "configMapKeyRef", "key") != "spiffe-trust-domain" {
			t.Errorf("QUICBAAS_IAM_SPIFFE_TRUST_DOMAIN valueFrom = %v, want configMapKeyRef quicbaas-identity/spiffe-trust-domain",
				dig(e, "valueFrom"))
		}
	}
	if !found {
		t.Error("quicbaasd has no QUICBAAS_IAM_SPIFFE_TRUST_DOMAIN: first boot refuses an empty trust domain")
	}
}

// TestKubernetesOnlyTheReplicasOwnKeyReachesTheDaemon holds that no daemon
// can read another replica's Raft private key: the keys Secret is mounted by
// the init container alone, which hands the daemon its own pair.
func TestKubernetesOnlyTheReplicasOwnKeyReachesTheDaemon(t *testing.T) {
	_, objs := kubernetesManifests(t)
	sts := findObject(t, objs, "StatefulSet", "quicbaas")
	podSpec := dig(sts, "spec", "template", "spec")
	if v, ok := dig(podSpec, "automountServiceAccountToken").(bool); !ok || v {
		t.Error("pod spec does not set automountServiceAccountToken: false")
	}

	cfg := nodeConfig(t, objs)
	for key, want := range map[string]string{
		"tls_cert_file": "/etc/quicbaas/raft-node/tls.crt",
		"tls_key_file":  "/etc/quicbaas/raft-node/tls.key",
	} {
		if got := dig(cfg, "raft", key); got != want {
			t.Errorf("ConfigMap raft.%s = %v, want %s", key, got, want)
		}
	}

	volumes, _ := dig(podSpec, "volumes").([]any)
	keyVolumes := map[string]bool{}
	for _, v := range volumes {
		name, _ := dig(v, "name").(string)
		if dig(v, "secret", "secretName") == "quicbaas-raft-keys" {
			keyVolumes[name] = true
		}
		if name == "raft-node" && dig(v, "emptyDir", "medium") != "Memory" {
			t.Error("volume raft-node is not a memory-backed emptyDir")
		}
	}
	if len(keyVolumes) == 0 {
		t.Fatal("no volume of Secret quicbaas-raft-keys")
	}

	containers, _ := dig(podSpec, "containers").([]any)
	for _, c := range containers {
		mounts, _ := dig(c, "volumeMounts").([]any)
		for _, m := range mounts {
			if name, _ := dig(m, "name").(string); keyVolumes[name] {
				t.Errorf("container %v mounts Secret quicbaas-raft-keys: every replica's key", dig(c, "name"))
			}
		}
	}
	inits, _ := dig(podSpec, "initContainers").([]any)
	mounting := 0
	for _, c := range inits {
		mounts, _ := dig(c, "volumeMounts").([]any)
		for _, m := range mounts {
			if name, _ := dig(m, "name").(string); keyVolumes[name] {
				mounting++
				if ro, _ := dig(m, "readOnly").(bool); !ro {
					t.Errorf("init container %v mounts the keys Secret writable", dig(c, "name"))
				}
			}
		}
	}
	if mounting != 1 {
		t.Errorf("%d init container mounts of Secret quicbaas-raft-keys, want exactly 1", mounting)
	}

	c := quicbaasdContainer(t, sts)
	mounts, _ := dig(c, "volumeMounts").([]any)
	nodeMounted := false
	for _, m := range mounts {
		if dig(m, "name") != "raft-node" {
			continue
		}
		nodeMounted = true
		ro, _ := dig(m, "readOnly").(bool)
		if dig(m, "mountPath") != "/etc/quicbaas/raft-node" || !ro {
			t.Errorf("quicbaasd mounts raft-node at %v (readOnly %v), want /etc/quicbaas/raft-node read-only",
				dig(m, "mountPath"), ro)
		}
	}
	if !nodeMounted {
		t.Error("quicbaasd does not mount raft-node")
	}

	script, _ := dig(c, "command", 2).(string)
	if strings.Contains(script, "QUICBAAS_RAFT_TLS_KEY_FILE") || strings.Contains(script, `.key"`) {
		t.Error("the quicbaasd start command names a Raft key file; the ConfigMap names the pod's own")
	}
}

// runKeyInit runs the raft-key init container's script as pod podName with
// Secret quicbaas-raft-keys mounted from keysDir and the raft-node volume at
// outDir, and returns its stderr.
func runKeyInit(t *testing.T, podName, keysDir, outDir string) (string, error) {
	t.Helper()
	_, objs := kubernetesManifests(t)
	sts := findObject(t, objs, "StatefulSet", "quicbaas")
	script, _ := dig(sts, "spec", "template", "spec", "initContainers", 0, "command", 2).(string)

	const keysLine = "keys=/etc/quicbaas/raft-keys\n"
	const outLine = "out=/etc/quicbaas/raft-node\n"
	if !strings.Contains(script, keysLine) || !strings.Contains(script, outLine) {
		t.Fatalf("raft-key init script no longer has %q and %q", keysLine, outLine)
	}
	script = strings.Replace(script, keysLine, "keys="+keysDir+"\n", 1)
	script = strings.Replace(script, outLine, "out="+outDir+"\n", 1)

	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "POD_NAME=" + podName}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

func TestKubernetesKeyInitCopiesOnlyTheReplicasOwnPair(t *testing.T) {
	writeKeys := func(t *testing.T, skip string) string {
		dir := t.TempDir()
		for _, pod := range []string{"quicbaas-0", "quicbaas-1", "quicbaas-2"} {
			for _, ext := range []string{".crt", ".key"} {
				if pod+ext == skip {
					continue
				}
				if err := os.WriteFile(filepath.Join(dir, pod+ext), []byte(pod+ext+" content\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		return dir
	}

	t.Run("the replica's own pair, owner-only, idempotent", func(t *testing.T) {
		keys, out := writeKeys(t, ""), t.TempDir()
		for run := 1; run <= 2; run++ {
			if stderr, err := runKeyInit(t, "quicbaas-1", keys, out); err != nil {
				t.Fatalf("run %d: init failed: %v\n%s", run, err, stderr)
			}
		}
		entries, err := os.ReadDir(out)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if !slices.Equal(names, []string{"tls.crt", "tls.key"}) {
			t.Fatalf("raft-node holds %v, want exactly [tls.crt tls.key]", names)
		}
		for name, want := range map[string]struct {
			src  string
			perm os.FileMode
		}{
			"tls.crt": {"quicbaas-1.crt", 0o444},
			"tls.key": {"quicbaas-1.key", 0o400},
		} {
			got, err := os.ReadFile(filepath.Join(out, name))
			if err != nil {
				t.Fatal(err)
			}
			src, _ := os.ReadFile(filepath.Join(keys, want.src))
			if !bytes.Equal(got, src) {
				t.Errorf("%s = %q, want the content of %s", name, got, want.src)
			}
			info, err := os.Stat(filepath.Join(out, name))
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); perm != want.perm {
				t.Errorf("%s mode = %o, want %o", name, perm, want.perm)
			}
		}
	})

	t.Run("refuses a Secret without the replica's key", func(t *testing.T) {
		out := t.TempDir()
		stderr, err := runKeyInit(t, "quicbaas-1", writeKeys(t, "quicbaas-1.key"), out)
		if err == nil {
			t.Fatal("init succeeded without quicbaas-1.key")
		}
		if !strings.Contains(stderr, "quicbaas-1.key") {
			t.Errorf("stderr = %q, want it to name quicbaas-1.key", stderr)
		}
		if _, err := os.Stat(filepath.Join(out, "tls.key")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("tls.key exists after a refusal (stat: %v)", err)
		}
	})
}

// TestKubernetesRenewRaftCertScript runs deployments/kubernetes/renew-raft-cert.sh
// against fake openssl and kubectl: it stops before any kubectl call when
// the key changed or the replica is unknown, evicts through the Eviction API
// (never a direct delete), and stops when the eviction is refused.
func TestKubernetesRenewRaftCertScript(t *testing.T) {
	for _, tool := range []string{"sha256sum", "mktemp", "/bin/sh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found: %v", tool, err)
		}
	}
	bin := t.TempDir()
	fakes := map[string]string{
		// The spki of a file is the sha256 of its content.
		"openssl": `#!/bin/sh
if [ "$1" = x509 ]; then
  while [ "$#" -gt 0 ]; do
    if [ "$1" = -in ]; then cat "$2"; exit 0; fi
    shift
  done
  exit 1
fi
cat
`,
		"kubectl": `#!/bin/sh
echo "$*" >> "$KUBECTL_LOG"
case " $* " in
  *" get pod "*)
    if [ -e "$KUBECTL_LOG.evicted" ]; then echo uid-new; else echo uid-old; fi ;;
  *"create --raw"*)
    if [ -n "${KUBECTL_FAIL_EVICT:-}" ]; then
      echo "Cannot evict pod as it would violate the pod's disruption budget." >&2
      exit 1
    fi
    : > "$KUBECTL_LOG.evicted" ;;
  *"--dry-run=client"*) echo "kind: Secret" ;;
  *"apply -f"*) exit 0 ;;
  *" wait "*) exit 0 ;;
esac
`,
	}
	for name, body := range fakes {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		// The fakes run from PATH, so they must be executable.
		if out, err := exec.Command("chmod", "u+x", path).CombinedOutput(); err != nil {
			t.Fatalf("chmod %s: %v\n%s", name, err, out)
		}
	}
	// The script runs from its own work directory, so its path is absolute.
	script, err := filepath.Abs(filepath.Join(kubernetesDir, "renew-raft-cert.sh"))
	if err != nil {
		t.Fatal(err)
	}

	run := func(t *testing.T, arg, oldCert string, failEvict bool) (string, []string, error) {
		t.Helper()
		work := t.TempDir()
		for _, pod := range []string{"quicbaas-0", "quicbaas-1", "quicbaas-2"} {
			for _, ext := range []string{".crt", ".key"} {
				if err := os.WriteFile(filepath.Join(work, pod+ext), []byte(pod+ext+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := os.Mkdir(filepath.Join(work, "old"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(work, "old", "quicbaas-1.crt"), []byte(oldCert), 0o600); err != nil {
			t.Fatal(err)
		}
		logPath := filepath.Join(t.TempDir(), "kubectl.log")
		cmd := exec.Command("/bin/sh", script, arg)
		cmd.Dir = work
		cmd.Env = []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "KUBECTL_LOG=" + logPath}
		if failEvict {
			cmd.Env = append(cmd.Env, "KUBECTL_FAIL_EVICT=1")
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		var lines []string
		if raw, rerr := os.ReadFile(logPath); rerr == nil {
			lines = strings.Split(strings.TrimSpace(string(raw)), "\n")
		} else if !errors.Is(rerr, os.ErrNotExist) {
			t.Fatal(rerr)
		}
		return stderr.String(), lines, err
	}

	t.Run("key changed", func(t *testing.T) {
		stderr, lines, err := run(t, "1", "another key\n", false)
		if err == nil {
			t.Fatal("renewal succeeded with a changed key")
		}
		if !strings.Contains(stderr, "different key") {
			t.Errorf("stderr = %q, want it to say the key is different", stderr)
		}
		if lines != nil {
			t.Errorf("kubectl ran before the identity check stopped: %q", lines)
		}
	})

	t.Run("key unchanged", func(t *testing.T) {
		stderr, lines, err := run(t, "1", "quicbaas-1.crt\n", false)
		if err != nil {
			t.Fatalf("renewal failed: %v\n%s", err, stderr)
		}
		want := []string{
			"create secret generic quicbaas-raft-keys",
			"apply -f",
			"get pod quicbaas-1",
			"create --raw /api/v1/namespaces/quicbaas/pods/quicbaas-1/eviction",
			"wait --for=condition=Ready pod/quicbaas-1",
		}
		i := 0
		for _, line := range lines {
			if strings.Contains(line, "delete") {
				t.Errorf("renewal runs %q; a direct delete bypasses the PodDisruptionBudget", line)
			}
			if i < len(want) && strings.Contains(line, want[i]) {
				i++
			}
		}
		if i != len(want) {
			t.Errorf("kubectl calls %q lack, in order, %q", lines, want[i])
		}
	})

	t.Run("eviction refused", func(t *testing.T) {
		_, lines, err := run(t, "1", "quicbaas-1.crt\n", true)
		if err == nil {
			t.Fatal("renewal succeeded although the eviction was refused")
		}
		for _, line := range lines {
			if strings.Contains(" "+line+" ", " wait ") {
				t.Errorf("renewal went on after a refused eviction: %q", line)
			}
		}
	})

	t.Run("bad replica", func(t *testing.T) {
		_, lines, err := run(t, "3", "quicbaas-1.crt\n", false)
		if err == nil {
			t.Fatal("renewal accepted replica 3")
		}
		if lines != nil {
			t.Errorf("kubectl ran for an unknown replica: %q", lines)
		}
	})
}
