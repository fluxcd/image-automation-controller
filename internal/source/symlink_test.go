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
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	extgogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1"
	"github.com/fluxcd/image-automation-controller/internal/testutil"
)

// treeEntry is a file, or a symlink when link is set.
type treeEntry struct {
	content string
	link    string
}

func TestSourceManager_CheckoutSource_symlinks(t *testing.T) {
	tests := []struct {
		name     string
		checkout map[string]treeEntry
		push     map[string]treeEntry
	}{
		{
			// https://github.com/fluxcd/image-automation-controller/issues/1037
			name: "symlink replaced by file whose target is deleted",
			checkout: map[string]treeEntry{
				"deploy/_stacks/config.yaml": {content: "v2"},
				"deploy/app/config.yaml":     {link: "../_stacks/config.yaml"},
			},
			push: map[string]treeEntry{
				"deploy/app/config.yaml": {content: "v1"},
			},
		},
		{
			name: "symlink deleted, target kept",
			checkout: map[string]treeEntry{
				"shared.yaml": {content: "shared"},
				"link.yaml":   {link: "shared.yaml"},
			},
			push: map[string]treeEntry{
				"shared.yaml": {content: "shared"},
			},
		},
		{
			name: "symlink to .git/config deleted",
			checkout: map[string]treeEntry{
				"app.yaml": {content: "app"},
				"git.yaml": {link: ".git/config"},
			},
			push: map[string]treeEntry{
				"app.yaml": {content: "app"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.TODO()

			gitServer := testutil.SetUpGitTestServer(g)
			t.Cleanup(func() {
				g.Expect(os.RemoveAll(gitServer.Root())).To(Succeed())
				gitServer.StopHTTP()
			})

			branch := "main"
			pushBranch := "auto"
			repoPath := "/config-" + rand.String(5) + ".git"
			repo := testutil.InitGitRepo(g, gitServer, "testdata/appconfig", branch, repoPath)
			setBranch(g, repo.Storer, branch, tt.checkout)
			setBranch(g, repo.Storer, pushBranch, tt.push)

			gitRepo := &sourcev1.GitRepository{}
			gitRepo.Name = "test-repo"
			gitRepo.Namespace = "test-ns"
			gitRepo.Spec = sourcev1.GitRepositorySpec{
				URL:       gitServer.HTTPAddressWithCredentials() + repoPath,
				Reference: &sourcev1.GitRepositoryRef{Branch: branch},
			}

			updateAuto := &imagev1.ImageUpdateAutomation{}
			updateAuto.Name = "test-update"
			updateAuto.Namespace = gitRepo.Namespace
			updateAuto.Spec = imagev1.ImageUpdateAutomationSpec{
				SourceRef: imagev1.CrossNamespaceSourceReference{
					Kind: sourcev1.GitRepositoryKind,
					Name: gitRepo.Name,
				},
				GitSpec: &imagev1.GitSpec{
					Push: &imagev1.PushSpec{Branch: pushBranch},
				},
			}

			kClient := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(gitRepo, updateAuto).
				Build()

			sm, err := NewSourceManager(ctx, kClient, updateAuto, WithSourceOptionGitAllBranchReferences())
			g.Expect(err).ToNot(HaveOccurred())
			t.Cleanup(func() {
				g.Expect(sm.Cleanup()).To(Succeed())
			})

			_, err = sm.CheckoutSource(ctx)
			g.Expect(err).ToNot(HaveOccurred())

			r, err := extgogit.PlainOpen(sm.WorkDirectory())
			g.Expect(err).ToNot(HaveOccurred())
			head, err := r.Head()
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(head.Name().Short()).To(Equal(pushBranch))

			g.Expect(filepath.Join(sm.WorkDirectory(), ".git", "config")).To(BeARegularFile())
			g.Expect(readTree(g, sm.WorkDirectory())).To(Equal(tt.push))

			wt, err := r.Worktree()
			g.Expect(err).ToNot(HaveOccurred())
			status, err := wt.Status()
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(status.IsClean()).To(BeTrue(), "worktree not clean: %v", status)
		})
	}
}

func readTree(g *WithT, dir string) map[string]treeEntry {
	got := map[string]treeEntry{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			got[filepath.ToSlash(rel)] = treeEntry{link: target}
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(rel)] = treeEntry{content: string(b)}
		return nil
	})
	g.Expect(err).ToNot(HaveOccurred())
	return got
}

func setBranch(g *WithT, st storer.Storer, branch string, tree map[string]treeEntry) {
	sig := object.Signature{Name: "test", Email: "test@example.com", When: time.Now()}
	commit := &object.Commit{Author: sig, Committer: sig, Message: branch, TreeHash: writeTree(g, st, tree)}
	obj := st.NewEncodedObject()
	g.Expect(commit.Encode(obj)).To(Succeed())
	hash, err := st.SetEncodedObject(obj)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(st.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), hash))).To(Succeed())
}

func writeTree(g *WithT, st storer.Storer, tree map[string]treeEntry) plumbing.Hash {
	subtrees := map[string]map[string]treeEntry{}
	var entries []object.TreeEntry
	for p, e := range tree {
		if dir, rest, ok := strings.Cut(p, "/"); ok {
			if subtrees[dir] == nil {
				subtrees[dir] = map[string]treeEntry{}
			}
			subtrees[dir][rest] = e
			continue
		}
		mode, data := filemode.Regular, e.content
		if e.link != "" {
			mode, data = filemode.Symlink, e.link
		}
		obj := st.NewEncodedObject()
		obj.SetType(plumbing.BlobObject)
		w, err := obj.Writer()
		g.Expect(err).ToNot(HaveOccurred())
		_, err = w.Write([]byte(data))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(w.Close()).To(Succeed())
		hash, err := st.SetEncodedObject(obj)
		g.Expect(err).ToNot(HaveOccurred())
		entries = append(entries, object.TreeEntry{Name: p, Mode: mode, Hash: hash})
	}
	for dir, sub := range subtrees {
		entries = append(entries, object.TreeEntry{Name: dir, Mode: filemode.Dir, Hash: writeTree(g, st, sub)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	obj := st.NewEncodedObject()
	g.Expect((&object.Tree{Entries: entries}).Encode(obj)).To(Succeed())
	hash, err := st.SetEncodedObject(obj)
	g.Expect(err).ToNot(HaveOccurred())
	return hash
}
