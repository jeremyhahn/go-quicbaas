package buildgates

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// imageTagProblems reports why the kustomization does not pin quicbaasd to
// the repository VERSION.
func imageTagProblems(kust k8sObject, version string) []string {
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
		case tag != version:
			problems = append(problems, "quicbaasd image tag "+tag+" is not VERSION "+version)
		}
	}
	if pinned != 1 {
		problems = append(problems, fmt.Sprintf("kustomization pins the quicbaasd image %d times, want exactly once", pinned))
	}
	return problems
}

func TestKubernetesEveryNodeRunsTheOneReleaseTag(t *testing.T) {
	kust, objs := kubernetesManifests(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "VERSION"))
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	for _, p := range imageTagProblems(kust, strings.TrimSpace(string(raw))) {
		t.Error(p)
	}

	// The tag lives in the kustomization alone: an image reference that
	// carries its own tag or digest would escape the pin.
	for _, o := range objs {
		containers, _ := dig(o, "spec", "template", "spec", "containers").([]any)
		for _, c := range containers {
			image, _ := dig(c, "image").(string)
			if image != quicbaasdImage {
				t.Errorf("%s %s: container image %q, want the untagged %q the kustomization pins",
					o.kind(), o.name(), image, quicbaasdImage)
			}
		}
	}
}

func TestImageTagProblemsRejectsLatestAndDrift(t *testing.T) {
	for tag, want := range map[string]int{"latest": 1, "0.1.0": 1, "0.16.0-alpha": 0} {
		kust := k8sObject{"images": []any{map[string]any{"name": quicbaasdImage, "newTag": tag}}}
		if got := len(imageTagProblems(kust, "0.16.0-alpha")); got != want {
			t.Errorf("tag %q: %d problems, want %d", tag, got, want)
		}
	}
	if len(imageTagProblems(k8sObject{}, "0.16.0-alpha")) == 0 {
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

// TestKubernetesNodeConfigLoadsAsRaftNode2 loads the ConfigMap's node config
// through the daemon's own loader with the overrides the start command
// exports in pod quicbaas-1, and holds it to what Raft node 2 of a
// three-member group needs. The validator's raft and server sections must
// accept it; findings about files the pod mounts (certificates, /data) are
// not this test's to make.
