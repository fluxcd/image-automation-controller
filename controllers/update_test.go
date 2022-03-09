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
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	securejoin "github.com/cyphar/filepath-securejoin"
	git2go "github.com/libgit2/git2go/v33"
	libgit2 "github.com/libgit2/git2go/v33"
	"github.com/otiai10/copy"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta1"
	"github.com/fluxcd/pkg/apis/acl"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/gittestserver"
	"github.com/fluxcd/pkg/ssh"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta1"
	"github.com/fluxcd/image-automation-controller/pkg/test"
	"github.com/fluxcd/image-automation-controller/pkg/update"
)

const timeout = 10 * time.Second

var (
	// Copied from
	// https://github.com/fluxcd/source-controller/blob/master/controllers/suite_test.go
	letterRunes = []rune("abcdefghijklmnopqrstuvwxyz1234567890")

	gitServer *gittestserver.GitServer

	repositoryPath string
)

func randStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

var _ = Describe("ImageUpdateAutomation", func() {
	var (
		branch             string
		namespace          *corev1.Namespace
		username, password string
		authorName         = "Flux B Ot"
		authorEmail        = "fluxbot@example.com"
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
		username = randStringRunes(5)
		password = randStringRunes(5)
		// using authentication makes using the server more fiddly in
		// general, but is required for testing SSH.
		gitServer.Auth(username, password)
		gitServer.AutoCreate()
		Expect(gitServer.StartHTTP()).To(Succeed())
		gitServer.KeyDir(filepath.Join(gitServer.Root(), "keys"))
		Expect(gitServer.ListenSSH()).To(Succeed())
	})

	AfterEach(func() {
		gitServer.StopHTTP()
		os.RemoveAll(gitServer.Root())
	})

	It("Initialises git OK", func() {
		Expect(initGitRepo(gitServer, "testdata/appconfig", branch, repositoryPath)).To(Succeed())
	})

	Context("commit spec", func() {

		var (
			localRepo     *git2go.Repository
			commitMessage string
		)

		const (
			commitTemplate = `Commit summary

Automation: {{ .AutomationObject }}

Files:
{{ range $filename, $_ := .Updated.Files -}}
- {{ $filename }}
{{ end -}}

Objects:
{{ range $resource, $_ := .Updated.Objects -}}
{{ if eq $resource.Kind "Deployment" -}}
- {{ $resource.Kind | lower }} {{ $resource.Name | lower }}
{{ else -}}
- {{ $resource.Kind }} {{ $resource.Name }}
{{ end -}}
{{ end -}}

Images:
{{ range .Updated.Images -}}
- {{.}} ({{.Policy.Name}})
{{ end -}}
`
			commitMessageFmt = `Commit summary

Automation: %s/update-test

Files:
- deploy.yaml
Objects:
- deployment test
Images:
- helloworld:v1.0.0 (%s)
`
		)

		BeforeEach(func() {
			Expect(initGitRepo(gitServer, "testdata/appconfig", branch, repositoryPath)).To(Succeed())
			repoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
			var err error
			localRepo, err = clone(repoURL, "origin", branch)
			Expect(err).ToNot(HaveOccurred())

			gitRepoKey := types.NamespacedName{
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
			policyKey := types.NamespacedName{
				Name:      "policy-" + randStringRunes(5),
				Namespace: namespace.Name,
			}
			// NB not testing the image reflector controller; this
			// will make a "fully formed" ImagePolicy object.
			policy := &imagev1_reflect.ImagePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policyKey.Name,
					Namespace: policyKey.Namespace,
				},
				Spec: imagev1_reflect.ImagePolicySpec{
					ImageRepositoryRef: meta.NamespacedObjectReference{
						Name: "not-expected-to-exist",
					},
					Policy: imagev1_reflect.ImagePolicyChoice{
						SemVer: &imagev1_reflect.SemVerPolicy{
							Range: "1.x",
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			policy.Status.LatestImage = "helloworld:v1.0.0"
			Expect(k8sClient.Status().Update(context.Background(), policy)).To(Succeed())

			// Format the expected message given the generated values
			commitMessage = fmt.Sprintf(commitMessageFmt, namespace.Name, policyKey.Name)

			// Insert a setter reference into the deployment file,
			// before creating the automation object itself.
			commitInRepo(repoURL, branch, "Install setter marker", func(tmp string) {
				Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId := commitIdFromBranch(localRepo, branch)

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			waitForNewHead(localRepo, branch, preChangeCommitId)

			// now create the automation object, and let it (one
			// hopes!) make a commit itself.
			updateKey := types.NamespacedName{
				Namespace: namespace.Name,
				Name:      "update-test",
			}
			updateBySetters := &imagev1.ImageUpdateAutomation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      updateKey.Name,
					Namespace: updateKey.Namespace,
				},
				Spec: imagev1.ImageUpdateAutomationSpec{
					Interval: metav1.Duration{Duration: 2 * time.Hour}, // this is to ensure any subsequent run should be outside the scope of the testing
					SourceRef: imagev1.CrossNamespaceSourceReference{
						Kind:      "GitRepository",
						Name:      gitRepoKey.Name,
						Namespace: gitRepoKey.Namespace,
					},
					GitSpec: &imagev1.GitSpec{
						Checkout: &imagev1.GitCheckoutSpec{
							Reference: sourcev1.GitRepositoryRef{
								Branch: branch,
							},
						},
						Commit: imagev1.CommitSpec{
							MessageTemplate: commitTemplate,
							Author: imagev1.CommitUser{
								Name:  authorName,
								Email: authorEmail,
							},
						},
					},
					Update: &imagev1.UpdateStrategy{
						Strategy: imagev1.UpdateStrategySetters,
					},
				},
			}

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId = commitIdFromBranch(localRepo, branch)
			Expect(k8sClient.Create(context.Background(), updateBySetters)).To(Succeed())
			// wait for a new commit to be made by the controller
			waitForNewHead(localRepo, branch, preChangeCommitId)
		})

		AfterEach(func() {
			imageAutoReconciler.NoCrossNamespaceRef = false
			Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())
		})

		It("formats the commit message as in the template", func() {
			head, _ := headCommit(localRepo)
			commit, err := localRepo.LookupCommit(head.Id())
			Expect(err).ToNot(HaveOccurred())
			Expect(commit.Message()).To(Equal(commitMessage))
		})

		It("has the commit author as given", func() {
			head, _ := headCommit(localRepo)
			commit, err := localRepo.LookupCommit(head.Id())
			Expect(err).ToNot(HaveOccurred())

			signature := commit.Author()
			Expect(signature).NotTo(BeNil())
			Expect(signature.Name).To(Equal(authorName))
			Expect(signature.Email).To(Equal(authorEmail))
		})
	})

	Context("ref cross-ns GitRepository", func() {
		var (
			localRepo       *git2go.Repository
			commitMessage   string
			updateBySetters *imagev1.ImageUpdateAutomation
		)

		const (
			authorName     = "Flux B Ot"
			authorEmail    = "fluxbot@example.com"
			commitTemplate = `Commit summary

Automation: {{ .AutomationObject }}

Files:
{{ range $filename, $_ := .Updated.Files -}}
- {{ $filename }}
{{ end -}}

Objects:
{{ range $resource, $_ := .Updated.Objects -}}
{{ if eq $resource.Kind "Deployment" -}}
- {{ $resource.Kind | lower }} {{ $resource.Name | lower }}
{{ else -}}
- {{ $resource.Kind }} {{ $resource.Name }}
{{ end -}}
{{ end -}}

Images:
{{ range .Updated.Images -}}
- {{.}} ({{.Policy.Name}})
{{ end -}}
`
			commitMessageFmt = `Commit summary

Automation: %s/update-test

Files:
- deploy.yaml
Objects:
- deployment test
Images:
- helloworld:v1.0.0 (%s)
`
		)

		BeforeEach(func() {
			Expect(initGitRepo(gitServer, "testdata/appconfig", branch, repositoryPath)).To(Succeed())
			repoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
			var err error
			localRepo, err = clone(repoURL, "origin", branch)
			Expect(err).ToNot(HaveOccurred())

			// A different namespace for the GitRepository.
			gitRepoNamespace := &corev1.Namespace{}
			gitRepoNamespace.Name = "cross-ns-git-repo" + randStringRunes(5)
			Expect(k8sClient.Create(context.Background(), gitRepoNamespace)).To(Succeed())

			gitRepoKey := types.NamespacedName{
				Name:      "image-auto-" + randStringRunes(5),
				Namespace: gitRepoNamespace.Name,
			}
			gitRepo := &sourcev1.GitRepository{
				ObjectMeta: metav1.ObjectMeta{
					Name:      gitRepoKey.Name,
					Namespace: gitRepoKey.Namespace,
				},
				Spec: sourcev1.GitRepositorySpec{
					URL:      repoURL,
					Interval: metav1.Duration{Duration: time.Minute},
				},
			}
			Expect(k8sClient.Create(context.Background(), gitRepo)).To(Succeed())
			policyKey := types.NamespacedName{
				Name:      "policy-" + randStringRunes(5),
				Namespace: namespace.Name,
			}
			// NB not testing the image reflector controller; this
			// will make a "fully formed" ImagePolicy object.
			policy := &imagev1_reflect.ImagePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policyKey.Name,
					Namespace: policyKey.Namespace,
				},
				Spec: imagev1_reflect.ImagePolicySpec{
					ImageRepositoryRef: meta.NamespacedObjectReference{
						Name: "not-expected-to-exist",
					},
					Policy: imagev1_reflect.ImagePolicyChoice{
						SemVer: &imagev1_reflect.SemVerPolicy{
							Range: "1.x",
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			policy.Status.LatestImage = "helloworld:v1.0.0"
			Expect(k8sClient.Status().Update(context.Background(), policy)).To(Succeed())

			// Format the expected message given the generated values
			commitMessage = fmt.Sprintf(commitMessageFmt, namespace.Name, policyKey.Name)

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId := commitIdFromBranch(localRepo, branch)

			// Insert a setter reference into the deployment file,
			// before creating the automation object itself.
			commitInRepo(repoURL, branch, "Install setter marker", func(tmp string) {
				Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			waitForNewHead(localRepo, branch, preChangeCommitId)

			// now create the automation object, and let it (one
			// hopes!) make a commit itself.
			updateKey := types.NamespacedName{
				Namespace: namespace.Name,
				Name:      "update-test",
			}
			updateBySetters = &imagev1.ImageUpdateAutomation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      updateKey.Name,
					Namespace: updateKey.Namespace,
				},
				Spec: imagev1.ImageUpdateAutomationSpec{
					Interval: metav1.Duration{Duration: 2 * time.Hour}, // this is to ensure any subsequent run should be outside the scope of the testing
					SourceRef: imagev1.CrossNamespaceSourceReference{
						Kind:      "GitRepository",
						Name:      gitRepoKey.Name,
						Namespace: gitRepoKey.Namespace,
					},
					GitSpec: &imagev1.GitSpec{
						Checkout: &imagev1.GitCheckoutSpec{
							Reference: sourcev1.GitRepositoryRef{
								Branch: branch,
							},
						},
						Commit: imagev1.CommitSpec{
							MessageTemplate: commitTemplate,
							Author: imagev1.CommitUser{
								Name:  authorName,
								Email: authorEmail,
							},
						},
					},
					Update: &imagev1.UpdateStrategy{
						Strategy: imagev1.UpdateStrategySetters,
					},
				},
			}

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId = commitIdFromBranch(localRepo, branch)
			Expect(k8sClient.Create(context.Background(), updateBySetters)).To(Succeed())
			// wait for a new commit to be made by the controller
			waitForNewHead(localRepo, branch, preChangeCommitId)
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())
		})

		It("formats the commit message as in the template", func() {
			head, _ := headCommit(localRepo)
			commit, err := localRepo.LookupCommit(head.Id())
			Expect(err).ToNot(HaveOccurred())
			Expect(commit.Message()).To(Equal(commitMessage))
		})

		It("has the commit author as given", func() {
			head, _ := headCommit(localRepo)
			commit, err := localRepo.LookupCommit(head.Id())
			Expect(err).ToNot(HaveOccurred())

			signature := commit.Author()
			Expect(signature).NotTo(BeNil())
			Expect(signature.Name).To(Equal(authorName))
			Expect(signature.Email).To(Equal(authorEmail))
		})

		It("fails to reconcile if cross-namespace flag is set", func() {
			imageAutoReconciler.NoCrossNamespaceRef = true

			// trigger reconcile
			var updatePatch imagev1.ImageUpdateAutomation
			Expect(k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(updateBySetters), &updatePatch)).To(Succeed())
			updatePatch.Spec.Interval = metav1.Duration{Duration: 5 * time.Minute}
			Expect(k8sClient.Patch(context.Background(), &updatePatch, client.Merge)).To(Succeed())

			resultAuto := &imagev1.ImageUpdateAutomation{}
			var readyCondition *metav1.Condition

			Eventually(func() bool {
				_ = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(updateBySetters), resultAuto)
				readyCondition = apimeta.FindStatusCondition(resultAuto.Status.Conditions, meta.ReadyCondition)
				return apimeta.IsStatusConditionFalse(resultAuto.Status.Conditions, meta.ReadyCondition)
			}, timeout, time.Second).Should(BeTrue())

			Expect(readyCondition).ToNot(BeNil())
			Expect(readyCondition.Reason).To(Equal(acl.AccessDeniedReason))
		})
	})

	Context("update path", func() {

		var localRepo *git2go.Repository
		const commitTemplate = `Commit summary

{{ range $resource, $_ := .Updated.Objects -}}
- {{ $resource.Name }}
{{ end -}}
`

		BeforeEach(func() {
			Expect(initGitRepo(gitServer, "testdata/pathconfig", branch, repositoryPath)).To(Succeed())
			repoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
			var err error
			localRepo, err = clone(repoURL, "origin", branch)
			Expect(err).ToNot(HaveOccurred())

			gitRepoKey := types.NamespacedName{
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
			policyKey := types.NamespacedName{
				Name:      "policy-" + randStringRunes(5),
				Namespace: namespace.Name,
			}
			// NB not testing the image reflector controller; this
			// will make a "fully formed" ImagePolicy object.
			policy := &imagev1_reflect.ImagePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policyKey.Name,
					Namespace: policyKey.Namespace,
				},
				Spec: imagev1_reflect.ImagePolicySpec{
					ImageRepositoryRef: meta.NamespacedObjectReference{
						Name: "not-expected-to-exist",
					},
					Policy: imagev1_reflect.ImagePolicyChoice{
						SemVer: &imagev1_reflect.SemVerPolicy{
							Range: "1.x",
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			policy.Status.LatestImage = "helloworld:v1.0.0"
			Expect(k8sClient.Status().Update(context.Background(), policy)).To(Succeed())

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId := commitIdFromBranch(localRepo, branch)

			// Insert a setter reference into the deployment file,
			// before creating the automation object itself.
			commitInRepo(repoURL, branch, "Install setter marker", func(tmp string) {
				Expect(replaceMarker(path.Join(tmp, "yes"), policyKey)).To(Succeed())
			})
			commitInRepo(repoURL, branch, "Install setter marker", func(tmp string) {
				Expect(replaceMarker(path.Join(tmp, "no"), policyKey)).To(Succeed())
			})

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			waitForNewHead(localRepo, branch, preChangeCommitId)

			// now create the automation object, and let it (one
			// hopes!) make a commit itself.
			updateKey := types.NamespacedName{
				Namespace: namespace.Name,
				Name:      "update-test",
			}
			updateBySetters := &imagev1.ImageUpdateAutomation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      updateKey.Name,
					Namespace: updateKey.Namespace,
				},
				Spec: imagev1.ImageUpdateAutomationSpec{
					Interval: metav1.Duration{Duration: 2 * time.Hour}, // this is to ensure any subsequent run should be outside the scope of the testing
					Update: &imagev1.UpdateStrategy{
						Strategy: imagev1.UpdateStrategySetters,
						Path:     "./yes",
					},
					SourceRef: imagev1.CrossNamespaceSourceReference{
						Kind:      "GitRepository",
						Name:      gitRepoKey.Name,
						Namespace: gitRepoKey.Namespace,
					},
					GitSpec: &imagev1.GitSpec{
						Checkout: &imagev1.GitCheckoutSpec{
							Reference: sourcev1.GitRepositoryRef{
								Branch: branch,
							},
						},
						Commit: imagev1.CommitSpec{
							Author: imagev1.CommitUser{
								Name:  authorName,
								Email: authorEmail,
							},
							MessageTemplate: commitTemplate,
						},
					},
				},
			}

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId = commitIdFromBranch(localRepo, branch)
			Expect(k8sClient.Create(context.Background(), updateBySetters)).To(Succeed())
			// wait for a new commit to be made by the controller
			waitForNewHead(localRepo, branch, preChangeCommitId)
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())
		})

		It("updates only the deployment in the specified path", func() {
			head, _ := headCommit(localRepo)
			commit, err := localRepo.LookupCommit(head.Id())
			Expect(err).ToNot(HaveOccurred())
			Expect(commit.Message()).To(Not(ContainSubstring("update-no")))
			Expect(commit.Message()).To(ContainSubstring("update-yes"))
		})
	})

	Context("commit signing", func() {

		var (
			localRepo *git2go.Repository
			pgpEntity *openpgp.Entity
		)

		BeforeEach(func() {
			Expect(initGitRepo(gitServer, "testdata/appconfig", branch, repositoryPath)).To(Succeed())
			repoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
			var err error
			localRepo, err = clone(repoURL, "origin", branch)
			Expect(err).ToNot(HaveOccurred())

			gitRepoKey := types.NamespacedName{
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
			policyKey := types.NamespacedName{
				Name:      "policy-" + randStringRunes(5),
				Namespace: namespace.Name,
			}
			// NB not testing the image reflector controller; this
			// will make a "fully formed" ImagePolicy object.
			policy := &imagev1_reflect.ImagePolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      policyKey.Name,
					Namespace: policyKey.Namespace,
				},
				Spec: imagev1_reflect.ImagePolicySpec{
					ImageRepositoryRef: meta.NamespacedObjectReference{
						Name: "not-expected-to-exist",
					},
					Policy: imagev1_reflect.ImagePolicyChoice{
						SemVer: &imagev1_reflect.SemVerPolicy{
							Range: "1.x",
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
			policy.Status.LatestImage = "helloworld:v1.0.0"
			Expect(k8sClient.Status().Update(context.Background(), policy)).To(Succeed())

			// Insert a setter reference into the deployment file,
			// before creating the automation object itself.
			commitInRepo(repoURL, branch, "Install setter marker", func(tmp string) {
				Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId := commitIdFromBranch(localRepo, branch)

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			waitForNewHead(localRepo, branch, preChangeCommitId)

			// generate keypair for signing
			pgpEntity, err = openpgp.NewEntity("", "", "", nil)
			Expect(err).ToNot(HaveOccurred())

			// configure OpenPGP armor encoder
			b := bytes.NewBuffer(nil)
			w, err := armor.Encode(b, openpgp.PrivateKeyType, nil)
			Expect(err).ToNot(HaveOccurred())

			// serialize private key
			err = pgpEntity.SerializePrivate(w, nil)
			Expect(err).ToNot(HaveOccurred())
			err = w.Close()
			Expect(err).ToNot(HaveOccurred())

			// create the secret containing signing key
			sec := &corev1.Secret{
				Data: map[string][]byte{
					"git.asc": b.Bytes(),
				},
			}
			sec.Name = "signing-key-secret-" + randStringRunes(5)
			sec.Namespace = namespace.Name
			Expect(k8sClient.Create(context.Background(), sec)).To(Succeed())

			// now create the automation object, and let it (one
			// hopes!) make a commit itself.
			updateKey := types.NamespacedName{
				Namespace: namespace.Name,
				Name:      "update-test",
			}
			updateBySetters := &imagev1.ImageUpdateAutomation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      updateKey.Name,
					Namespace: updateKey.Namespace,
				},
				Spec: imagev1.ImageUpdateAutomationSpec{
					SourceRef: imagev1.CrossNamespaceSourceReference{
						Kind:      "GitRepository",
						Name:      gitRepoKey.Name,
						Namespace: gitRepoKey.Namespace,
					},
					Interval: metav1.Duration{Duration: 2 * time.Hour}, // this is to ensure any subsequent run should be outside the scope of the testing
					GitSpec: &imagev1.GitSpec{
						Checkout: &imagev1.GitCheckoutSpec{
							Reference: sourcev1.GitRepositoryRef{
								Branch: branch,
							},
						},
						Commit: imagev1.CommitSpec{
							Author: imagev1.CommitUser{
								Name:  authorName,
								Email: authorEmail,
							},
							SigningKey: &imagev1.SigningKey{
								SecretRef: meta.LocalObjectReference{Name: sec.Name},
							},
						},
					},
					Update: &imagev1.UpdateStrategy{
						Strategy: imagev1.UpdateStrategySetters,
					},
				},
			}

			preChangeCommitId = commitIdFromBranch(localRepo, branch)
			Expect(k8sClient.Create(context.Background(), updateBySetters)).To(Succeed())
			// wait for a new commit to be made by the controller
			waitForNewHead(localRepo, branch, preChangeCommitId)
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())
		})

		It("signs the commit with the generated GPG key", func() {
			head, _ := headCommit(localRepo)
			commit, err := localRepo.LookupCommit(head.Id())
			Expect(err).ToNot(HaveOccurred())

			// verify commit
			commitSig, commitContent, err := commit.ExtractSignature()
			Expect(err).ToNot(HaveOccurred())

			kr := openpgp.EntityList([]*openpgp.Entity{pgpEntity})
			signature := strings.NewReader(commitSig)
			content := strings.NewReader(commitContent)

			_, err = openpgp.CheckArmoredDetachedSignature(kr, content, signature)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	endToEnd := func(impl, proto string) func() {
		return func() {
			var (
				// for cloning locally
				cloneLocalRepoURL string
				// for the controller
				repoURL       string
				localRepo     *git2go.Repository
				policy        *imagev1_reflect.ImagePolicy
				policyKey     types.NamespacedName
				gitRepoKey    types.NamespacedName
				commitMessage string
			)

			const latestImage = "helloworld:1.0.1"

			BeforeEach(func() {
				cloneLocalRepoURL = gitServer.HTTPAddressWithCredentials() + repositoryPath
				if proto == "http" {
					repoURL = cloneLocalRepoURL // NB not testing auth for git over HTTP
				} else if proto == "ssh" {
					sshURL := gitServer.SSHAddress()
					// this is expected to use 127.0.0.1, but host key
					// checking usually wants a hostname, so use
					// "localhost".
					sshURL = strings.Replace(sshURL, "127.0.0.1", "localhost", 1)
					repoURL = sshURL + repositoryPath
					go func() {
						defer GinkgoRecover()
						gitServer.StartSSH()
					}()
				} else {
					Fail("proto not set to http or ssh")
				}

				commitMessage = "Commit a difference " + randStringRunes(5)

				Expect(initGitRepo(gitServer, "testdata/appconfig", branch, repositoryPath)).To(Succeed())

				var err error
				localRepo, err = clone(cloneLocalRepoURL, "origin", branch)
				Expect(err).ToNot(HaveOccurred())

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
						URL:               repoURL,
						Interval:          metav1.Duration{Duration: time.Minute},
						GitImplementation: impl,
					},
				}

				// If using SSH, we need to provide an identity (private
				// key) and known_hosts file in a secret.
				if proto == "ssh" {
					url, err := url.Parse(repoURL)
					Expect(err).ToNot(HaveOccurred())
					knownhosts, err := ssh.ScanHostKey(url.Host, 5*time.Second)
					Expect(err).ToNot(HaveOccurred())
					keygen := ssh.NewRSAGenerator(2048)
					pair, err := keygen.Generate()
					Expect(err).ToNot(HaveOccurred())

					sec := &corev1.Secret{
						StringData: map[string]string{
							"known_hosts":  string(knownhosts),
							"identity":     string(pair.PrivateKey),
							"identity.pub": string(pair.PublicKey),
						},
					}
					sec.Name = "git-secret-" + randStringRunes(5)
					sec.Namespace = namespace.Name
					Expect(k8sClient.Create(context.Background(), sec)).To(Succeed())
					gitRepo.Spec.SecretRef = &meta.LocalObjectReference{Name: sec.Name}
				}

				Expect(k8sClient.Create(context.Background(), gitRepo)).To(Succeed())

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
						ImageRepositoryRef: meta.NamespacedObjectReference{
							Name: "not-expected-to-exist",
						},
						Policy: imagev1_reflect.ImagePolicyChoice{
							SemVer: &imagev1_reflect.SemVerPolicy{
								Range: "1.x",
							},
						},
					},
				}
				Expect(k8sClient.Create(context.Background(), policy)).To(Succeed())
				policy.Status.LatestImage = latestImage
				Expect(k8sClient.Status().Update(context.Background(), policy)).To(Succeed())

			})

			AfterEach(func() {
				Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())
				Expect(k8sClient.Delete(context.Background(), policy)).To(Succeed())
				Expect(gitServer.StopSSH()).To(Succeed())
			})

			Context("with PushSpec", func() {

				var (
					update     *imagev1.ImageUpdateAutomation
					pushBranch string
				)

				BeforeEach(func() {
					// pull the head commit we just pushed, so it's not
					// considered a new commit when checking for a commit
					// made by automation.
					preChangeCommitId := commitIdFromBranch(localRepo, branch)
					commitInRepo(cloneLocalRepoURL, branch, "Install setter marker", func(tmp string) {
						Expect(replaceMarker(tmp, policyKey)).To(Succeed())
					})
					waitForNewHead(localRepo, branch, preChangeCommitId)

					pushBranch = "pr-" + randStringRunes(5)

					update = &imagev1.ImageUpdateAutomation{
						Spec: imagev1.ImageUpdateAutomationSpec{
							SourceRef: imagev1.CrossNamespaceSourceReference{
								Kind:      "GitRepository",
								Name:      gitRepoKey.Name,
								Namespace: gitRepoKey.Namespace,
							},
							Update: &imagev1.UpdateStrategy{
								Strategy: imagev1.UpdateStrategySetters,
							},
							Interval: metav1.Duration{Duration: 2 * time.Hour},
							GitSpec: &imagev1.GitSpec{
								Checkout: &imagev1.GitCheckoutSpec{
									Reference: sourcev1.GitRepositoryRef{
										Branch: branch,
									},
								},
								Commit: imagev1.CommitSpec{
									Author: imagev1.CommitUser{
										Name:  authorName,
										Email: authorEmail,
									},
									MessageTemplate: commitMessage,
								},
								Push: &imagev1.PushSpec{
									Branch: pushBranch,
								},
							},
						},
					}
					update.Name = "update-" + randStringRunes(5)
					update.Namespace = namespace.Name

					Expect(k8sClient.Create(context.Background(), update)).To(Succeed())
				})

				It("creates and pushes the push branch", func() {
					// pull the head commit we just pushed, so it's not
					// considered a new commit when checking for a commit
					// made by automation.
					preChangeCommitId := commitIdFromBranch(localRepo, branch)

					waitForNewHead(localRepo, pushBranch, preChangeCommitId)

					head, err := getRemoteHead(localRepo, pushBranch)
					Expect(err).NotTo(HaveOccurred())
					commit, err := localRepo.LookupCommit(head)
					Expect(err).ToNot(HaveOccurred())
					defer commit.Free()
					Expect(commit.Message()).To(Equal(commitMessage))
				})

				It("pushes another commit to the existing push branch", func() {
					// pull the head commit we just pushed, so it's not
					// considered a new commit when checking for a commit
					// made by automation.
					preChangeCommitId := commitIdFromBranch(localRepo, branch)

					// observe the first commit
					waitForNewHead(localRepo, pushBranch, preChangeCommitId)
					head, err := getRemoteHead(localRepo, pushBranch)
					headHash := head.String()
					Expect(err).NotTo(HaveOccurred())

					// update the policy and expect another commit in the push branch
					policy.Status.LatestImage = "helloworld:v1.3.0"
					Expect(k8sClient.Status().Update(context.TODO(), policy)).To(Succeed())

					preChangeCommitId = commitIdFromBranch(localRepo, branch)
					waitForNewHead(localRepo, pushBranch, preChangeCommitId)

					head, err = getRemoteHead(localRepo, pushBranch)
					Expect(err).NotTo(HaveOccurred())
					Expect(head.String()).NotTo(Equal(headHash))
				})

				It("still pushes to the push branch after it's merged", func() {
					preChangeCommitId := commitIdFromBranch(localRepo, branch)

					// observe the first commit
					waitForNewHead(localRepo, pushBranch, preChangeCommitId)
					head, err := getRemoteHead(localRepo, pushBranch)
					Expect(err).NotTo(HaveOccurred())
					headHash := head.String()

					// merge the push branch into checkout branch, and push the merge commit
					// upstream.
					// waitForNewHead() leaves the repo at the head of the branch given, i.e., the
					// push branch), so we have to check out the "main" branch first.
					r, err := rebase(localRepo, pushBranch, branch)
					Expect(err).ToNot(HaveOccurred())
					err = r.Finish()
					Expect(err).ToNot(HaveOccurred())
					defer r.Free()

					// update the policy and expect another commit in the push branch
					preChangeCommitId = commitIdFromBranch(localRepo, branch)
					policy.Status.LatestImage = "helloworld:v1.3.0"
					Expect(k8sClient.Status().Update(context.TODO(), policy)).To(Succeed())
					waitForNewHead(localRepo, pushBranch, preChangeCommitId)

					head, err = getRemoteHead(localRepo, pushBranch)
					Expect(err).NotTo(HaveOccurred())
					Expect(head.String()).NotTo(Equal(headHash))
				})

				AfterEach(func() {
					Expect(k8sClient.Delete(context.Background(), update)).To(Succeed())
				})

			})

			Context("with Setters", func() {

				var (
					updateKey       types.NamespacedName
					updateBySetters *imagev1.ImageUpdateAutomation
				)

				BeforeEach(func() {
					preChangeCommitId := commitIdFromBranch(localRepo, branch)
					// Insert a setter reference into the deployment file,
					// before creating the automation object itself.
					commitInRepo(cloneLocalRepoURL, branch, "Install setter marker", func(tmp string) {
						Expect(replaceMarker(tmp, policyKey)).To(Succeed())
					})

					// pull the head commit we just pushed, so it's not
					// considered a new commit when checking for a commit
					// made by automation.
					waitForNewHead(localRepo, branch, preChangeCommitId)

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
							SourceRef: imagev1.CrossNamespaceSourceReference{
								Kind:      "GitRepository",
								Name:      gitRepoKey.Name,
								Namespace: gitRepoKey.Namespace,
							},
							Update: &imagev1.UpdateStrategy{
								Strategy: imagev1.UpdateStrategySetters,
							},
							GitSpec: &imagev1.GitSpec{
								Checkout: &imagev1.GitCheckoutSpec{
									Reference: sourcev1.GitRepositoryRef{
										Branch: branch,
									},
								},
								Commit: imagev1.CommitSpec{
									Author: imagev1.CommitUser{
										Name:  authorName,
										Email: authorEmail,
									},
									MessageTemplate: commitMessage,
								},
							},
						},
					}
					preChangeCommitId = commitIdFromBranch(localRepo, branch)
					Expect(k8sClient.Create(context.Background(), updateBySetters)).To(Succeed())
					// wait for a new commit to be made by the controller
					waitForNewHead(localRepo, branch, preChangeCommitId)
				})

				AfterEach(func() {
					Expect(k8sClient.Delete(context.Background(), updateBySetters)).To(Succeed())
				})

				It("updates to the most recent image", func() {
					// having passed the BeforeEach, we should see a commit
					commit, err := headCommit(localRepo)
					Expect(err).ToNot(HaveOccurred())

					defer commit.Free()
					Expect(commit.Message()).To(Equal(commitMessage))

					var newObj imagev1.ImageUpdateAutomation
					Expect(k8sClient.Get(context.Background(), updateKey, &newObj)).To(Succeed())
					Expect(newObj.Status.LastPushCommit).To(Equal(commit.Id().String()))
					Expect(newObj.Status.LastPushTime).ToNot(BeNil())

					compareRepoWithExpected(cloneLocalRepoURL, branch, "testdata/appconfig-setters-expected", func(tmp string) {
						Expect(replaceMarker(tmp, policyKey)).To(Succeed())
					})
				})

				It("stops updating when suspended", func() {
					// suspend it, and check that reconciliation does not run
					var updatePatch imagev1.ImageUpdateAutomation
					Expect(k8sClient.Get(context.TODO(), updateKey, &updatePatch)).To(Succeed())
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
				})
			})
		}
	}

	Context("Using go-git", func() {
		Context("with HTTP", func() {
			Describe("runs end to end", endToEnd(sourcev1.GoGitImplementation, "http"))
		})
		Context("with SSH", func() {
			Describe("runs end to end", endToEnd(sourcev1.GoGitImplementation, "ssh"))
		})
	})

	Context("Using libgit2", func() {
		Context("with HTTP", func() {
			Describe("runs end to end", endToEnd(sourcev1.LibGit2Implementation, "http"))
		})
		Context("with SSH", func() {
			Describe("runs end to end", endToEnd(sourcev1.LibGit2Implementation, "ssh"))
		})
	})

	Context("defaulting", func() {
		var key types.NamespacedName
		var auto *imagev1.ImageUpdateAutomation

		BeforeEach(func() {
			key = types.NamespacedName{
				Namespace: namespace.Name,
				Name:      "update-" + randStringRunes(5),
			}
			auto = &imagev1.ImageUpdateAutomation{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: imagev1.ImageUpdateAutomationSpec{
					SourceRef: imagev1.CrossNamespaceSourceReference{
						Kind:      "GitRepository",
						Name:      "garbage",
						Namespace: key.Namespace,
					},
					Interval: metav1.Duration{Duration: 2 * time.Hour}, // this is to ensure any subsequent run should be outside the scope of the testing
					GitSpec: &imagev1.GitSpec{
						Checkout: &imagev1.GitCheckoutSpec{
							Reference: sourcev1.GitRepositoryRef{
								Branch: branch,
							},
						},
						// leave Update field out
						Commit: imagev1.CommitSpec{
							Author: imagev1.CommitUser{
								Name:  authorName,
								Email: authorEmail,
							},
							MessageTemplate: "nothing",
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), auto)).To(Succeed())
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), auto)).To(Succeed())
		})

		It("defaults .spec.update to {strategy: Setters}", func() {
			var fetchedAuto imagev1.ImageUpdateAutomation
			Expect(k8sClient.Get(context.Background(), key, &fetchedAuto)).To(Succeed())
			Expect(fetchedAuto.Spec.Update).To(Equal(&imagev1.UpdateStrategy{Strategy: imagev1.UpdateStrategySetters}))
		})
	})
})

