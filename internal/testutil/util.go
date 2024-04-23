/*
Copyright 2024 The Flux authors

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

package testutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/go-git/go-billy/v5/osfs"
	extgogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/fluxcd/pkg/gittestserver"

	"github.com/fluxcd/image-automation-controller/pkg/update"
)

const (
	signingSecretKey     = "git.asc"
	signingPassphraseKey = "passphrase"
)

func CheckoutBranch(g *WithT, repo *extgogit.Repository, branch string) {
	g.THelper()

	wt, err := repo.Worktree()
	g.Expect(err).ToNot(HaveOccurred())

	err = wt.Checkout(&extgogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
	})
	g.Expect(err).ToNot(HaveOccurred())
}

func ReplaceMarker(path string, policyKey types.NamespacedName) error {
	return ReplaceMarkerWithMarker(path, policyKey, "SETTER_SITE")
}

func ReplaceMarkerWithMarker(path string, policyKey types.NamespacedName, marker string) error {
	filebytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	newfilebytes := bytes.ReplaceAll(filebytes, []byte(marker), []byte(setterRef(policyKey)))
	if err = os.WriteFile(path, newfilebytes, os.FileMode(0666)); err != nil {
		return err
	}
	return nil
}

func setterRef(name types.NamespacedName) string {
	return fmt.Sprintf(`{"%s": "%s:%s"}`, update.SetterShortHand, name.Namespace, name.Name)
}

func CommitInRepo(ctx context.Context, g *WithT, repoURL, branch, remote, msg string, changeFiles func(path string)) plumbing.Hash {
	g.THelper()

	repo, cloneDir, err := Clone(ctx, repoURL, branch, remote)
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { os.RemoveAll(cloneDir) }()

	wt, err := repo.Worktree()
	g.Expect(err).ToNot(HaveOccurred())

	changeFiles(wt.Filesystem.Root())

	id := CommitWorkDir(g, repo, branch, msg)

	origin, err := repo.Remote(remote)
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(origin.Push(&extgogit.PushOptions{
		RemoteName: remote,
		RefSpecs:   []config.RefSpec{config.RefSpec(BranchRefName(branch))},
	})).To(Succeed())
	return id
}

func WaitForNewHead(g *WithT, repo *extgogit.Repository, branch, remote, preChangeHash string) {
	g.THelper()

	var commitToResetTo *object.Commit

	origin, err := repo.Remote(remote)
	g.Expect(err).ToNot(HaveOccurred())

	// Now try to fetch new commits from that remote branch
	g.Eventually(func() bool {
		err := origin.Fetch(&extgogit.FetchOptions{
			RemoteName: remote,
			RefSpecs:   []config.RefSpec{config.RefSpec(BranchRefName(branch))},
		})
		if err != nil {
			return false
		}

		wt, err := repo.Worktree()
		if err != nil {
			return false
		}

		err = wt.Checkout(&extgogit.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(branch),
		})
		if err != nil {
			return false
		}

		remoteHeadRef, err := repo.Head()
		if err != nil {
			return false
		}

		remoteHeadHash := remoteHeadRef.Hash()

		if preChangeHash != remoteHeadHash.String() {
			commitToResetTo, _ = repo.CommitObject(remoteHeadHash)
			return true
		}
		return false
	}, 10*time.Second, time.Second).Should(BeTrue())

	if commitToResetTo != nil {
		wt, err := repo.Worktree()
		g.Expect(err).ToNot(HaveOccurred())

		// New commits in the remote branch -- reset the working tree head
		// to that. Note this does not create a local branch tracking the
		// remote, so it is a detached head.
		g.Expect(wt.Reset(&extgogit.ResetOptions{
			Commit: commitToResetTo.Hash,
			Mode:   extgogit.HardReset,
		})).To(Succeed())
	}
}

// Initialise a git server with a repo including the files in dir.
func InitGitRepo(g *WithT, gitServer *gittestserver.GitServer, fixture, branch, repoPath string) *extgogit.Repository {
	g.THelper()

	workDir, err := securejoin.SecureJoin(gitServer.Root(), repoPath)
	g.Expect(err).ToNot(HaveOccurred())

	repo := InitGitRepoPlain(g, fixture, workDir)

	headRef, err := repo.Head()
	g.Expect(err).ToNot(HaveOccurred())

	ref := plumbing.NewHashReference(
		plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branch)),
		headRef.Hash())

	g.Expect(repo.Storer.SetReference(ref)).ToNot(HaveOccurred())

	return repo
}

func InitGitRepoPlain(g *WithT, fixture, repoPath string) *extgogit.Repository {
	g.THelper()

	wt := osfs.New(repoPath)
	dot := osfs.New(filepath.Join(repoPath, extgogit.GitDirName))
	storer := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	repo, err := extgogit.Init(storer, wt)
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(copyDir(fixture, repoPath)).ToNot(HaveOccurred())

	_ = CommitWorkDir(g, repo, "main", "Initial commit")
	g.Expect(err).ToNot(HaveOccurred())

	return repo
}

func HeadFromBranch(repo *extgogit.Repository, branchName string) (*object.Commit, error) {
	ref, err := repo.Storer.Reference(plumbing.ReferenceName("refs/heads/" + branchName))
	if err != nil {
		return nil, err
	}

	return repo.CommitObject(ref.Hash())
}

func CommitWorkDir(g *WithT, repo *extgogit.Repository, branchName, message string) plumbing.Hash {
	g.THelper()

	wt, err := repo.Worktree()
	g.Expect(err).ToNot(HaveOccurred())

	// Checkout to an existing branch. If this is the first commit,
	// this is a no-op.
	_ = wt.Checkout(&extgogit.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + branchName),
	})

	status, err := wt.Status()
	g.Expect(err).ToNot(HaveOccurred())

	for file := range status {
		wt.Add(file)
	}

	sig := mockSignature(time.Now())
	c, err := wt.Commit(message, &extgogit.CommitOptions{
		All:       true,
		Author:    sig,
		Committer: sig,
	})
	g.Expect(err).ToNot(HaveOccurred())

	_, err = repo.Branch(branchName)
	if err == extgogit.ErrBranchNotFound {
		ref := plumbing.NewHashReference(
			plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branchName)), c)
		err = repo.Storer.SetReference(ref)
	}
	g.Expect(err).ToNot(HaveOccurred())

	// Now the target branch exists, we can checkout to it.
	err = wt.Checkout(&extgogit.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + branchName),
	})
	g.Expect(err).ToNot(HaveOccurred())

	return c
}

func TagCommit(g *WithT, repo *extgogit.Repository, commit plumbing.Hash, annotated bool, tag string, time time.Time) (*plumbing.Reference, error) {
	g.THelper()

	var opts *extgogit.CreateTagOptions
	if annotated {
		opts = &extgogit.CreateTagOptions{
			Tagger:  mockSignature(time),
			Message: "Annotated tag for: " + tag,
		}
	}
	return repo.CreateTag(tag, commit, opts)
}

func copyDir(src string, dest string) error {
	file, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !file.IsDir() {
		return fmt.Errorf("source %q must be a directory", file.Name())
	}

	if err = os.MkdirAll(dest, 0o755); err != nil {
		return err
	}

	files, err := ioutil.ReadDir(src)
	if err != nil {
		return err
	}

	for _, f := range files {
		srcFile := filepath.Join(src, f.Name())
		destFile := filepath.Join(dest, f.Name())

		if f.IsDir() {
			if err = copyDir(srcFile, destFile); err != nil {
				return err
			}
		}

		if !f.IsDir() {
			// ignore symlinks
			if f.Mode()&os.ModeSymlink == os.ModeSymlink {
				continue
			}

			content, err := os.ReadFile(srcFile)
			if err != nil {
				return err
			}

			if err = os.WriteFile(destFile, content, 0o755); err != nil {
				return err
			}
		}
	}

	return nil
}

func BranchRefName(branch string) string {
	return fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)
}

func mockSignature(time time.Time) *object.Signature {
	return &object.Signature{
		Name:  "Jane Doe",
		Email: "author@example.com",
		When:  time,
	}
}

func Clone(ctx context.Context, repoURL, branchName, remote string) (*extgogit.Repository, string, error) {
	dir, err := os.MkdirTemp("", "iac-clone-*")
	if err != nil {
		return nil, "", err
	}

	opts := &extgogit.CloneOptions{
		URL:           repoURL,
		RemoteName:    remote,
		ReferenceName: plumbing.NewBranchReferenceName(branchName),
	}

	wt := osfs.New(dir, osfs.WithBoundOS())
	dot := osfs.New(filepath.Join(dir, extgogit.GitDirName), osfs.WithBoundOS())
	storer := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	repo, err := extgogit.Clone(storer, wt, opts)
	if err != nil {
		return nil, "", err
	}

	w, err := repo.Worktree()
	if err != nil {
		return nil, "", err
	}

	err = w.Checkout(&extgogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
		Create: false,
	})
	if err != nil {
		return nil, "", err
	}

	return repo, dir, nil
}

func CommitIdFromBranch(repo *extgogit.Repository, branchName string) string {
	commitId := ""
	head, err := HeadFromBranch(repo, branchName)

	if err == nil {
		commitId = head.Hash.String()
	}
	return commitId
}

func GetRemoteHead(repo *extgogit.Repository, branchName, remote string) (plumbing.Hash, error) {
	rmt, err := repo.Remote(remote)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	err = rmt.Fetch(&extgogit.FetchOptions{
		RemoteName: remote,
		RefSpecs:   []config.RefSpec{config.RefSpec(BranchRefName(branchName))},
	})
	if err != nil && !errors.Is(err, extgogit.NoErrAlreadyUpToDate) {
		return plumbing.ZeroHash, err
	}

	remoteHeadRef, err := HeadFromBranch(repo, branchName)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	return remoteHeadRef.Hash, nil
}

// SetUpGitTestServer creates and returns a git test server. The caller must
// ensure it's stopped and cleaned up.
func SetUpGitTestServer(g *WithT) *gittestserver.GitServer {
	g.THelper()

	gitServer, err := gittestserver.NewTempGitServer()
	g.Expect(err).ToNot(HaveOccurred())

	username := rand.String(5)
	password := rand.String(5)

	gitServer.Auth(username, password)
	gitServer.AutoCreate()
	g.Expect(gitServer.StartHTTP()).ToNot(HaveOccurred())
	gitServer.KeyDir(filepath.Join(gitServer.Root(), "keys"))
	g.Expect(gitServer.ListenSSH()).ToNot(HaveOccurred())
	return gitServer
}

func GetSigningKeyPairSecret(g *WithT, name, namespace string) (*corev1.Secret, *openpgp.Entity) {
	g.THelper()

	passphrase := "abcde12345"
	pgpEntity, key := GetSigningKeyPair(g, passphrase)

	// Create the secret containing signing key.
	sec := &corev1.Secret{
		Data: map[string][]byte{
			signingSecretKey:     key,
			signingPassphraseKey: []byte(passphrase),
		},
	}
	sec.Name = name
	sec.Namespace = namespace
	return sec, pgpEntity
}

func GetSigningKeyPair(g *WithT, passphrase string) (*openpgp.Entity, []byte) {
	g.THelper()

	pgpEntity, err := openpgp.NewEntity("", "", "", nil)
	g.Expect(err).ToNot(HaveOccurred())

	// Configure OpenPGP armor encoder.
	b := bytes.NewBuffer(nil)
	w, err := armor.Encode(b, openpgp.PrivateKeyType, nil)
	g.Expect(err).ToNot(HaveOccurred())
	// Serialize private key.
	g.Expect(pgpEntity.SerializePrivate(w, nil)).To(Succeed())
	g.Expect(w.Close()).To(Succeed())

	if passphrase != "" {
		g.Expect(pgpEntity.PrivateKey.Encrypt([]byte(passphrase))).To(Succeed())
	}

	return pgpEntity, b.Bytes()
}
