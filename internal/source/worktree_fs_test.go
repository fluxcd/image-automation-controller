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
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

func TestWorktreeFS_Remove(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(g *WithT, root string) (target string)
		wantErr    bool
		wantAbsent []string
		wantExist  []string
	}{
		{
			name: "regular file",
			setup: func(g *WithT, root string) string {
				g.Expect(os.WriteFile(filepath.Join(root, "file.yaml"), []byte("a"), 0o644)).To(Succeed())
				return "file.yaml"
			},
			wantAbsent: []string{"file.yaml"},
		},
		{
			name: "symlink keeps its target",
			setup: func(g *WithT, root string) string {
				g.Expect(os.MkdirAll(filepath.Join(root, "deploy", "_stacks"), 0o755)).To(Succeed())
				g.Expect(os.MkdirAll(filepath.Join(root, "deploy", "app"), 0o755)).To(Succeed())
				g.Expect(os.WriteFile(filepath.Join(root, "deploy", "_stacks", "config.yaml"), []byte("a"), 0o644)).To(Succeed())
				g.Expect(os.Symlink("../_stacks/config.yaml", filepath.Join(root, "deploy", "app", "config.yaml"))).To(Succeed())
				return "deploy/app/config.yaml"
			},
			wantAbsent: []string{"deploy/app/config.yaml"},
			wantExist:  []string{"deploy/_stacks/config.yaml"},
		},
		{
			name: "symlink with absent target",
			setup: func(g *WithT, root string) string {
				g.Expect(os.Symlink("missing.yaml", filepath.Join(root, "link.yaml"))).To(Succeed())
				return "link.yaml"
			},
			wantAbsent: []string{"link.yaml"},
		},
		{
			name: "absent path is a no-op",
			setup: func(g *WithT, root string) string {
				return "does/not/exist.yaml"
			},
		},
		{
			name: "root cannot be removed",
			setup: func(g *WithT, root string) string {
				return "."
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			root := t.TempDir()
			target := tt.setup(g, root)

			err := newWorktreeFS(root).Remove(target)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			for _, p := range tt.wantAbsent {
				_, err := os.Lstat(filepath.Join(root, p))
				g.Expect(os.IsNotExist(err)).To(BeTrue(), "expected %s to be absent", p)
			}
			for _, p := range tt.wantExist {
				_, err := os.Lstat(filepath.Join(root, p))
				g.Expect(err).ToNot(HaveOccurred(), "expected %s to exist", p)
			}
		})
	}
}

func TestWorktreeFS_Remove_cannotEscapeRoot(t *testing.T) {
	g := NewWithT(t)

	base := t.TempDir()
	root := filepath.Join(base, "root")
	g.Expect(os.MkdirAll(root, 0o755)).To(Succeed())
	outside := filepath.Join(base, "escape.yaml")
	g.Expect(os.WriteFile(outside, []byte("a"), 0o644)).To(Succeed())

	// The relative path is clamped to the root, like BoundOS does, and
	// the resulting absent path is a no-op.
	g.Expect(newWorktreeFS(root).Remove("../escape.yaml")).To(Succeed())
	_, err := os.Lstat(outside)
	g.Expect(err).ToNot(HaveOccurred(), "expected file outside the root to be untouched")
}

func TestWorktreeFS_RemoveAll(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(g *WithT, root string) (target string)
		wantAbsent []string
		wantExist  []string
	}{
		{
			name: "directory with contents",
			setup: func(g *WithT, root string) string {
				g.Expect(os.MkdirAll(filepath.Join(root, "dir", "sub"), 0o755)).To(Succeed())
				g.Expect(os.WriteFile(filepath.Join(root, "dir", "sub", "file.yaml"), []byte("a"), 0o644)).To(Succeed())
				return "dir"
			},
			wantAbsent: []string{"dir"},
		},
		{
			name: "symlink keeps its target",
			setup: func(g *WithT, root string) string {
				g.Expect(os.WriteFile(filepath.Join(root, "config.yaml"), []byte("a"), 0o644)).To(Succeed())
				g.Expect(os.Symlink("config.yaml", filepath.Join(root, "link.yaml"))).To(Succeed())
				return "link.yaml"
			},
			wantAbsent: []string{"link.yaml"},
			wantExist:  []string{"config.yaml"},
		},
		{
			name: "absent path is a no-op",
			setup: func(g *WithT, root string) string {
				return "does/not/exist.yaml"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			root := t.TempDir()
			target := tt.setup(g, root)

			g.Expect(newWorktreeFS(root).RemoveAll(target)).To(Succeed())
			for _, p := range tt.wantAbsent {
				_, err := os.Lstat(filepath.Join(root, p))
				g.Expect(os.IsNotExist(err)).To(BeTrue(), "expected %s to be absent", p)
			}
			for _, p := range tt.wantExist {
				_, err := os.Lstat(filepath.Join(root, p))
				g.Expect(err).ToNot(HaveOccurred(), "expected %s to exist", p)
			}
		})
	}
}
