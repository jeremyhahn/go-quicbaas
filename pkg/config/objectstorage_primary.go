package config

import (
	"sort"
	"strings"
)

// UnknownPrimaryBackendError reports that object_storage.primary_backend names
// a backend that is not configured under object_storage.backends.
type UnknownPrimaryBackendError struct {
	// Name is the configured object_storage.primary_backend.
	Name string

	// Backends are the configured backend names, sorted.
	Backends []string
}

// Error implements error.
func (e *UnknownPrimaryBackendError) Error() string {
	return "object_storage.primary_backend \"" + e.Name +
		"\" is not a configured backend (configured: " + strings.Join(e.Backends, ", ") + ")"
}

// PrimaryBackendUnresolvedError reports that object_storage.primary_backend is
// unset and cannot be inferred, because there is not exactly one configured
// backend.
type PrimaryBackendUnresolvedError struct {
	// Backends are the configured backend names, sorted.
	Backends []string
}

// Error implements error.
func (e *PrimaryBackendUnresolvedError) Error() string {
	if len(e.Backends) == 0 {
		return "object_storage.primary_backend is unset and no backends are configured"
	}
	return "object_storage.primary_backend is unset and several backends are configured (" +
		strings.Join(e.Backends, ", ") + "): set primary_backend to one of them"
}

// ResolvePrimaryBackend returns the backend that receives objects stored
// without an explicit target backend.
//
// An explicit PrimaryBackend wins and must name a configured backend. When it
// is unset and exactly one backend is configured (including the automatic
// in-memory "default" backend), that backend is primary. Any other case is a
// configuration error, so a request never falls back to an empty name.
func (c *ObjectStorageConfig) ResolvePrimaryBackend() (string, error) {
	if c.PrimaryBackend != "" {
		if _, ok := c.Backends[c.PrimaryBackend]; !ok {
			return "", &UnknownPrimaryBackendError{Name: c.PrimaryBackend, Backends: c.backendNames()}
		}
		return c.PrimaryBackend, nil
	}
	names := c.backendNames()
	if len(names) != 1 {
		return "", &PrimaryBackendUnresolvedError{Backends: names}
	}
	return names[0], nil
}

// backendNames returns the configured backend names in sorted order, so error
// messages are stable.
func (c *ObjectStorageConfig) backendNames() []string {
	names := make([]string, 0, len(c.Backends))
	for name := range c.Backends {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