func checkoutBranch(repo *git2go.Repository, branch string) error {
	sl, err := repo.StatusList(&git2go.StatusOptions{
		Show: git2go.StatusShowIndexAndWorkdir,
	})
	if err != nil {
		return err
	}
	defer sl.Free()

	count, err := sl.EntryCount()
	if err != nil {
		return err
	}
	// check that there's no local changes, as a sanity check
	if count > 0 {
		for i := 0; i < count; i++ {
			s, err := sl.ByIndex(i)
			if err == nil {
				fmt.Println(s.HeadToIndex.NewFile, " is changed")
			}
		}
	} // the checkout next will fail if there are changed files

	return repo.SetHead(fmt.Sprintf("refs/heads/%s", branch))
}

func expectCommittedAndPushed(conditions []metav1.Condition) {
	rc := apimeta.FindStatusCondition(conditions, meta.ReadyCondition)
	Expect(rc).ToNot(BeNil())
	Expect(rc.Message).To(ContainSubstring("committed and pushed"))
}

func replaceMarker(path string, policyKey types.NamespacedName) error {
	// NB this requires knowledge of what's in the git repo, so a little brittle
	deployment := filepath.Join(path, "deploy.yaml")
	filebytes, err := os.ReadFile(deployment)
	if err != nil {
		return err
	}
	newfilebytes := bytes.ReplaceAll(filebytes, []byte("SETTER_SITE"), []byte(setterRef(policyKey)))
	if err = os.WriteFile(deployment, newfilebytes, os.FileMode(0666)); err != nil {
		return err
	}
	return nil
}

