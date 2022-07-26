package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	libgit2 "github.com/libgit2/git2go/v33"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	"github.com/fluxcd/pkg/gittestserver"
	"github.com/fluxcd/source-controller/pkg/git"
)

func populateRepoFromFixture(repo *libgit2.Repository, fixture string) error {
	absFixture, err := filepath.Abs(fixture)
	if err != nil {
		return err
	}
	if err := filepath.Walk(absFixture, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(path[len(fixture):]), info.Mode())
		}
		// copy symlinks as-is, so I can test what happens with broken symlinks
		if info.Mode()&os.ModeSymlink > 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, path[len(fixture):])
		}

		fileBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		ff, err := os.Create(path[len(fixture):])
		if err != nil {
			return err
		}
		defer ff.Close()

		_, err = ff.Write(fileBytes)
		return err
	}); err != nil {
		return err
	}

	sig := &libgit2.Signature{
		Name:  "Testbot",
		Email: "test@example.com",
		When:  time.Now(),
	}

	if _, err := commitWorkDir(repo, "main", "Initial revision from "+fixture, sig); err != nil {
		return err
	}

	return nil
}

func TestRepoForFixture(t *testing.T) {
	tmp, err := os.MkdirTemp("", "flux-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	repo, err := initGitRepoPlain("testdata/pathconfig", tmp)
	if err != nil {
		t.Error(err)
	}
	repo.Free()
}

func TestIgnoreBrokenSymlink(t *testing.T) {
	// init a git repo in the filesystem so we can operate on files there
	tmp, err := os.MkdirTemp("", "flux-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	repo, err := initGitRepoPlain("testdata/brokenlink", tmp)
	if err != nil {
		t.Fatal(err)
	}

	_, err = commitChangedManifests(logr.Discard(), repo, tmp, nil, nil, "unused")
	if err != errNoChanges {
		t.Fatalf("expected no changes but got: %v", err)
	}
}

// this is a hook script that will reject a ref update for a branch
// that's not `main`
const rejectBranch = `
if [ "$1" != "refs/heads/main" ]; then
  echo "*** Rejecting push to non-main branch $1" >&2
  exit 1
fi
`

func TestPushRejected(t *testing.T) {
	// Check that pushing to a repository which rejects a ref update
	// results in an error. Why would a repo reject an update? If yu
	// use e.g., branch protection in GitHub, this is what happens --
	// see
	// https://github.com/fluxcd/image-automation-controller/issues/194.

	branch := "push-branch"

	gitServer, err := gittestserver.NewTempGitServer()
	if err != nil {
		t.Fatal(err)
	}
	gitServer.AutoCreate()
	gitServer.InstallUpdateHook(rejectBranch)

	if err = gitServer.StartHTTP(); err != nil {
		t.Fatal(err)
	}

	// We use "test" as the branch to init repo, to avoid potential conflicts
	// with the default branch(main/master) of the system this test is running
	// on. If, for e.g., we used main as the branch and the default branch is
	// supposed to be main, this will fail as this would try to create a branch
	// named main explicitly.
	if err = initGitRepo(gitServer, "testdata/appconfig", "test", "/appconfig.git"); err != nil {
		t.Fatal(err)
	}

	repoURL := gitServer.HTTPAddressWithCredentials() + "/appconfig.git"
	cloneCtx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	repo, err := clone(cloneCtx, repoURL, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Free()

	cleanup, err := configureTransportOptsForRepo(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// This is here to guard against push in general being broken
	err = push(context.TODO(), repo.Workdir(), "test", repoAccess{})
	if err != nil {
		t.Fatal(err)
	}

	// This is not under test, but needed for the next bit
	if err = repo.SetHead(fmt.Sprintf("refs/heads/%s", branch)); err != nil {
		t.Fatal(err)
	}

	// This is supposed to fail, because the hook rejects the branch
	// pushed to.
	err = push(context.TODO(), repo.Workdir(), branch, repoAccess{})
	if err == nil {
		t.Error("push to a forbidden branch is expected to fail, but succeeded")
	}
}

func TestEarlyEOF(t *testing.T) {
	g := NewWithT(t)

	gitServer, err := gittestserver.NewTempGitServer()
	g.Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(gitServer.Root())

	username := "norris"
	password := "chuck"

	gitServer.
		AutoCreate().
		KeyDir(filepath.Join(t.TempDir(), "keys")).
		Auth(username, password).
		ReadOnly(true)

	err = gitServer.StartHTTP()
	g.Expect(err).ToNot(HaveOccurred())

	err = initGitRepo(gitServer, "testdata/appconfig", "test", "/appconfig.git")
	g.Expect(err).ToNot(HaveOccurred())

	repoURL := gitServer.HTTPAddressWithCredentials() + "/appconfig.git"
	cloneCtx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	access := repoAccess{
		auth: &git.AuthOptions{
			Username: username,
			Password: password,
		},
	}

	repo, err := clone(cloneCtx, repoURL, "test", access.auth)
	g.Expect(err).ToNot(HaveOccurred())

	defer repo.Free()

	cleanup, err := configureTransportOptsForRepo(repo, access.auth)
	g.Expect(err).ToNot(HaveOccurred())

	defer cleanup()

	err = push(context.TODO(), repo.Workdir(), "test", access)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("early EOF (the SSH key may not have write access to the repository)"))
}

func Test_switchToBranch(t *testing.T) {
	g := NewWithT(t)
	gitServer, err := gittestserver.NewTempGitServer()
	g.Expect(err).ToNot(HaveOccurred())
	gitServer.AutoCreate()
	g.Expect(gitServer.StartHTTP()).To(Succeed())

	// We use "test" as the branch to init repo, to avoid potential conflicts
	// with the default branch(main/master) of the system this test is running
	// on. If, for e.g., we used main as the branch and the default branch is
	// supposed to be main, this will fail as this would try to create a branch
	// named main explicitly.
	branch := "test"
	g.Expect(initGitRepo(gitServer, "testdata/appconfig", branch, "/appconfig.git")).To(Succeed())

	repoURL := gitServer.HTTPAddressWithCredentials() + "/appconfig.git"
	cloneCtx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()
	repo, err := clone(cloneCtx, repoURL, branch, nil)
	g.Expect(err).ToNot(HaveOccurred())
	defer repo.Free()

	head, err := repo.Head()
	g.Expect(err).ToNot(HaveOccurred())
	defer head.Free()
	target := head.Target()

	// register transport options and update remote to transport url
	cleanup, err := configureTransportOptsForRepo(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// calling switchToBranch with a branch that doesn't exist on origin
	// should result in the branch being created and switched to.
	branch = "not-on-origin"
	switchToBranch(repo, context.TODO(), branch, repoAccess{})

	head, err = repo.Head()
	g.Expect(err).ToNot(HaveOccurred())
	name, err := head.Branch().Name()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(name).To(Equal(branch))

	cc, err := repo.LookupCommit(head.Target())
	g.Expect(err).ToNot(HaveOccurred())
	defer cc.Free()
	g.Expect(cc.Id().String()).To(Equal(target.String()))

	// create a branch with the HEAD commit and push it to origin
	branch = "exists-on-origin"
	_, err = repo.CreateBranch(branch, cc, false)
	g.Expect(err).ToNot(HaveOccurred())
	origin, err := repo.Remotes.Lookup(originRemote)
	g.Expect(err).ToNot(HaveOccurred())
	defer origin.Free()

	g.Expect(origin.Push(
		[]string{fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)}, &libgit2.PushOptions{},
	)).To(Succeed())

	// push a new commit to the branch. this is done to test whether we properly
	// sync our local branch with the remote branch, before switching.
	policyKey := types.NamespacedName{
		Name:      "policy",
		Namespace: "ns",
	}
	commitID := commitInRepo(g, repoURL, branch, "Install setter marker", func(tmp string) {
		g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
	})

	// calling switchToBranch with a branch that exists should make sure to fetch latest
	// for that branch from origin, and then switch to it.
	switchToBranch(repo, context.TODO(), branch, repoAccess{})
	head, err = repo.Head()
	g.Expect(err).ToNot(HaveOccurred())
	name, err = head.Branch().Name()

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(name).To(Equal(branch))
	g.Expect(head.Target().String()).To(Equal(commitID.String()))

	// push a commit after switching to the branch, to check if the local
	// branch is synced with origin.
	replaceMarker(repo.Workdir(), policyKey)
	sig := &libgit2.Signature{
		Name:  "Testbot",
		Email: "test@example.com",
		When:  time.Now(),
	}
	_, err = commitWorkDir(repo, branch, "update policy", sig)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(push(context.TODO(), repo.Workdir(), branch, repoAccess{})).To(Succeed())
}
