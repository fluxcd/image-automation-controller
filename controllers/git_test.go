package controllers

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

func populateRepoFromFixture(repo *git.Repository, fixture string) error {
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

	if _, err = working.Commit("Initial revision from "+fixture, &git.CommitOptions{
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
	repo, err := git.Init(memory.NewStorage(), memfs.New())
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

	repo, err := git.PlainInit(tmp, false)
	if err != nil {
		t.Fatal(err)
	}
	err = populateRepoFromFixture(repo, "testdata/brokenlink")
	if err != nil {
		t.Fatal(err)
	}

	_, err = commitChangedManifests(repo, tmp, nil, nil, "unused")
	if err != errNoChanges {
		t.Fatalf("expected no changes but got: %v", err)
	}
}