func setterRef(name types.NamespacedName) string {
	return fmt.Sprintf(`{"%s": "%s:%s"}`, update.SetterShortHand, name.Namespace, name.Name)
}

func compareRepoWithExpected(repoURL, branch, fixture string, changeFixture func(tmp string)) {
	expected, err := os.MkdirTemp("", "gotest-imageauto-expected")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(expected)

	copy.Copy(fixture, expected)
	changeFixture(expected)

	repo, err := clone(repoURL, "origin", branch)
	Expect(err).ToNot(HaveOccurred())
	actual := repo.Workdir()
	defer os.RemoveAll(actual)

	Expect(err).ToNot(HaveOccurred())
	test.ExpectMatchingDirectories(actual, expected)
}

func commitInRepo(repoURL, branch, msg string, changeFiles func(path string)) {
	originRemote := "origin"
	repo, err := clone(repoURL, originRemote, branch)
	Expect(err).ToNot(HaveOccurred())

	changeFiles(repo.Workdir())

	sig := &git2go.Signature{
		Name:  "Testbot",
		Email: "test@example.com",
		When:  time.Now(),
	}
	_, err = commitWorkDir(repo, branch, msg, sig)
	Expect(err).ToNot(HaveOccurred())

	origin, err := repo.Remotes.Lookup(originRemote)
	if err != nil {
		panic(fmt.Errorf("cannot find origin: %v", err))
	}
	defer origin.Free()

	Expect(origin.Push([]string{branchRefName(branch)}, &libgit2.PushOptions{})).To(Succeed())
}

