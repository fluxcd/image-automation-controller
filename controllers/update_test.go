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
	"math/rand"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
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

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta1"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/gittestserver"
	"github.com/fluxcd/pkg/ssh"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta1"
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
		branch             string
		repositoryPath     string
		namespace          *corev1.Namespace
		username, password string
		gitServer          *gittestserver.GitServer
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
			localRepo     *git.Repository
			commitMessage string
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
			localRepo, err = git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
				URL:           repoURL,
				RemoteName:    "origin",
				ReferenceName: plumbing.NewBranchReferenceName(branch),
			})
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
			waitForNewHead(localRepo, branch)

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
			Expect(k8sClient.Create(context.Background(), updateBySetters)).To(Succeed())
			// wait for a new commit to be made by the controller
			waitForNewHead(localRepo, branch)
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())
		})

		It("formats the commit message as in the template", func() {
			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			Expect(err).ToNot(HaveOccurred())
			Expect(commit.Message).To(Equal(commitMessage))
		})

		It("has the commit author as given", func() {
			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			Expect(err).ToNot(HaveOccurred())
			Expect(commit.Author).NotTo(BeNil())
			Expect(commit.Author.Name).To(Equal(authorName))
			Expect(commit.Author.Email).To(Equal(authorEmail))
		})
	})

	Context("ref cross-ns GitRepository", func() {
		var (
			localRepo     *git.Repository
			commitMessage string
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
			localRepo, err = git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
				URL:           repoURL,
				RemoteName:    "origin",
				ReferenceName: plumbing.NewBranchReferenceName(branch),
			})
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

			// Insert a setter reference into the deployment file,
			// before creating the automation object itself.
			commitInRepo(repoURL, branch, "Install setter marker", func(tmp string) {
				Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			waitForNewHead(localRepo, branch)

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
			Expect(k8sClient.Create(context.Background(), updateBySetters)).To(Succeed())
			// wait for a new commit to be made by the controller
			waitForNewHead(localRepo, branch)
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())
		})

		It("formats the commit message as in the template", func() {
			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			Expect(err).ToNot(HaveOccurred())
			Expect(commit.Message).To(Equal(commitMessage))
		})

		It("has the commit author as given", func() {
			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			Expect(err).ToNot(HaveOccurred())
			Expect(commit.Author).NotTo(BeNil())
			Expect(commit.Author.Name).To(Equal(authorName))
			Expect(commit.Author.Email).To(Equal(authorEmail))
		})
	})

	Context("update path", func() {

		var localRepo *git.Repository
		const commitTemplate = `Commit summary

{{ range $resource, $_ := .Updated.Objects -}}
- {{ $resource.Name }}
{{ end -}}
`

		BeforeEach(func() {
			Expect(initGitRepo(gitServer, "testdata/pathconfig", branch, repositoryPath)).To(Succeed())
			repoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
			var err error
			localRepo, err = git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
				URL:           repoURL,
				RemoteName:    "origin",
				ReferenceName: plumbing.NewBranchReferenceName(branch),
			})
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
				Expect(replaceMarker(path.Join(tmp, "yes"), policyKey)).To(Succeed())
			})
			commitInRepo(repoURL, branch, "Install setter marker", func(tmp string) {
				Expect(replaceMarker(path.Join(tmp, "no"), policyKey)).To(Succeed())
			})

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			waitForNewHead(localRepo, branch)

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
								Email: "fluxbot@example.com",
							},
							MessageTemplate: commitTemplate,
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), updateBySetters)).To(Succeed())
			// wait for a new commit to be made by the controller
			waitForNewHead(localRepo, branch)
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())
		})

		It("updates only the deployment in the specified path", func() {
			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			Expect(err).ToNot(HaveOccurred())
			Expect(commit.Message).To(Not(ContainSubstring("update-no")))
			Expect(commit.Message).To(ContainSubstring("update-yes"))
		})
	})

	Context("commit signing", func() {

		var (
			localRepo *git.Repository
			pgpEntity *openpgp.Entity
		)

		BeforeEach(func() {
			Expect(initGitRepo(gitServer, "testdata/appconfig", branch, repositoryPath)).To(Succeed())
			repoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
			var err error
			localRepo, err = git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
				URL:           repoURL,
				RemoteName:    "origin",
				ReferenceName: plumbing.NewBranchReferenceName(branch),
			})
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
			waitForNewHead(localRepo, branch)

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

			Expect(k8sClient.Create(context.Background(), updateBySetters)).To(Succeed())
			// wait for a new commit to be made by the controller
			waitForNewHead(localRepo, branch)
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), namespace)).To(Succeed())
		})

		It("signs the commit with the generated GPG key", func() {
			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			Expect(err).ToNot(HaveOccurred())

			// configure OpenPGP armor encoder
			b := bytes.NewBuffer(nil)
			w, err := armor.Encode(b, openpgp.PublicKeyType, nil)
			Expect(err).ToNot(HaveOccurred())

			// serialize public key
			err = pgpEntity.Serialize(w)
			Expect(err).ToNot(HaveOccurred())
			err = w.Close()
			Expect(err).ToNot(HaveOccurred())

			// verify commit
			ent, err := commit.Verify(b.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(ent.PrimaryKey.Fingerprint).To(Equal(pgpEntity.PrimaryKey.Fingerprint))
		})
	})

	endToEnd := func(impl, proto string) func() {
		return func() {
			var (
				// for cloning locally
				cloneLocalRepoURL string
				// for the controller
				repoURL       string
				localRepo     *git.Repository
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
				localRepo, err = git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
					URL:           cloneLocalRepoURL,
					RemoteName:    "origin",
					ReferenceName: plumbing.NewBranchReferenceName(branch),
				})
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
					commitInRepo(cloneLocalRepoURL, branch, "Install setter marker", func(tmp string) {
						Expect(replaceMarker(tmp, policyKey)).To(Succeed())
					})
					waitForNewHead(localRepo, branch)

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
										Email: "fluxbot@example.com",
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
					waitForNewHead(localRepo, pushBranch)
					head, err := localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
					Expect(err).NotTo(HaveOccurred())
					commit, err := localRepo.CommitObject(head.Hash())
					Expect(err).ToNot(HaveOccurred())
					Expect(commit.Message).To(Equal(commitMessage))
				})

				It("pushes another commit to the existing push branch", func() {
					// observe the first commit
					waitForNewHead(localRepo, pushBranch)
					head, err := localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
					headHash := head.String()
					Expect(err).NotTo(HaveOccurred())

					// update the policy and expect another commit in the push branch
					policy.Status.LatestImage = "helloworld:v1.3.0"
					Expect(k8sClient.Status().Update(context.TODO(), policy)).To(Succeed())
					waitForNewHead(localRepo, pushBranch)
					head, err = localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
					Expect(err).NotTo(HaveOccurred())
					Expect(head.String()).NotTo(Equal(headHash))
				})

				It("still pushes to the push branch after it's merged", func() {
					// observe the first commit
					waitForNewHead(localRepo, pushBranch)
					head, err := localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
					headHash := head.String()
					Expect(err).NotTo(HaveOccurred())

					// merge the push branch into checkout branch, and push the merge commit
					// upstream.
					// waitForNewHead() leaves the repo at the head of the branch given, i.e., the
					// push branch), so we have to check out the "main" branch first.
					Expect(checkoutBranch(localRepo, branch)).To(Succeed())
					mergeBranchIntoHead(localRepo, pushBranch)

					// update the policy and expect another commit in the push branch
					policy.Status.LatestImage = "helloworld:v1.3.0"
					Expect(k8sClient.Status().Update(context.TODO(), policy)).To(Succeed())
					waitForNewHead(localRepo, pushBranch)
					head, err = localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
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
					// Insert a setter reference into the deployment file,
					// before creating the automation object itself.
					commitInRepo(cloneLocalRepoURL, branch, "Install setter marker", func(tmp string) {
						Expect(replaceMarker(tmp, policyKey)).To(Succeed())
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
										Email: "fluxbot@example.com",
									},
									MessageTemplate: commitMessage,
								},
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
								Email: "fluxbot@example.com",
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

// waitForHead fetches the remote branch given until it differs from
// the remote ref locally (or if there's no ref locally, until it has
// fetched the remote branch). It resets the working tree head to the
// remote branch ref.
func waitForNewHead(repo *git.Repository, branch string) {
	working, err := repo.Worktree()
	Expect(err).ToNot(HaveOccurred())

	// Try to find the remote branch in the repo locally; this will
	// fail if we're on a branch that didn't exist when we cloned the
	// repo (e.g., if the automation is pushing to another branch).
	remoteHeadHash := ""
	remoteBranch := plumbing.NewRemoteReferenceName(originRemote, branch)
	remoteHead, err := repo.Reference(remoteBranch, false)
	if err != plumbing.ErrReferenceNotFound {
		Expect(err).ToNot(HaveOccurred())
	}
	if err == nil {
		remoteHeadHash = remoteHead.Hash().String()
	} // otherwise, any reference fetched will do.

	// Now try to fetch new commits from that remote branch
	Eventually(func() bool {
		if err := repo.Fetch(&git.FetchOptions{
			RefSpecs: []config.RefSpec{
				config.RefSpec("refs/heads/" + branch + ":refs/remotes/origin/" + branch),
			},
		}); err != nil {
			return false
		}
		remoteHead, err = repo.Reference(remoteBranch, false)
		if err != nil {
			return false
		}
		return remoteHead.Hash().String() != remoteHeadHash
	}, timeout, time.Second).Should(BeTrue())

	// New commits in the remote branch -- reset the working tree head
	// to that. Note this does not create a local branch tracking the
	// remote, so it is a detached head.
	Expect(working.Reset(&git.ResetOptions{
		Commit: remoteHead.Hash(),
		Mode:   git.HardReset,
	})).To(Succeed())
}

func compareRepoWithExpected(repoURL, branch, fixture string, changeFixture func(tmp string)) {
	expected, err := os.MkdirTemp("", "gotest-imageauto-expected")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(expected)
	copy.Copy(fixture, expected)
	changeFixture(expected)

	tmp, err := os.MkdirTemp("", "gotest-imageauto")
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
	tmp, err := os.MkdirTemp("", "gotest-imageauto")
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

	err = populateRepoFromFixture(repo, fixture)
	if err != nil {
		return err
	}

	working, err := repo.Worktree()
	if err != nil {
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
		URLs: []string{gitServer.HTTPAddressWithCredentials() + repositoryPath},
	})
	if err != nil {
		return err
	}

	return remote.Push(&git.PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)),
		},
	})
}

