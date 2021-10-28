package controllers

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/go-logr/logr"

	"github.com/fluxcd/pkg/gittestserver"
)

func populateRepoFromFixture(repo *gogit.Repository, fixture string) error {
	working, err := repo.Worktree()
	if err != nil {
		return err
	}
	fs := working.Filesystem

	if err = filepath.Walk(fixture, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fs.MkdirAll(fs.Join(path[len(fixture):]), info.Mode())
		}
		// copy symlinks as-is, so I can test what happens with broken symlinks
		if info.Mode()&os.ModeSymlink > 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return fs.Symlink(target, path[len(fixture):])
		}

		fileBytes, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}

		ff, err := fs.Create(path[len(fixture):])
		if err != nil {
			return err
		}
		defer ff.Close()

		_, err = ff.Write(fileBytes)
		return err
	}); err != nil {
		return err
	}

	_, err = working.Add(".")
	if err != nil {
		return err
	}

	if _, err = working.Commit("Initial revision from "+fixture, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Testbot",
			Email: "test@example.com",
			When:  time.Now(),
		},
	}); err != nil {
		return err
	}

	return nil
}

func TestRepoForFixture(t *testing.T) {
	repo, err := gogit.Init(memory.NewStorage(), memfs.New())
	if err != nil {
		t.Fatal(err)
	}

	err = populateRepoFromFixture(repo, "testdata/pathconfig")
	if err != nil {
		t.Error(err)
	}
}

func TestIgnoreBrokenSymlink(t *testing.T) {
	// init a git repo in the filesystem so we can operate on files there
	tmp, err := ioutil.TempDir("", "flux-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	repo, err := gogit.PlainInit(tmp, false)
	if err != nil {
		t.Fatal(err)
	}
	err = populateRepoFromFixture(repo, "testdata/brokenlink")
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

	// this is currently defined in update_test.go, but handy right here ..
	if err = initGitRepo(gitServer, "testdata/appconfig", "main", "/appconfig.git"); err != nil {
		t.Fatal(err)
	}

	tmp, err := ioutil.TempDir("", "gotest-imageauto-git")
	if err != nil {
		t.Fatal(err)
	}
	repoURL := gitServer.HTTPAddress() + "/appconfig.git"
	repo, err := gogit.PlainClone(tmp, false, &gogit.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.NewBranchReferenceName("main"),
	})

	// This is here to guard against push in general being broken
	err = push(context.TODO(), tmp, "main", repoAccess{
		url:  repoURL,
		auth: nil,
	})
	if err != nil {
		t.Fatal(err)
	}

	// This is not under test, but needed for the next bit
	if err = switchBranch(repo, branch); err != nil {
		t.Fatal(err)
	}

	// This is supposed to fail, because the hook rejects the branch
	// pushed to.
	err = push(context.TODO(), tmp, branch, repoAccess{
		url:  repoURL,
		auth: nil,
	})
	if err == nil {
		t.Error("push to a forbidden branch is expected to fail, but succeeded")
	}
}