// Initialise a git server with a repo including the files in dir.
func initGitRepo(gitServer *gittestserver.GitServer, fixture, branch, repositoryPath string) error {
	workDir, err := securejoin.SecureJoin(gitServer.Root(), repositoryPath)
	if err != nil {
		return err
	}

	repo, err := initGitRepoPlain(fixture, workDir)
	if err != nil {
		return err
	}

	commitID, err := headCommit(repo)
	if err != nil {
		return err
	}

	_, err = repo.CreateBranch(branch, commitID, false)
	if err != nil {
		return err
	}

	return repo.Remotes.AddPush("origin", branchRefName(branch))
}

func initGitRepoPlain(fixture, repositoryPath string) (*git2go.Repository, error) {
	repo, err := git2go.InitRepository(repositoryPath, false)
	if err != nil {
		return nil, err
	}

	err = copyDir(fixture, repositoryPath)
	if err != nil {
		return nil, err
	}

	_, err = commitWorkDir(repo, "main", "Initial commit", mockSignature(time.Now()))
	if err != nil {
		return nil, err
	}

	return repo, nil
}

func headFromBranch(repo *git2go.Repository, branchName string) (*git2go.Commit, error) {
	branch, err := repo.LookupBranch(branchName, git2go.BranchAll)
	if err != nil {
		return nil, err
	}
	defer branch.Free()

	return repo.LookupCommit(branch.Reference.Target())
}

