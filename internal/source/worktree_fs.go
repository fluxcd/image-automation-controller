/*
Copyright 2026 The Flux authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package source

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-billy/v5/util"
	extgogit "github.com/go-git/go-git/v5"
	gogitcache "github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"

	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/gogit"
)

// worktreeFS wraps the billy.Filesystem used for the repository worktree
// to work around go-git/go-billy#135: the BoundOS filesystem resolves a
// trailing symlink path component, making Remove and RemoveAll operate
// on the symlink target instead of the symlink itself.
//
// When switching to the push branch, go-git removes worktree files that
// differ between the two branches. With the BoundOS behavior, removing
// a symlink path deletes its target instead, and when the target has
// already been deleted by a preceding change of the same checkout, the
// removal fails with a "no such file or directory" error. This
// permanently fails the reconciliation of an ImageUpdateAutomation with
// a push branch that has fallen behind the checkout ref, until the push
// branch is manually rebased or deleted.
//
// Remove and RemoveAll here operate on the given path itself, never on
// the target it may link to, and treat removal of an already-absent
// path as a no-op, matching `git checkout` semantics.
//
// The upstream fix exists only on the go-billy v6 (pre-release) line
// and won't be backported to v5. This wrapper can be removed once
// go-git and go-billy are bumped to v6.
type worktreeFS struct {
	billy.Filesystem
	root string
}

// newWorktreeFS returns a filesystem rooted at the given path that is
// safe for worktree file removals involving symlinks.
func newWorktreeFS(root string) worktreeFS {
	return worktreeFS{
		Filesystem: osfs.New(root, osfs.WithBoundOS()),
		root:       root,
	}
}

// Remove deletes the named file, symlink or empty directory, without
// following a trailing symlink. Removal of an already-absent path is a
// no-op.
func (fs worktreeFS) Remove(name string) error {
	abs, ok, err := fs.securePath(name)
	if err != nil {
		return err
	}
	if !ok {
		return fs.Filesystem.Remove(name)
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// RemoveAll deletes the named path and any children it contains,
// without following a trailing symlink. Removal of an already-absent
// path is a no-op.
func (fs worktreeFS) RemoveAll(name string) error {
	abs, ok, err := fs.securePath(name)
	if err != nil {
		return err
	}
	if !ok {
		return util.RemoveAll(fs.Filesystem, name)
	}
	return os.RemoveAll(abs)
}

// securePath maps name to an absolute path within the worktree root,
// resolving intermediate symlinks like BoundOS does so relative paths
// cannot escape the root, but keeping the final component unresolved so
// that operations act on a trailing symlink itself rather than on its
// target. It returns false for paths this wrapper does not handle (the
// root itself, absolute paths, or a final ".." component), for which
// callers fall back to the wrapped filesystem.
func (fs worktreeFS) securePath(name string) (string, bool, error) {
	if filepath.IsAbs(name) {
		return "", false, nil
	}
	dir, base := filepath.Split(filepath.Clean(name))
	if base == "" || base == "." || base == ".." {
		return "", false, nil
	}
	parent, err := securejoin.SecureJoin(fs.root, dir)
	if err != nil {
		return "", false, err
	}
	return filepath.Join(parent, base), true, nil
}

// diskStorageClientOptions returns client options for storing the cloned
// repository on disk at the given path, equivalent to
// gogit.WithDiskStorage, but with the worktree filesystem wrapped in
// worktreeFS.
func diskStorageClientOptions(path string) ([]gogit.ClientOption, error) {
	securePath, err := git.SecurePath(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path %s: %w", path, err)
	}
	dot := osfs.New(filepath.Join(securePath, extgogit.GitDirName), osfs.WithBoundOS())
	return []gogit.ClientOption{
		gogit.WithStorer(filesystem.NewStorage(dot, gogitcache.NewObjectLRUDefault())),
		gogit.WithWorkTreeFS(newWorktreeFS(securePath)),
	}, nil
}