func checkoutBranch(repo *git.Repository, branch string) error {
	working, err := repo.Worktree()
	if err != nil {
		return err
	}
	// check that there's no local changes, as a sanity check
	status, err := working.Status()
	if err != nil {
		return err
	}
	if len(status) > 0 {
		for path := range status {
			println(path, "is changed")
		}
	} // the checkout next will fail if there are changed files

	if err = working.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
		Create: false,
	}); err != nil {
		return err
	}
	return nil
}

// This merges the push branch into HEAD, and pushes upstream. This is
// to simulate e.g., a PR being merged.
func mergeBranchIntoHead(repo *git.Repository, pushBranch string) {
	// hash of head
	headRef, err := repo.Head()
	Expect(err).NotTo(HaveOccurred())
	pushBranchRef, err := repo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), false)
	Expect(err).NotTo(HaveOccurred())

	// You need the worktree to be able to create a commit
	worktree, err := repo.Worktree()
	Expect(err).NotTo(HaveOccurred())
	_, err = worktree.Commit(fmt.Sprintf("Merge %s", pushBranch), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Testbot",
			Email: "test@example.com",
			When:  time.Now(),
		},
		Parents: []plumbing.Hash{headRef.Hash(), pushBranchRef.Hash()},
	})
	Expect(err).NotTo(HaveOccurred())

	// push upstream
	err = repo.Push(&git.PushOptions{
		RemoteName: originRemote,
	})
	Expect(err).NotTo(HaveOccurred())
}
