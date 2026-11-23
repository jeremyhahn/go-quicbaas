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

package containerruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// containerRecordFile names the record each container's bundle keeps of the
// container. The Runtime tracks containers in memory; the record is how a
// later process of the runtime finds what an earlier one left behind.
const containerRecordFile = "container.json"

// LeftoverContainer is a container an earlier process of this runtime created
// under the same root and never removed: the agent crashed or was killed
// while a job ran. Its init may still be running.
type LeftoverContainer struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Image     string            `json:"image"`
	CreatedAt time.Time         `json:"created_at"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// bundlesDir is the directory holding one bundle per container.
func (r *Runtime) bundlesDir() string {
	return filepath.Join(r.config.RunRoot, "bundles")
}

// writeContainerRecord records a container in its bundle.
func writeContainerRecord(bundleDir string, record LeftoverContainer) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bundleDir, containerRecordFile), data, 0600)
}

// ListLeftovers enumerates the containers an earlier process of this runtime
// left under its root, oldest first. A bundle without a record is one whose
// create never finished; nothing of it runs and nothing can claim it, so it
// is skipped. A bundle directory or record that cannot be read is an error:
// a sweep that skipped it would report itself exact when it is not.
func (r *Runtime) ListLeftovers() ([]LeftoverContainer, error) {
	entries, err := os.ReadDir(r.bundlesDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, &RuntimeError{Operation: "list leftover containers", Err: err}
	}

	var leftovers []LeftoverContainer
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if _, ok := r.containers.Load(id); ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.bundlesDir(), id, containerRecordFile))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, &RuntimeError{Operation: "list leftover containers", Err: err}
		}
		var record LeftoverContainer
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, &RuntimeError{Operation: "list leftover containers", Err: fmt.Errorf("bundle %s: %w", id, err)}
		}
		if record.ID != id {
			return nil, &RuntimeError{Operation: "list leftover containers",
				Err: fmt.Errorf("bundle %s records container %q", id, record.ID)}
		}
		leftovers = append(leftovers, record)
	}
	slices.SortFunc(leftovers, func(a, b LeftoverContainer) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return leftovers, nil
}

// ReclaimLeftover kills and removes a container an earlier process left:
// its engine state, its root filesystem and its bundle. Each step finds
// nothing left when an earlier reclaim already did it, so a reclaim that
// failed part way can be retried.
func (r *Runtime) ReclaimLeftover(id string) error {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id {
		return NewContainerError(id, "reclaim", ErrContainerNotFound)
	}
	if _, ok := r.containers.Load(id); ok {
		return NewContainerError(id, "reclaim", errors.New("the container belongs to this process"))
	}

	r.logger.Info("reclaiming a container left by an earlier process", "id", id)
	if err := r.backends.engine.Reclaim(id); err != nil {
		return NewContainerError(id, "reclaim", err)
	}
	if err := r.backends.rootfs.Remove(id); err != nil {
		return NewContainerError(id, "reclaim", err)
	}
	if err := os.RemoveAll(filepath.Join(r.bundlesDir(), id)); err != nil {
		return NewContainerError(id, "reclaim", err)
	}
	return nil
}
