// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package containerruntime

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"

	securejoin "github.com/cyphar/filepath-securejoin"
)

// initVolumes makes the directory of each named volume a container mounts.
// A named volume that is new or empty is filled once from the image's
// content at its target in the unpacked rootfsDir, keeping owners, modes and
// times, as Docker and Podman do. A volume with content is never changed.
// Initialization is serialized across the runtime, so two containers created
// at once never fill the same volume twice.
func (r *Runtime) initVolumes(rootfsDir string, volumes []namedVolume) error {
	if len(volumes) == 0 {
		return nil
	}
	r.volumeMu.Lock()
	defer r.volumeMu.Unlock()
	for _, v := range volumes {
		if err := r.initVolume(rootfsDir, v); err != nil {
			return &RuntimeError{Operation: "initialize volume " + v.Name, Err: err}
		}
	}
	return nil
}

// initVolume makes the directory of one named volume. A volume that holds
// any entry is left as it is. Otherwise, when the image has a directory at
// the volume's target, its content is copied into a temporary directory
// under VolumePath, whose leading dot no volume name can have, and renamed
// into place; when it has none, the volume is an empty directory.
func (r *Runtime) initVolume(rootfsDir string, v namedVolume) error {
	entries, err := os.ReadDir(v.Dir)
	switch {
	case err == nil && len(entries) > 0:
		return nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	}

	src, err := securejoin.SecureJoin(rootfsDir, v.Target)
	if err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err != nil || !info.IsDir() {
		return os.MkdirAll(v.Dir, 0o750)
	}

	if err := os.MkdirAll(r.config.VolumePath, 0o750); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(r.config.VolumePath, "."+v.Name+".init-")
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = os.RemoveAll(tmp)
		}
	}()
	if err := copyVolumeTree(src, tmp); err != nil {
		return err
	}
	// os.Rename refuses an existing directory as its target, so an empty
	// volume directory is removed first; os.Remove never removes content.
	if err := os.Remove(v.Dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(tmp, v.Dir); err != nil {
		return err
	}
	renamed = true
	return nil
}

// copyVolumeTree copies the tree under srcDir into the empty dstDir, keeping
// owners, modes and modification times. Directories, regular files and
// symbolic links are copied; device nodes, fifos and sockets are not. Both
// trees are opened as roots, so no path in the image can reach outside them.
func copyVolumeTree(srcDir, dstDir string) error {
	src, err := os.OpenRoot(srcDir)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenRoot(dstDir)
	if err != nil {
		return err
	}
	defer dst.Close()

	type dirAttrs struct {
		name string
		info fs.FileInfo
	}
	var dirs []dirAttrs
	err = fs.WalkDir(src.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case name == ".":
			dirs = append(dirs, dirAttrs{name, info})
		case d.IsDir():
			if err := dst.Mkdir(name, 0o700); err != nil {
				return err
			}
			dirs = append(dirs, dirAttrs{name, info})
		case d.Type().IsRegular():
			if err := copyVolumeFile(src, dst, name); err != nil {
				return err
			}
			return applyVolumeAttrs(dst, name, info)
		case d.Type()&fs.ModeSymlink != 0:
			target, err := src.Readlink(name)
			if err != nil {
				return err
			}
			if err := dst.Symlink(target, name); err != nil {
				return err
			}
			return applyVolumeAttrs(dst, name, info)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// A directory's attributes are set after its children are written,
	// deepest first, so its mode and time are the image's at the end.
	for _, dir := range slices.Backward(dirs) {
		if err := applyVolumeAttrs(dst, dir.name, dir.info); err != nil {
			return err
		}
	}
	return nil
}

// copyVolumeFile copies the content of the regular file name from src to a
// new file of the same name in dst.
func copyVolumeFile(src, dst *os.Root, name string) error {
	in, err := src.Open(name)
	if err != nil {
		return err
	}
	out, err := dst.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = in.Close()
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close(), in.Close())
}

// applyVolumeAttrs gives the copy name in dst the owner of info and, unless
// it is a symbolic link, its mode bits and modification time. The owner is
// set first because a change of owner clears the setuid and setgid bits.
func applyVolumeAttrs(dst *os.Root, name string, info fs.FileInfo) error {
	if uid, gid, ok := fileOwnerIDs(info); ok {
		cur, err := dst.Lstat(name)
		if err != nil {
			return err
		}
		curUID, curGID, curOK := fileOwnerIDs(cur)
		if !curOK || curUID != uid || curGID != gid {
			if err := dst.Lchown(name, uid, gid); err != nil {
				return err
			}
		}
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil
	}
	if err := dst.Chmod(name, info.Mode()&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky)); err != nil {
		return err
	}
	return dst.Chtimes(name, info.ModTime(), info.ModTime())
}