func commitWorkDir(repo *git2go.Repository, branchName, message string, sig *git2go.Signature) (*git2go.Oid, error) {
	var parentC []*git2go.Commit
	head, err := headFromBranch(repo, branchName)
	if err == nil {
		defer head.Free()
		parentC = append(parentC, head)
	}

	index, err := repo.Index()
	if err != nil {
		return nil, err
	}
	defer index.Free()

	// add to index any files that are not within .git/
	if err = filepath.Walk(repo.Workdir(),
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(repo.Workdir(), path)
			if err != nil {
				return err
			}
			f, err := os.Stat(path)
			if err != nil {
				return err
			}
			if f.IsDir() || strings.HasPrefix(rel, ".git") || rel == "." {
				return nil
			}
			if err := index.AddByPath(rel); err != nil {
				return err
			}
			return nil
		}); err != nil {
		return nil, err
	}

	if err := index.Write(); err != nil {
		return nil, err
	}

	treeID, err := index.WriteTree()
	if err != nil {
		return nil, err
	}

	tree, err := repo.LookupTree(treeID)
	if err != nil {
		return nil, err
	}
	defer tree.Free()

	c, err := repo.CreateCommit("HEAD", sig, sig, message, tree, parentC...)
	if err != nil {
		return nil, err
	}
	return c, nil
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

			content, err := ioutil.ReadFile(srcFile)
			if err != nil {
				return err
			}

			if err = ioutil.WriteFile(destFile, content, 0o755); err != nil {
				return err
			}
		}
	}

	return nil
}

