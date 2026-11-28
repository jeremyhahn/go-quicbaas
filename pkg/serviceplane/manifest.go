package serviceplane

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-qrdb/pkg/role"
)

// Manifest is what a service is: the image it runs, the policies it runs
// under, what it is given and where it may run. It is signed as a whole
// (SignedManifest) and stored as signed; a change is a redeploy.
type Manifest struct {
	// Tenant scopes the service. It is also the SPIFFE trust domain of the
	// workload identity deploy grants.
	Tenant string `json:"tenant"`
	// Name is the service's registered name, unique within the tenant: a DNS
	// label.
	Name string `json:"name"`
	// Image is the image the service runs, pinned by digest.
	Image Image `json:"image"`
	// Policy names the sandbox, mount and network policies.
	Policy PolicyRefs `json:"policy"`
	// Resources bounds what the service may use.
	Resources Resources `json:"resources"`
	// Volumes the node agent attaches at launch.
	Volumes []Volume `json:"volumes,omitempty"`
	// Ports the service listens on.
	Ports []Port `json:"ports,omitempty"`
	// Role is the go-qrdb built-in role the service runs under; deploy grants
	// its RBAC from it.
	Role string `json:"role"`
	// Placement constrains the nodes the service may run on.
	Placement Placement `json:"placement"`
	// Class is the privilege class the service runs in. Empty means
	// standard. The broker class is refused until its grants exist.
	Class Class `json:"class,omitempty"`
}

// Class is a service's privilege class.
type Class string

const (
	// ClassStandard is the default class: an unprivileged tenant service.
	ClassStandard Class = "standard"
	// ClassBroker is the privileged broker class.
	ClassBroker Class = "broker"
)

// Image pins the service's image.
type Image struct {
	// Reference is the repository the image is pulled from.
	Reference string `json:"reference"`
	// Digest is the content digest, "<algorithm>:<hex>".
	Digest string `json:"digest"`
	// Provenance references the image's provenance attestation.
	Provenance string `json:"provenance"`
}

// PolicyRefs names the policies the service runs under.
type PolicyRefs struct {
	Sandbox string `json:"sandbox"`
	Mount   string `json:"mount"`
	Network string `json:"network"`
}

// Resources bounds a service's CPU and memory.
type Resources struct {
	MilliCPU    int64 `json:"milli_cpu"`
	MemoryBytes int64 `json:"memory_bytes"`
}

// Volume is a volume attached at launch.
type Volume struct {
	Name      string `json:"name"`
	MountPath string `json:"mount_path"`
	SizeBytes int64  `json:"size_bytes"`
}

// Port is a port the service listens on.
type Port struct {
	Name     string `json:"name"`
	Port     uint16 `json:"port"`
	Protocol string `json:"protocol"`
}

// Placement constrains where a service runs: every label in NodeSelector must
// match the node's.
type Placement struct {
	NodeSelector map[string]string `json:"node_selector,omitempty"`
}

var (
	dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	digests  = map[string]int{"sha256": 64, "sha384": 96, "sha512": 128}
	hexLower = regexp.MustCompile(`^[0-9a-f]+$`)
)

// Validate reports the first field that makes the manifest unusable.
func (m *Manifest) Validate() error {
	switch {
	case strings.TrimSpace(m.Tenant) == "":
		return &ManifestError{Field: "tenant", Reason: "is empty"}
	case strings.ContainsAny(m.Tenant, "/ \t\n"):
		return &ManifestError{Field: "tenant", Reason: "contains a slash or a space"}
	case !dnsLabel.MatchString(m.Name):
		return &ManifestError{Field: "name", Reason: "is not a DNS label"}
	case strings.TrimSpace(m.Image.Reference) == "":
		return &ManifestError{Field: "image.reference", Reason: "is empty"}
	case strings.TrimSpace(m.Image.Provenance) == "":
		return &ManifestError{Field: "image.provenance", Reason: "is empty"}
	case m.Policy.Sandbox == "" || m.Policy.Mount == "" || m.Policy.Network == "":
		return &ManifestError{Field: "policy", Reason: "sandbox, mount and network policies are all required"}
	case m.Resources.MilliCPU <= 0:
		return &ManifestError{Field: "resources.milli_cpu", Reason: "must be positive"}
	case m.Resources.MemoryBytes <= 0:
		return &ManifestError{Field: "resources.memory_bytes", Reason: "must be positive"}
	case m.Class != "" && m.Class != ClassStandard && m.Class != ClassBroker:
		return &ManifestError{Field: "class", Reason: "must be standard or broker"}
	}
	if err := validateDigest(m.Image.Digest); err != nil {
		return err
	}
	if err := validateRole(m.Role); err != nil {
		return err
	}
	if err := validateVolumes(m.Volumes); err != nil {
		return err
	}
	return validatePorts(m.Ports)
}

func validateDigest(d string) error {
	alg, hex, ok := strings.Cut(d, ":")
	want, known := digests[alg]
	if !ok || !known || len(hex) != want || !hexLower.MatchString(hex) {
		return &ManifestError{Field: "image.digest", Reason: "is not a sha256, sha384 or sha512 digest"}
	}
	return nil
}

// validateRole admits a go-qrdb built-in role other than the Security
// Officer's: a service does cryptographic work, which the SO role is denied.
func validateRole(name string) error {
	if !role.IsSystemRoleName(name) {
		return &ManifestError{Field: "role", Reason: "is not a built-in role"}
	}
	if name == role.RoleNameSO {
		return &ManifestError{Field: "role", Reason: "a service cannot run as the Security Officer"}
	}
	return nil
}

func validateVolumes(vols []Volume) error {
	seen := make(map[string]bool, len(vols))
	for _, v := range vols {
		switch {
		case !dnsLabel.MatchString(v.Name):
			return &ManifestError{Field: "volumes.name", Reason: "is not a DNS label"}
		case seen[v.Name]:
			return &ManifestError{Field: "volumes.name", Reason: "is repeated"}
		case !strings.HasPrefix(v.MountPath, "/"):
			return &ManifestError{Field: "volumes.mount_path", Reason: "is not absolute"}
		case v.SizeBytes <= 0:
			return &ManifestError{Field: "volumes.size_bytes", Reason: "must be positive"}
		}
		seen[v.Name] = true
	}
	return nil
}

func validatePorts(ports []Port) error {
	seen := make(map[string]bool, len(ports))
	for _, p := range ports {
		key := p.Protocol + "/" + strconv.Itoa(int(p.Port))
		switch {
		case !dnsLabel.MatchString(p.Name):
			return &ManifestError{Field: "ports.name", Reason: "is not a DNS label"}
		case p.Port == 0:
			return &ManifestError{Field: "ports.port", Reason: "must be positive"}
		case p.Protocol != "tcp" && p.Protocol != "udp":
			return &ManifestError{Field: "ports.protocol", Reason: "must be tcp or udp"}
		case seen[key]:
			return &ManifestError{Field: "ports.port", Reason: "is repeated"}
		}
		seen[key] = true
	}
	return nil
}

// manifestDomain separates a manifest signature from every other signature
// the same key makes.
const manifestDomain = "quicbaas.serviceplane.manifest.v1\n"

// signingPayload is the exact byte string a manifest signature covers: the
// domain, then the manifest's JSON encoding. encoding/json writes struct
// fields in declaration order and map keys sorted, so the encoding is stable.
func signingPayload(m *Manifest) ([]byte, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return nil, &ManifestError{Field: "manifest", Reason: "does not encode: " + err.Error()}
	}
	return append([]byte(manifestDomain), body...), nil
}
