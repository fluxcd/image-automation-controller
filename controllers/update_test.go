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
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/fluxcd/source-controller/pkg/testserver"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	sourcev1alpha1 "github.com/fluxcd/source-controller/api/v1alpha1"
	imagev1alpha1 "github.com/squaremo/image-automation-controller/api/v1alpha1"
	"github.com/squaremo/image-automation-controller/pkg/test"
	imagev1alpha1_reflect "github.com/squaremo/image-reflector-controller/api/v1alpha1"
)

const timeout = 10 * time.Second

const defaultBranch = "test-main"

// Copied from
// https://github.com/fluxcd/source-controller/blob/master/controllers/suite_test.go
var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz1234567890")

func randStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

var _ = Describe("ImageUpdateAutomation", func() {
	var (
		repositoryPath string
		repoURL        string
		namespace      *corev1.Namespace
		gitServer      *testserver.GitServer
		gitRepoKey     types.NamespacedName
	)

	// Start the git server
	BeforeEach(func() {
		repositoryPath = "/config-" + randStringRunes(5) + ".git"

		namespace = &corev1.Namespace{}
		namespace.Name = "image-auto-test-" + randStringRunes(5)
		Expect(k8sClient.Create(context.Background(), namespace)).To(Succeed())

		var err error
		gitServer, err = testserver.NewTempGitServer()
		Expect(err).NotTo(HaveOccurred())
		gitServer.AutoCreate()
		Expect(gitServer.StartHTTP()).To(Succeed())

		repoURL = gitServer.HTTPAddress() + repositoryPath

		gitRepoKey = types.NamespacedName{
			Name:      "image-auto-" + randStringRunes(5),
			Namespace: namespace.Name,
		}

		gitRepo := &sourcev1alpha1.GitRepository{
			ObjectMeta: metav1.ObjectMeta{
				Name:      gitRepoKey.Name,
				Namespace: namespace.Name,
			},
			Spec: sourcev1alpha1.GitRepositorySpec{
				URL:      repoURL,
				Interval: metav1.Duration{Duration: time.Minute},
			},
		}
		Expect(k8sClient.Create(context.Background(), gitRepo)).To(Succeed())
	})

	AfterEach(func() {
		gitServer.StopHTTP()
		os.RemoveAll(gitServer.Root())
		Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())
	})

	It("Initialises git OK", func() {
		Expect(initGitRepo(gitServer, "testdata/appconfig", repositoryPath)).To(Succeed())
	})

	Context("with ImagePolicy", func() {
		var (
			localRepo           *git.Repository
			updateKey           types.NamespacedName
			policy              *imagev1alpha1_reflect.ImagePolicy
			updateByImagePolicy *imagev1alpha1.ImageUpdateAutomation
			commitMessage       string
		)

		const latestImage = "helloworld:1.0.1"
		const evenLatestImage = "helloworld:1.2.0"

		BeforeEach(func() {
			Expect(initGitRepo(gitServer, "testdata/appconfig", repositoryPath)).To(Succeed())

			var err error
			localRepo, err = git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
				URL:           repoURL,
				RemoteName:    "origin",
				ReferenceName: plumbing.NewBranchReferenceName(defaultBranch),
			})
			Expect(err).ToNot(HaveOccurred())

			policyKey := types.NamespacedName{
				Name:      "policy-" + randStringRunes(5),
				Namespace: namespace.Name,
			}
			// NB not testing the image reflector controller; this
			// will make a "fully formed" ImagePolicy object.
			policy = &imagev1alpha1_reflect.ImagePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policyKey.Name,
					Namespace: policyKey.Namespace,
				},
				Spec: imagev1alpha1_reflect.ImagePolicySpec{
					ImageRepositoryRef: corev1.LocalObjectReference{
						Name: "not-expected-to-exist",
					},
					Policy: imagev1alpha1_reflect.ImagePolicyChoice{
						SemVer: &imagev1alpha1_reflect.SemVerPolicy{
							Range: "1.x",
						},
					},
				},
				Status: imagev1alpha1_reflect.ImagePolicyStatus{
					LatestImage: latestImage,
				},
			}
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Expect(k8sClient.Status().Update(context.Background(), policy)).To(Succeed())

			commitMessage = "Commit a difference " + randStringRunes(5)
			updateKey = types.NamespacedName{
				Namespace: gitRepoKey.Namespace,
				Name:      "update-" + randStringRunes(5),
			}
			updateByImagePolicy = &imagev1alpha1.ImageUpdateAutomation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      updateKey.Name,
					Namespace: updateKey.Namespace,
				},
				Spec: imagev1alpha1.ImageUpdateAutomationSpec{
					Checkout: imagev1alpha1.GitCheckoutSpec{
						GitRepositoryRef: corev1.LocalObjectReference{
							Name: gitRepoKey.Name,
						},
						Branch: defaultBranch,
					},
					Update: imagev1alpha1.UpdateStrategy{
						ImagePolicyRef: &corev1.LocalObjectReference{
							Name: policyKey.Name,
						},
					},
					Commit: imagev1alpha1.CommitSpec{
						MessageTemplate: commitMessage,
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), updateByImagePolicy)).To(Succeed())
			head, _ := localRepo.Head()
			headHash := head.Hash().String()
			working, err := localRepo.Worktree()
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool {
				if working.Pull(&git.PullOptions{
					ReferenceName: plumbing.NewBranchReferenceName(defaultBranch),
				}); err != nil {
					return false
				}
				h, _ := localRepo.Head()
				return headHash != h.Hash().String()
			}, timeout, time.Second).Should(BeTrue())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), updateByImagePolicy)).To(Succeed())
			Expect(k8sClient.Delete(context.Background(), policy)).To(Succeed())
		})

		It("updates to the most recent image", func() {
			// having passed the BeforeEach, we should see a commit
			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			Expect(err).ToNot(HaveOccurred())
			Expect(commit.Message).To(Equal(commitMessage))

			tmp, err := ioutil.TempDir("", "gotest-imageauto")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmp)

			_, err = git.PlainClone(tmp, false, &git.CloneOptions{
				URL:           repoURL,
				ReferenceName: plumbing.NewBranchReferenceName(defaultBranch),
			})
			Expect(err).ToNot(HaveOccurred())
			test.ExpectMatchingDirectories(tmp, "testdata/appconfig-expected")
		})

		It("makes a commit when the policy changes", func() {
			// make sure the first commit happened
			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			Expect(err).ToNot(HaveOccurred())
			Expect(commit.Message).To(Equal(commitMessage))

			headHash := head.Hash().String()

			// change the status and
			// make sure there's a commit for that.
			policy.Status.LatestImage = evenLatestImage
			Expect(k8sClient.Status().Update(context.Background(), policy)).To(Succeed())

			working, err := localRepo.Worktree()
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool {
				if working.Pull(&git.PullOptions{
					ReferenceName: plumbing.NewBranchReferenceName(defaultBranch),
				}); err != nil {
					return false
				}
				h, _ := localRepo.Head()
				return headHash != h.Hash().String()
			}, timeout, time.Second).Should(BeTrue())

			tmp, err := ioutil.TempDir("", "gotest-imageauto")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmp)

			_, err = git.PlainClone(tmp, false, &git.CloneOptions{
				URL:           repoURL,
				ReferenceName: plumbing.NewBranchReferenceName(defaultBranch),
			})
			Expect(err).ToNot(HaveOccurred())
			test.ExpectMatchingDirectories(tmp, "testdata/appconfig-expected2")
		})
	})
})

// Initialise a git server with a repo including the files in dir.
func initGitRepo(gitServer *testserver.GitServer, fixture, repositoryPath string) error {
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

	if err = working.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(defaultBranch),
		Create: true,
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
