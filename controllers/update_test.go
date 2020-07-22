/*
Copyright 2020 Michael Bridgen <mikeb@squaremobius.net>

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

package controllers

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	//	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//	"k8s.io/apimachinery/pkg/types"
	"github.com/fluxcd/source-controller/pkg/testserver"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	//"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	//imagev1alpha1 "github.com/squaremo/image-automation-controller/api/v1alpha1"
)

const repositoryPath = "/config.git"

var _ = Describe("ImageUpdateAutomation", func() {
	var (
		namespace *corev1.Namespace
		gitServer *testserver.GitServer
	)

	const namespaceName = "image-update-test"

	BeforeEach(func() {
		namespace = &corev1.Namespace{}
		namespace.Name = namespaceName
		Expect(k8sClient.Create(context.Background(), namespace)).To(Succeed())

		var err error
		gitServer, err = testserver.NewTempGitServer()
		Expect(err).NotTo(HaveOccurred())
		gitServer.AutoCreate()
	})

	AfterEach(func() {
		os.RemoveAll(gitServer.Root())
		Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())
	})

	It("Initialises git OK", func() {
		Expect(gitServer.StartHTTP()).To(Succeed())
		defer gitServer.StopHTTP()
		Expect(initGitRepo(gitServer, "testdata/appconfig")).To(Succeed())
	})
})

// Initialise a git server with a repo including the files in dir.
func initGitRepo(gitServer *testserver.GitServer, fixture string) error {
	fs := memfs.New()
	repo, err := git.Init(memory.NewStorage(), fs)
	if err != nil {
		return err
	}

	if err = filepath.Walk(fixture, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fs.MkdirAll(fs.Join(path[len(fixture):]), info.Mode())
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

	working, err := repo.Worktree()
	if err != nil {
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

	remote, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{gitServer.HTTPAddress() + repositoryPath},
	})
	if err != nil {
		return err
	}

	return remote.Push(&git.PushOptions{
		RefSpecs: []config.RefSpec{"refs/heads/*:refs/heads/*"},
	})
}
