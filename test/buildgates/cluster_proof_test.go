package buildgates

// The 3-node deployment proof (`make integration-test-cluster-failover`) runs
// only on a Docker host. These gates hold its shape from the files alone: the
// cluster nodes run one image built once with the release version compiled
// in, and the failover target stops the leader the first proof test recorded
// before the second one checks the survivors.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var clusterNodeServices = []string{"quicbaas-node1", "quicbaas-node2", "quicbaas-node3", "quicbaas-node4"}

func readRepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(append([]string{repoRoot}, parts...)...))
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Join(parts...), err)
	}
	return string(raw)
}

// TestClusterComposeNodesRunOneImage: every cluster node runs the one image
// node1 builds, tagged by QUICBAAS_CLUSTER_IMAGE, so the four nodes cannot
// run four separately built images.
func TestClusterComposeNodesRunOneImage(t *testing.T) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(readRepoFile(t, "test", "integration", "docker-compose.cluster.yml")), &doc); err != nil {
		t.Fatalf("decode docker-compose.cluster.yml: %v", err)
	}
	services, ok := doc["services"].(map[string]any)
	if !ok {
		t.Fatal("docker-compose.cluster.yml has no services map")
	}

	var image string
	var builders []string
	for _, name := range clusterNodeServices {
		svc, ok := services[name].(map[string]any)
		if !ok {
			t.Fatalf("service %s is missing", name)
		}
		img, _ := svc["image"].(string)
		if img == "" {
			t.Errorf("%s sets no image", name)
		} else if !strings.Contains(img, "QUICBAAS_CLUSTER_IMAGE") {
			t.Errorf("%s image %q is not taken from QUICBAAS_CLUSTER_IMAGE", name, img)
		}
		if image == "" {
			image = img
		} else if img != image {
			t.Errorf("%s runs image %q, node1 runs %q: the nodes must run one image", name, img, image)
		}

		build, hasBuild := svc["build"]
		if !hasBuild {
			if svc["pull_policy"] != "never" {
				t.Errorf("%s has no build and must set pull_policy: never, got %v", name, svc["pull_policy"])
			}
			continue
		}
		builders = append(builders, name)
		buildMap, _ := build.(map[string]any)
		args, _ := buildMap["args"].(map[string]any)
		for _, arg := range []string{"QUICBAAS_VERSION", "GIT_COMMIT"} {
			if _, ok := args[arg]; !ok {
				t.Errorf("%s build.args does not pass %s", name, arg)
			}
		}
	}
	if len(builders) != 1 || builders[0] != "quicbaas-node1" {
		t.Errorf("only quicbaas-node1 may build the image, builders are %v", builders)
	}
}

// TestClusterNodeImageCarriesTheVersion: the node image compiles the release
// version and commit into quicbaasd, on the coverage and the plain build.
func TestClusterNodeImageCarriesTheVersion(t *testing.T) {
	src := readRepoFile(t, "test", "integration", "Dockerfile.node")
	for _, arg := range []string{"ARG QUICBAAS_VERSION", "ARG GIT_COMMIT"} {
		if !strings.Contains(src, arg) {
			t.Errorf("Dockerfile.node does not declare %s", arg)
		}
	}

	joined := strings.ReplaceAll(src, "\\\n", " ")
	builds := 0
	for _, segment := range strings.Split(joined, "go build")[1:] {
		end := strings.Index(segment, "-o bin/")
		if end < 0 || !strings.HasPrefix(segment[end:], "-o bin/quicbaasd ") {
			continue
		}
		builds++
		cmd := segment[:end]
		for _, flag := range []string{
			"pkg/version.Version=${QUICBAAS_VERSION}",
			"pkg/version.GitCommit=${GIT_COMMIT}",
		} {
			if !strings.Contains(cmd, flag) {
				t.Errorf("quicbaasd build %d does not set %s", builds, flag)
			}
		}
	}
	if builds != 2 {
		t.Errorf("Dockerfile.node has %d quicbaasd builds, want 2 (coverage and plain)", builds)
	}
}

// TestClusterFailoverTargetStopsTheLeader: the failover target runs the
// formation proof, stops the leader it recorded, then checks the survivors.
func TestClusterFailoverTargetStopsTheLeader(t *testing.T) {
	const target = "integration-test-cluster-failover:"
	makefile := readRepoFile(t, "Makefile")
	start := strings.Index(makefile, "\n"+target)
	if start < 0 {
		t.Fatalf("Makefile has no %s target", target)
	}
	recipe := makefile[start+1+len(target):]
	if end := strings.Index(recipe, "\n\n"); end >= 0 {
		recipe = recipe[:end]
	}

	pos := 0
	for _, step := range []string{
		"-tags 'integration deployment_proof'",
		"-run '^TestDeploymentProof_OneBuildFormsAndServes$$'",
		"stop quicbaas-node$$DEPLOYMENT_PROOF_LEADER_NODE_ID",
		"-run '^TestDeploymentProof_AfterLeaderStop$$'",
	} {
		i := strings.Index(recipe[pos:], step)
		if i < 0 {
			t.Errorf("%s recipe lacks %q after the previous step", target, step)
			continue
		}
		pos += i + len(step)
	}
	for _, env := range []string{"QUICBAAS_EXPECTED_VERSION", "QUICBAAS_DEPLOYMENT_PROOF_STATE", "QUICBAAS_CLUSTER_IMAGE"} {
		if !strings.Contains(recipe, env) {
			t.Errorf("%s recipe does not set %s", target, env)
		}
	}
}
