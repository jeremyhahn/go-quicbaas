package provenance

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// In-toto and SLSA type URIs.
const (
	StatementType            = "https://in-toto.io/Statement/v1"
	PredicateTypeSLSAv1      = "https://slsa.dev/provenance/v1"
	BuildTypeSoftwareFactory = "https://github.com/jeremyhahn/go-quicbaas/sdlc/image-build/v1"
)

// ResourceDescriptor names an artifact by digest.
type ResourceDescriptor struct {
	Name   string            `json:"name,omitempty"`
	URI    string            `json:"uri,omitempty"`
	Digest map[string]string `json:"digest,omitempty"`
}

// Statement is an in-toto v1 statement.
type Statement struct {
	Type          string               `json:"_type"`
	Subject       []ResourceDescriptor `json:"subject"`
	PredicateType string               `json:"predicateType"`
	Predicate     json.RawMessage      `json:"predicate"`
}

// SLSAProvenance is the SLSA v1 provenance predicate.
type SLSAProvenance struct {
	BuildDefinition BuildDefinition `json:"buildDefinition"`
	RunDetails      RunDetails      `json:"runDetails"`
}

// BuildDefinition is what the build ran.
type BuildDefinition struct {
	BuildType            string               `json:"buildType"`
	ExternalParameters   map[string]any       `json:"externalParameters"`
	ResolvedDependencies []ResourceDescriptor `json:"resolvedDependencies,omitempty"`
}

// RunDetails is who ran the build and when.
type RunDetails struct {
	Builder  Builder       `json:"builder"`
	Metadata BuildMetadata `json:"metadata,omitempty"`
}

// Builder identifies the build platform.
type Builder struct {
	ID string `json:"id"`
}

// BuildMetadata identifies one build run.
type BuildMetadata struct {
	InvocationID string     `json:"invocationId,omitempty"`
	StartedOn    *time.Time `json:"startedOn,omitempty"`
	FinishedOn   *time.Time `json:"finishedOn,omitempty"`
}

// NewImageStatement returns the statement that image name, pinned at
// digest ("sha256:<hex>"), was built as pred describes.
func NewImageStatement(name, digest string, pred *SLSAProvenance) (*Statement, error) {
	if pred == nil {
		return nil, errors.New("provenance: a SLSA predicate is required")
	}
	alg, hex, err := splitDigest(digest)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(pred)
	if err != nil {
		return nil, fmt.Errorf("provenance: encode predicate: %w", err)
	}
	stmt := &Statement{
		Type:          StatementType,
		Subject:       []ResourceDescriptor{{Name: name, Digest: map[string]string{alg: hex}}},
		PredicateType: PredicateTypeSLSAv1,
		Predicate:     raw,
	}
	return stmt, stmt.Validate()
}

// Validate checks the statement is an in-toto v1 statement with SLSA v1
// provenance, a subject and a builder.
func (s *Statement) Validate() error {
	if s.Type != StatementType {
		return fmt.Errorf("provenance: statement type %q is not %q", s.Type, StatementType)
	}
	if s.PredicateType != PredicateTypeSLSAv1 {
		return fmt.Errorf("provenance: predicate type %q is not %q", s.PredicateType, PredicateTypeSLSAv1)
	}
	if len(s.Subject) == 0 {
		return errors.New("provenance: the statement has no subject")
	}
	pred, err := s.SLSA()
	if err != nil {
		return err
	}
	if pred.RunDetails.Builder.ID == "" || pred.BuildDefinition.BuildType == "" {
		return errors.New("provenance: the predicate names no builder or build type")
	}
	return nil
}

// SLSA decodes the statement's SLSA v1 predicate.
func (s *Statement) SLSA() (*SLSAProvenance, error) {
	var pred SLSAProvenance
	if err := json.Unmarshal(s.Predicate, &pred); err != nil {
		return nil, fmt.Errorf("provenance: decode SLSA predicate: %w", err)
	}
	return &pred, nil
}

// Covers reports whether one of the statement's subjects is digest
// ("sha256:<hex>").
func (s *Statement) Covers(digest string) bool {
	alg, hex, err := splitDigest(digest)
	if err != nil {
		return false
	}
	for _, subject := range s.Subject {
		if strings.EqualFold(subject.Digest[alg], hex) {
			return true
		}
	}
	return false
}

func splitDigest(digest string) (alg, hex string, err error) {
	alg, hex, ok := strings.Cut(digest, ":")
	if !ok || alg == "" || hex == "" {
		return "", "", fmt.Errorf("provenance: %q is not an <algorithm>:<hex> digest", digest)
	}
	return alg, hex, nil
}