func branchRefName(branch string) string {
	return fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)
}

// copied from source-controller/pkg/git/libgit2/checkout.go
func commitFile(repo *git2go.Repository, path, content string, time time.Time) (*git2go.Oid, error) {
	var parentC []*git2go.Commit
	head, err := headCommit(repo)
	if err == nil {
		defer head.Free()
		parentC = append(parentC, head)
	}

	index, err := repo.Index()
	if err != nil {
		return nil, err
	}
	defer index.Free()

	blobOID, err := repo.CreateBlobFromBuffer([]byte(content))
	if err != nil {
		return nil, err
	}

	entry := &git2go.IndexEntry{
		Mode: git2go.FilemodeBlob,
		Id:   blobOID,
		Path: path,
	}

	if err := index.Add(entry); err != nil {
		return nil, err
	}
	if err := index.Write(); err != nil {
		return nil, err
	}

	treeID, err := index.WriteTree()
	if err != nil {
		return nil, err
	}

	tree, err := repo.LookupTree(treeID)
	if err != nil {
		return nil, err
	}
	defer tree.Free()

	c, err := repo.CreateCommit("HEAD", mockSignature(time), mockSignature(time), "Committing "+path, tree, parentC...)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// copied from source-controller/pkg/git/libgit2/checkout.go
func mockSignature(time time.Time) *git2go.Signature {
	return &git2go.Signature{
		Name:  "Jane Doe",
		Email: "author@example.com",
		When:  time,
	}
}

func clone(repoURL, remoteName, branchName string) (*git2go.Repository, error) {
	dir, err := os.MkdirTemp("", "iac-clone-*")
	if err != nil {
		return nil, err
	}
	opts := &git2go.CloneOptions{
		Bare:           false,
		CheckoutBranch: branchName,
		CheckoutOptions: git2go.CheckoutOptions{
			Strategy: git2go.CheckoutForce,
		},
		FetchOptions: git2go.FetchOptions{
			RemoteCallbacks: git2go.RemoteCallbacks{
				CertificateCheckCallback: func(cert *git2go.Certificate, valid bool, hostname string) error {
					return nil
				},
			},
		},
	}

	return git2go.Clone(repoURL, dir, opts)
}

func waitForNewHead(repo *git2go.Repository, branch, preChangeHash string) {
	var commitToResetTo *git2go.Commit

	// Now try to fetch new commits from that remote branch
	Eventually(func() bool {
		origin, err := repo.Remotes.Lookup("origin")
		if err != nil {
			panic("origin not set")
		}
		defer origin.Free()

		if err := origin.Fetch(
			[]string{branchRefName(branch)},
			&libgit2.FetchOptions{}, "",
		); err != nil {
			return false
		}

		remoteBranch, err := repo.LookupBranch(branch, git2go.BranchAll)
		if err != nil {
			return false
		}
		defer remoteBranch.Free()

		remoteHeadRef, err := repo.LookupCommit(remoteBranch.Reference.Target())
		if err != nil {
			return false
		}
		defer remoteHeadRef.Free()

		if preChangeHash != remoteHeadRef.Id().String() {
			commitToResetTo, _ = repo.LookupCommit(remoteBranch.Reference.Target())
			return true
		}
		return false
	}, timeout, time.Second).Should(BeTrue())

	if commitToResetTo != nil {
		defer commitToResetTo.Free()
		// New commits in the remote branch -- reset the working tree head
		// to that. Note this does not create a local branch tracking the
		// remote, so it is a detached head.
		Expect(repo.ResetToCommit(commitToResetTo, libgit2.ResetHard,
			&libgit2.CheckoutOptions{})).To(Succeed())
	}
}

func commitIdFromBranch(repo *git2go.Repository, branchName string) string {
	commitId := ""
	head, err := headFromBranch(repo, branchName)

	defer head.Free()
	if err == nil {
		commitId = head.Id().String()
	}
	return commitId
}

func getRemoteHead(repo *git2go.Repository, branchName string) (*git2go.Oid, error) {
	remote, err := repo.Remotes.Lookup("origin")
	if err != nil {
		return nil, err
	}
	defer remote.Free()

	err = remote.Fetch([]string{branchRefName(branchName)}, nil, "")
	if err != nil {
		return nil, err
	}

	remoteBranch, err := repo.LookupBranch(branchName, git2go.BranchAll)
	if err != nil {
		return nil, err
	}
	defer remoteBranch.Free()

	remoteHeadRef, err := repo.LookupCommit(remoteBranch.Reference.Target())
	if err != nil {
		return nil, err
	}
	defer remoteHeadRef.Free()

	return remoteHeadRef.Id(), nil
}

// This merges the push branch into HEAD, and pushes upstream. This is
// to simulate e.g., a PR being merged.
func rebase(repo *git2go.Repository, sourceBranch, targetBranch string) (*git2go.Rebase, error) {
	rebaseOpts, err := git2go.DefaultRebaseOptions()
	Expect(err).NotTo(HaveOccurred())

	err = checkoutBranch(repo, sourceBranch)
	Expect(err).NotTo(HaveOccurred())

	master, err := repo.LookupBranch(targetBranch, git2go.BranchLocal)
	if err != nil {
		return nil, err
	}
	defer master.Free()

	onto, err := repo.AnnotatedCommitFromRef(master.Reference)
	if err != nil {
		return nil, err
	}
	defer onto.Free()

	// Init rebase
	rebase, err := repo.InitRebase(nil, nil, onto, &rebaseOpts)
	if err != nil {
		return nil, err
	}

	// Check no operation has been started yet
	rebaseOperationIndex, err := rebase.CurrentOperationIndex()
	if rebaseOperationIndex != git2go.RebaseNoOperation && err != git2go.ErrRebaseNoOperation {
		return nil, errors.New("No operation should have been started yet")
	}

	// Iterate in rebase operations regarding operation count
	opCount := int(rebase.OperationCount())
	for op := 0; op < opCount; op++ {
		operation, err := rebase.Next()
		if err != nil {
			return nil, err
		}

		// Check operation index is correct
		rebaseOperationIndex, err = rebase.CurrentOperationIndex()
		if int(rebaseOperationIndex) != op {
			return nil, errors.New("Bad operation index")
		}
		if !operationsAreEqual(rebase.OperationAt(uint(op)), operation) {
			return nil, errors.New("Rebase operations should be equal")
		}

		// Get current rebase operation created commit
		commit, err := repo.LookupCommit(operation.Id)
		if err != nil {
			return nil, err
		}
		defer commit.Free()

		// Apply commit
		err = rebase.Commit(operation.Id, commit.Author(), commit.Author(), commit.Message())
		if err != nil {
			return nil, err
		}
	}

	return rebase, nil
}

func operationsAreEqual(l, r *git2go.RebaseOperation) bool {
	return l.Exec == r.Exec && l.Type == r.Type && l.Id.String() == r.Id.String()
}
