/*
Copyright 2020 The Flux authors

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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/otiai10/copy"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1alpha1"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/gittestserver"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1alpha1"
	"github.com/fluxcd/image-automation-controller/pkg/test"
	"github.com/fluxcd/image-automation-controller/pkg/update"
)

const timeout = 10 * time.Second

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
		branch         string
		repositoryPath string
		repoURL        string
		namespace      *corev1.Namespace
		gitServer      *gittestserver.GitServer
		gitRepoKey     types.NamespacedName
		commitMessage  string
	)

	// Start the git server
	BeforeEach(func() {
		branch = randStringRunes(8)
		repositoryPath = "/config-" + randStringRunes(5) + ".git"

		namespace = &corev1.Namespace{}
		namespace.Name = "image-auto-test-" + randStringRunes(5)
		Expect(k8sClient.Create(context.Background(), namespace)).To(Succeed())

		var err error
		gitServer, err = gittestserver.NewTempGitServer()
		Expect(err).NotTo(HaveOccurred())
		gitServer.AutoCreate()
		Expect(gitServer.StartHTTP()).To(Succeed())

		repoURL = gitServer.HTTPAddress() + repositoryPath

		gitRepoKey = types.NamespacedName{
			Name:      "image-auto-" + randStringRunes(5),
			Namespace: namespace.Name,
		}

		gitRepo := &sourcev1.GitRepository{
			ObjectMeta: metav1.ObjectMeta{
				Name:      gitRepoKey.Name,
				Namespace: namespace.Name,
			},
			Spec: sourcev1.GitRepositorySpec{
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
		Expect(initGitRepo(gitServer, "testdata/appconfig", branch, repositoryPath)).To(Succeed())
	})

	Context("with ImagePolicy", func() {
		var (
			localRepo *git.Repository
			policy    *imagev1_reflect.ImagePolicy
			policyKey types.NamespacedName
		)

		const latestImage = "helloworld:1.0.1"
		const evenLatestImage = "helloworld:1.2.0"

		BeforeEach(func() {
			commitMessage = "Commit a difference " + randStringRunes(5)

			Expect(initGitRepo(gitServer, "testdata/appconfig", branch, repositoryPath)).To(Succeed())

			var err error
			localRepo, err = git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
				URL:           repoURL,
				RemoteName:    "origin",
				ReferenceName: plumbing.NewBranchReferenceName(branch),
			})
			Expect(err).ToNot(HaveOccurred())

			policyKey = types.NamespacedName{
				Name:      "policy-" + randStringRunes(5),
				Namespace: namespace.Name,
			}
			// NB not testing the image reflector controller; this
			// will make a "fully formed" ImagePolicy object.
			policy = &imagev1_reflect.ImagePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policyKey.Name,
					Namespace: policyKey.Namespace,
				},
				Spec: imagev1_reflect.ImagePolicySpec{
					ImageRepositoryRef: corev1.LocalObjectReference{
						Name: "not-expected-to-exist",
					},
					Policy: imagev1_reflect.ImagePolicyChoice{
						SemVer: &imagev1_reflect.SemVerPolicy{
							Range: "1.x",
						},
					},
				},
				Status: imagev1_reflect.ImagePolicyStatus{
					LatestImage: latestImage,
				},
			}
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			Expect(k8sClient.Status().Update(context.Background(), policy)).To(Succeed())

		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), policy)).To(Succeed())
		})

		Context("with Setters", func() {

			var (
				updateKey       types.NamespacedName
				updateBySetters *imagev1.ImageUpdateAutomation
			)

			BeforeEach(func() {
				// Insert a setter reference into the deployment file,
				// before creating the automation object itself.
				commitInRepo(repoURL, branch, "Install setter marker", func(tmp string) {
					replaceMarker(tmp, policyKey)
				})

				// pull the head commit we just pushed, so it's not
				// considered a new commit when checking for a commit
				// made by automation.
				waitForNewHead(localRepo, branch)

				// now create the automation object, and let it (one
				// hopes!) make a commit itself.
				updateKey = types.NamespacedName{
					Namespace: gitRepoKey.Namespace,
					Name:      "update-" + randStringRunes(5),
				}
				updateBySetters = &imagev1.ImageUpdateAutomation{
					ObjectMeta: metav1.ObjectMeta{
						Name:      updateKey.Name,
						Namespace: updateKey.Namespace,
					},
					Spec: imagev1.ImageUpdateAutomationSpec{
						Interval: metav1.Duration{Duration: 2 * time.Hour}, // this is to ensure any subsequent run should be outside the scope of the testing
						Checkout: imagev1.GitCheckoutSpec{
							GitRepositoryRef: corev1.LocalObjectReference{
								Name: gitRepoKey.Name,
							},
							Branch: branch,
						},
						Update: imagev1.UpdateStrategy{
							Setters: &imagev1.SettersStrategy{},
						},
						Commit: imagev1.CommitSpec{
							MessageTemplate: commitMessage,
						},
					},
				}
				Expect(k8sClient.Create(context.Background(), updateBySetters)).To(Succeed())
				// wait for a new commit to be made by the controller
				waitForNewHead(localRepo, branch)
			})

			AfterEach(func() {
				Expect(k8sClient.Delete(context.Background(), updateBySetters)).To(Succeed())
			})

			It("updates to the most recent image", func() {
				// having passed the BeforeEach, we should see a commit
				head, _ := localRepo.Head()
				commit, err := localRepo.CommitObject(head.Hash())
				Expect(err).ToNot(HaveOccurred())
				Expect(commit.Message).To(Equal(commitMessage))

				var newObj imagev1.ImageUpdateAutomation
				Expect(k8sClient.Get(context.Background(), updateKey, &newObj)).To(Succeed())
				Expect(newObj.Status.LastPushCommit).To(Equal(head.Hash().String()))
				Expect(newObj.Status.LastPushTime).ToNot(BeNil())

				compareRepoWithExpected(repoURL, branch, "testdata/appconfig-setters-expected", func(tmp string) {
					replaceMarker(tmp, policyKey)
				})
			})

			It("stops updating when suspended", func() {
				// suspend it, and check that reconciliation does not run
				var updatePatch imagev1.ImageUpdateAutomation
				updatePatch.Name = updateKey.Name
				updatePatch.Namespace = updateKey.Namespace
				updatePatch.Spec.Suspend = true
				Expect(k8sClient.Patch(context.Background(), &updatePatch, client.Merge)).To(Succeed())
				// wait for the suspension to reach the cache
				var newUpdate imagev1.ImageUpdateAutomation
				Eventually(func() bool {
					if err := imageAutoReconciler.Get(context.Background(), updateKey, &newUpdate); err != nil {
						return false
					}
					return newUpdate.Spec.Suspend
				}, timeout, time.Second).Should(BeTrue())
				// run the reconciliation explicitly, and make sure it
				// doesn't do anything
				result, err := imageAutoReconciler.Reconcile(logr.NewContext(context.TODO(), ctrl.Log), ctrl.Request{
					NamespacedName: updateKey,
				})
				Expect(err).To(BeNil())
				// this ought to fail if suspend is not working, since the item would be requeued;
				// but if not, additional checks lie below.
				Expect(result).To(Equal(ctrl.Result{}))

				var checkUpdate imagev1.ImageUpdateAutomation
				Expect(k8sClient.Get(context.Background(), updateKey, &checkUpdate)).To(Succeed())
				Expect(checkUpdate.Status.ObservedGeneration).NotTo(Equal(checkUpdate.ObjectMeta.Generation))
			})

			It("runs when the reconcile request annotation is added", func() {
				// the automation has run, and is not expected to run
				// again for 2 hours. Make a commit to the git repo
				// which needs to be undone by automation, then add
				// the annotation and make sure it runs again.
				Expect(k8sClient.Get(context.Background(), updateKey, updateBySetters)).To(Succeed())
				Expect(updateBySetters.Status.LastAutomationRunTime).ToNot(BeNil())
				lastRunTime := updateBySetters.Status.LastAutomationRunTime.Time

				commitInRepo(repoURL, branch, "Revert image update", func(tmp string) {
					// revert the change made by copying the old version
					// of the file back over then restoring the setter
					// marker
					copy.Copy("testdata/appconfig/deploy.yaml", filepath.Join(tmp, "deploy.yaml"))
					replaceMarker(tmp, policyKey)
				})
				// check that it was reverted correctly
				compareRepoWithExpected(repoURL, branch, "testdata/appconfig", func(tmp string) {
					replaceMarker(tmp, policyKey)
				})

				ts := time.Now().String()
				var updatePatch imagev1.ImageUpdateAutomation
				updatePatch.Spec = updateBySetters.Spec // otherwise the patch will blank some fields
				updatePatch.Name = updateKey.Name
				updatePatch.Namespace = updateKey.Namespace
				updatePatch.ObjectMeta.Annotations = map[string]string{
					meta.ReconcileRequestAnnotation: ts,
				}
				Expect(k8sClient.Patch(context.Background(), &updatePatch, client.Merge)).To(Succeed())

				// ... this is where the reconciler is supposed to do its work ...

				var newUpdate imagev1.ImageUpdateAutomation
				Eventually(func() bool {
					if err := k8sClient.Get(context.Background(), updateKey, &newUpdate); err != nil {
						return false
					}
					newLastRun := newUpdate.Status.LastAutomationRunTime
					return newLastRun != nil && newLastRun.Time.After(lastRunTime)
				}, timeout, time.Second).Should(BeTrue())
				// check that the annotation was recorded as seen
				Expect(newUpdate.Status.LastHandledReconcileAt).To(Equal(ts))
				expectCommittedAndPushed(newUpdate.Status.Conditions)

				// check that a new commit was made
				compareRepoWithExpected(repoURL, branch, "testdata/appconfig-setters-expected", func(tmp string) {
					replaceMarker(tmp, policyKey)
				})
			})
		})
	})
})

func expectCommittedAndPushed(conditions []metav1.Condition) {
	rc := apimeta.FindStatusCondition(conditions, meta.ReadyCondition)
	Expect(rc).ToNot(BeNil())
	Expect(rc.Message).To(ContainSubstring("committed and pushed"))
}

func replaceMarker(path string, policyKey types.NamespacedName) {
	// NB this requires knowledge of what's in the git
	// repo, so a little brittle
	deployment := filepath.Join(path, "deploy.yaml")
	filebytes, err := ioutil.ReadFile(deployment)
	Expect(err).NotTo(HaveOccurred())
	newfilebytes := bytes.ReplaceAll(filebytes, []byte("SETTER_SITE"), []byte(setterRef(policyKey)))
	Expect(ioutil.WriteFile(deployment, newfilebytes, os.FileMode(0666))).To(Succeed())
}

func setterRef(name types.NamespacedName) string {
	return fmt.Sprintf(`{"%s": "%s:%s"}`, update.SetterShortHand, name.Namespace, name.Name)
}

func waitForNewHead(repo *git.Repository, branch string) {
	head, _ := repo.Head()
	headHash := head.Hash().String()
	working, err := repo.Worktree()
	Expect(err).ToNot(HaveOccurred())
	Eventually(func() bool {
		if working.Pull(&git.PullOptions{
			ReferenceName: plumbing.NewBranchReferenceName(branch),
		}); err != nil {
			return false
		}
		h, _ := repo.Head()
		return headHash != h.Hash().String()
	}, timeout, time.Second).Should(BeTrue())
}

func compareRepoWithExpected(repoURL, branch, fixture string, changeFixture func(tmp string)) {
	expected, err := ioutil.TempDir("", "gotest-imageauto-expected")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(expected)
	copy.Copy(fixture, expected)
	changeFixture(expected)

	tmp, err := ioutil.TempDir("", "gotest-imageauto")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tmp)
	_, err = git.PlainClone(tmp, false, &git.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
	})
	Expect(err).ToNot(HaveOccurred())
	test.ExpectMatchingDirectories(tmp, expected)
}

func commitInRepo(repoURL, branch, msg string, changeFiles func(path string)) {
	tmp, err := ioutil.TempDir("", "gotest-imageauto")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tmp)
	repo, err := git.PlainClone(tmp, false, &git.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
	})
	Expect(err).ToNot(HaveOccurred())

	changeFiles(tmp)

	worktree, err := repo.Worktree()
	Expect(err).ToNot(HaveOccurred())
	_, err = worktree.Add(".")
	Expect(err).ToNot(HaveOccurred())
	_, err = worktree.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Testbot",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(repo.Push(&git.PushOptions{RemoteName: "origin"})).To(Succeed())
}

// Initialise a git server with a repo including the files in dir.
func initGitRepo(gitServer *gittestserver.GitServer, fixture, branch, repositoryPath string) error {
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
		Branch: plumbing.NewBranchReferenceName(branch),
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
