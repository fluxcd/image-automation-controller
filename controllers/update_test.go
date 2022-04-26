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
	"testing"
	"time"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/go-logr/logr"
	git2go "github.com/libgit2/git2go/v33"
	libgit2 "github.com/libgit2/git2go/v33"
	. "github.com/onsi/gomega"
	"github.com/otiai10/copy"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

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

const (
	timeout            = 10 * time.Second
	testAuthorName     = "Flux B Ot"
	testAuthorEmail    = "fluxbot@example.com"
	testCommitTemplate = `Commit summary

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
	testCommitMessageFmt = `Commit summary

Automation: %s/update-test

Files:
- deploy.yaml
Objects:
- deployment test
Images:
- helloworld:v1.0.0 (%s)
`
)

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

func TestImageAutomationReconciler_commitMessage(t *testing.T) {
	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: "not-expected-to-exist",
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: "1.x",
			},
		},
	}
	fixture := "testdata/appconfig"
	latest := "helloworld:v1.0.0"

	testWithRepoAndImagePolicy(
		NewWithT(t), testEnv, fixture, policySpec, latest,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *libgit2.Repository) {
			commitMessage := fmt.Sprintf(testCommitMessageFmt, s.namespace, s.imagePolicyName)

			// Update the setter marker in the repo.
			policyKey := types.NamespacedName{
				Name:      s.imagePolicyName,
				Namespace: s.namespace,
			}
			commitInRepo(g, repoURL, s.branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// Pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId := commitIdFromBranch(localRepo, s.branch)

			// Pull the head commit that was just pushed, so it's not considered a new
			// commit when checking for a commit made by automation.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			preChangeCommitId = commitIdFromBranch(localRepo, s.branch)

			// Create the automation object and let it make a commit itself.
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			}
			err := createImageUpdateAutomation(testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", testCommitTemplate, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			head, _ := headCommit(localRepo)
			commit, err := localRepo.LookupCommit(head.Id())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Message()).To(Equal(commitMessage))

			signature := commit.Author()
			g.Expect(signature).NotTo(BeNil())
			g.Expect(signature.Name).To(Equal(testAuthorName))
			g.Expect(signature.Email).To(Equal(testAuthorEmail))
		},
	)
}

func TestImageAutomationReconciler_crossNamespaceRef(t *testing.T) {
	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: "not-expected-to-exist",
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: "1.x",
			},
		},
	}
	fixture := "testdata/appconfig"
	latest := "helloworld:v1.0.0"

	// Test successful cross namespace reference when NoCrossNamespaceRef=false.
	args := newRepoAndPolicyArgs()
	args.gitRepoNamespace = "cross-ns-git-repo" + randStringRunes(5)
	testWithCustomRepoAndImagePolicy(
		NewWithT(t), testEnv, fixture, policySpec, latest, args,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *libgit2.Repository) {
			commitMessage := fmt.Sprintf(testCommitMessageFmt, s.namespace, s.imagePolicyName)

			// Update the setter marker in the repo.
			policyKey := types.NamespacedName{
				Name:      s.imagePolicyName,
				Namespace: s.namespace,
			}
			commitInRepo(g, repoURL, s.branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// Pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId := commitIdFromBranch(localRepo, s.branch)

			// Pull the head commit that was just pushed, so it's not considered a new
			// commit when checking for a commit made by automation.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			preChangeCommitId = commitIdFromBranch(localRepo, s.branch)

			// Create the automation object and let it make a commit itself.
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			}
			err := createImageUpdateAutomation(testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", testCommitTemplate, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			head, _ := headCommit(localRepo)
			commit, err := localRepo.LookupCommit(head.Id())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Message()).To(Equal(commitMessage))

			signature := commit.Author()
			g.Expect(signature).NotTo(BeNil())
			g.Expect(signature.Name).To(Equal(testAuthorName))
			g.Expect(signature.Email).To(Equal(testAuthorEmail))
		},
	)

	// Test cross namespace reference failure when NoCrossNamespaceRef=true.
	builder := fakeclient.NewClientBuilder().WithScheme(testEnv.Scheme())
	r := &ImageUpdateAutomationReconciler{
		Client:              builder.Build(),
		Scheme:              scheme.Scheme,
		EventRecorder:       testEnv.GetEventRecorderFor("image-automation-controller"),
		NoCrossNamespaceRef: true,
	}
	args = newRepoAndPolicyArgs()
	args.gitRepoNamespace = "cross-ns-git-repo" + randStringRunes(5)
	testWithCustomRepoAndImagePolicy(
		NewWithT(t), r.Client, fixture, policySpec, latest, args,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *libgit2.Repository) {
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			}
			err := createImageUpdateAutomation(r.Client, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", testCommitTemplate, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())

			imageUpdateKey := types.NamespacedName{
				Name:      "update-test",
				Namespace: s.namespace,
			}
			_, err = r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: imageUpdateKey})
			g.Expect(err).To(BeNil())

			var imageUpdate imagev1.ImageUpdateAutomation
			_ = r.Client.Get(context.TODO(), imageUpdateKey, &imageUpdate)
			ready := apimeta.FindStatusCondition(imageUpdate.Status.Conditions, meta.ReadyCondition)
			g.Expect(ready.Reason).To(Equal(acl.AccessDeniedReason))
		},
	)
}

func TestImageAutomationReconciler_updatePath(t *testing.T) {
	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: "not-expected-to-exist",
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: "1.x",
			},
		},
	}
	fixture := "testdata/pathconfig"
	latest := "helloworld:v1.0.0"
	testWithRepoAndImagePolicy(
		NewWithT(t), testEnv, fixture, policySpec, latest,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *libgit2.Repository) {
			// Update the setter marker in the repo.
			policyKey := types.NamespacedName{
				Name:      s.imagePolicyName,
				Namespace: s.namespace,
			}

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId := commitIdFromBranch(localRepo, s.branch)

			commitInRepo(g, repoURL, s.branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(path.Join(tmp, "yes"), policyKey)).To(Succeed())
			})
			commitInRepo(g, repoURL, s.branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(path.Join(tmp, "no"), policyKey)).To(Succeed())
			})

			// Pull the head commit that was just pushed, so it's not considered a new
			// commit when checking for a commit made by automation.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			preChangeCommitId = commitIdFromBranch(localRepo, s.branch)

			// Create the automation object and let it make a commit itself.
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
				Path:     "./yes",
			}
			err := createImageUpdateAutomation(testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", testCommitTemplate, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			head, _ := headCommit(localRepo)
			commit, err := localRepo.LookupCommit(head.Id())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Message()).ToNot(ContainSubstring("update-no"))
			g.Expect(commit.Message()).To(ContainSubstring("update-yes"))
		},
	)
}

func TestImageAutomationReconciler_signedCommit(t *testing.T) {
	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: "not-expected-to-exist",
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: "1.x",
			},
		},
	}
	fixture := "testdata/appconfig"
	latest := "helloworld:v1.0.0"

	testWithRepoAndImagePolicy(
		NewWithT(t), testEnv, fixture, policySpec, latest,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *libgit2.Repository) {
			signingKeySecretName := "signing-key-secret-" + randStringRunes(5)
			// Update the setter marker in the repo.
			policyKey := types.NamespacedName{
				Name:      s.imagePolicyName,
				Namespace: s.namespace,
			}
			commitInRepo(g, repoURL, s.branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			preChangeCommitId := commitIdFromBranch(localRepo, s.branch)

			// Pull the head commit that was just pushed, so it's not considered a new
			// commit when checking for a commit made by automation.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			pgpEntity, err := createSigningKeyPair(testEnv, signingKeySecretName, s.namespace)
			g.Expect(err).ToNot(HaveOccurred(), "failed to create signing key pair")

			preChangeCommitId = commitIdFromBranch(localRepo, s.branch)

			// Create the automation object and let it make a commit itself.
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			}
			err = createImageUpdateAutomation(testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", testCommitTemplate, signingKeySecretName, updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			head, _ := headCommit(localRepo)
			g.Expect(err).ToNot(HaveOccurred())
			commit, err := localRepo.LookupCommit(head.Id())
			g.Expect(err).ToNot(HaveOccurred())

			commitSig, commitContent, err := commit.ExtractSignature()
			g.Expect(err).ToNot(HaveOccurred())

			kr := openpgp.EntityList([]*openpgp.Entity{pgpEntity})
			signature := strings.NewReader(commitSig)
			content := strings.NewReader(commitContent)

			_, err = openpgp.CheckArmoredDetachedSignature(kr, content, signature)
			g.Expect(err).ToNot(HaveOccurred())
		},
	)
}

func TestImageAutomationReconciler_e2e(t *testing.T) {
	gitImpls := []string{sourcev1.GoGitImplementation, sourcev1.LibGit2Implementation}
	protos := []string{"http", "ssh"}

	testFunc := func(t *testing.T, proto string, impl string) {
		g := NewWithT(t)

		const latestImage = "helloworld:1.0.1"

		namespace := "image-auto-test-" + randStringRunes(5)
		branch := randStringRunes(8)
		repositoryPath := "/config-" + randStringRunes(6) + ".git"
		gitRepoName := "image-auto-" + randStringRunes(5)
		gitSecretName := "git-secret-" + randStringRunes(5)
		imagePolicyName := "policy-" + randStringRunes(5)
		updateStrategy := &imagev1.UpdateStrategy{
			Strategy: imagev1.UpdateStrategySetters,
		}

		// Create a test namespace.
		nsCleanup, err := createNamespace(testEnv, namespace)
		g.Expect(err).ToNot(HaveOccurred(), "failed to create test namespace")
		defer func() {
			g.Expect(nsCleanup()).To(Succeed())
		}()

		// Create git server.
		gitServer, err := setupGitTestServer()
		g.Expect(err).ToNot(HaveOccurred(), "failed to create test git server")
		defer os.RemoveAll(gitServer.Root())
		defer gitServer.StopHTTP()

		cloneLocalRepoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
		repoURL, err := getRepoURL(gitServer, repositoryPath, proto)
		g.Expect(err).ToNot(HaveOccurred())

		// Start the ssh server if needed.
		if proto == "ssh" {
			// NOTE: Check how this is done in source-controller.
			go func() {
				gitServer.StartSSH()
			}()
			defer func() {
				g.Expect(gitServer.StopSSH()).To(Succeed())
			}()
		}

		commitMessage := "Commit a difference " + randStringRunes(5)

		// Initialize a git repo.
		g.Expect(initGitRepo(gitServer, "testdata/appconfig", branch, repositoryPath)).To(Succeed())

		// Create GitRepository resource for the above repo.
		if proto == "ssh" {
			// SSH requires an identity (private key) and known_hosts file
			// in a secret.
			err = createSSHIdentitySecret(testEnv, gitSecretName, namespace, repoURL)
			g.Expect(err).ToNot(HaveOccurred())
			err = createGitRepository(testEnv, gitRepoName, namespace, impl, repoURL, gitSecretName)
			g.Expect(err).ToNot(HaveOccurred())
		} else {
			err = createGitRepository(testEnv, gitRepoName, namespace, impl, repoURL, "")
			g.Expect(err).ToNot(HaveOccurred())
		}

		// Create an image policy.
		policyKey := types.NamespacedName{
			Name:      imagePolicyName,
			Namespace: namespace,
		}

		// Create ImagePolicy and ImageUpdateAutomation resource for each of the
		// test cases and cleanup at the end.

		t.Run("PushSpec", func(t *testing.T) {
			// Clone the repo locally.
			localRepo, err := clone(cloneLocalRepoURL, "origin", branch)
			g.Expect(err).ToNot(HaveOccurred(), "failed to clone git repo")

			// NB not testing the image reflector controller; this
			// will make a "fully formed" ImagePolicy object.
			err = createImagePolicyWithLatestImage(testEnv, imagePolicyName, namespace, "not-expected-to-exist", "1.x", latestImage)
			g.Expect(err).ToNot(HaveOccurred(), "failed to create ImagePolicy resource")

			defer func() {
				g.Expect(deleteImagePolicy(testEnv, imagePolicyName, namespace)).ToNot(HaveOccurred())
			}()

			imageUpdateAutomationName := "update-" + randStringRunes(5)
			pushBranch := "pr-" + randStringRunes(5)

			t.Run("update with PushSpec", func(t *testing.T) {
				preChangeCommitId := commitIdFromBranch(localRepo, branch)
				commitInRepo(g, cloneLocalRepoURL, branch, "Install setter marker", func(tmp string) {
					g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
				})
				// Pull the head commit we just pushed, so it's not
				// considered a new commit when checking for a commit
				// made by automation.
				waitForNewHead(g, localRepo, branch, preChangeCommitId)

				// Now create the automation object, and let it (one
				// hopes!) make a commit itself.
				err = createImageUpdateAutomation(testEnv, imageUpdateAutomationName, namespace, gitRepoName, namespace, branch, pushBranch, commitMessage, "", updateStrategy)
				g.Expect(err).ToNot(HaveOccurred())

				initialHead, err := headFromBranch(localRepo, branch)
				g.Expect(err).ToNot(HaveOccurred())
				defer initialHead.Free()

				preChangeCommitId = commitIdFromBranch(localRepo, branch)
				// Wait for a new commit to be made by the controller.
				waitForNewHead(g, localRepo, pushBranch, preChangeCommitId)

				head, err := getRemoteHead(localRepo, pushBranch)
				g.Expect(err).NotTo(HaveOccurred())
				commit, err := localRepo.LookupCommit(head)
				g.Expect(err).ToNot(HaveOccurred())
				defer commit.Free()
				g.Expect(commit.Message()).To(Equal(commitMessage))

				// previous commits should still exist in the tree.
				// regression check to ensure previous commits were not squashed.
				oldCommit, err := localRepo.LookupCommit(initialHead.Id())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(oldCommit).ToNot(BeNil())
			})

			t.Run("push branch gets updated", func(t *testing.T) {
				initialHead, err := headFromBranch(localRepo, branch)
				g.Expect(err).ToNot(HaveOccurred())
				defer initialHead.Free()

				// Get the head hash before update.
				head, err := getRemoteHead(localRepo, pushBranch)
				g.Expect(err).NotTo(HaveOccurred())
				headHash := head.String()

				preChangeCommitId := commitIdFromBranch(localRepo, branch)

				// Update the policy and expect another commit in the push
				// branch.
				err = updateImagePolicyWithLatestImage(testEnv, imagePolicyName, namespace, "helloworld:v1.3.0")
				g.Expect(err).ToNot(HaveOccurred())

				waitForNewHead(g, localRepo, pushBranch, preChangeCommitId)

				head, err = getRemoteHead(localRepo, pushBranch)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(head.String()).NotTo(Equal(headHash))

				// previous commits should still exist in the tree.
				// regression check to ensure previous commits were not squashed.
				oldCommit, err := localRepo.LookupCommit(initialHead.Id())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(oldCommit).ToNot(BeNil())
			})

			t.Run("still pushes to the push branch after it's merged", func(t *testing.T) {
				initialHead, err := headFromBranch(localRepo, branch)
				g.Expect(err).ToNot(HaveOccurred())
				defer initialHead.Free()

				// Get the head hash before.
				head, err := getRemoteHead(localRepo, pushBranch)
				g.Expect(err).NotTo(HaveOccurred())
				headHash := head.String()

				// Merge the push branch into checkout branch, and push the merge commit
				// upstream.
				// waitForNewHead() leaves the repo at the head of the branch given, i.e., the
				// push branch), so we have to check out the "main" branch first.
				r, err := rebase(g, localRepo, pushBranch, branch)
				g.Expect(err).ToNot(HaveOccurred())
				err = r.Finish()
				g.Expect(err).ToNot(HaveOccurred())
				defer r.Free()

				preChangeCommitId := commitIdFromBranch(localRepo, branch)

				// Update the policy and expect another commit in the push
				// branch.
				err = updateImagePolicyWithLatestImage(testEnv, imagePolicyName, namespace, "helloworld:v1.3.1")
				g.Expect(err).ToNot(HaveOccurred())

				waitForNewHead(g, localRepo, pushBranch, preChangeCommitId)

				head, err = getRemoteHead(localRepo, pushBranch)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(head.String()).NotTo(Equal(headHash))

				// previous commits should still exist in the tree.
				// regression check to ensure previous commits were not squashed.
				oldCommit, err := localRepo.LookupCommit(initialHead.Id())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(oldCommit).ToNot(BeNil())
			})

			// Cleanup the image update automation used above.
			g.Expect(deleteImageUpdateAutomation(testEnv, imageUpdateAutomationName, namespace)).To(Succeed())
		})

		t.Run("with update strategy setters", func(t *testing.T) {
			// Clone the repo locally.
			// NOTE: A new localRepo is created here instead of reusing the one
			// in the previous case due to a bug in some of the git operations
			// test helper. When switching branches, the localRepo seems to get
			// stuck in one particular branch. As a workaround, create a
			// separate localRepo.
			localRepo, err := clone(cloneLocalRepoURL, "origin", branch)
			g.Expect(err).ToNot(HaveOccurred(), "failed to clone git repo")

			g.Expect(checkoutBranch(localRepo, branch)).ToNot(HaveOccurred())
			err = createImagePolicyWithLatestImage(testEnv, imagePolicyName, namespace, "not-expected-to-exist", "1.x", latestImage)
			g.Expect(err).ToNot(HaveOccurred(), "failed to create ImagePolicy resource")

			defer func() {
				g.Expect(deleteImagePolicy(testEnv, imagePolicyName, namespace)).ToNot(HaveOccurred())
			}()

			preChangeCommitId := commitIdFromBranch(localRepo, branch)
			// Insert a setter reference into the deployment file,
			// before creating the automation object itself.
			commitInRepo(g, cloneLocalRepoURL, branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// Pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			waitForNewHead(g, localRepo, branch, preChangeCommitId)

			preChangeCommitId = commitIdFromBranch(localRepo, branch)

			// Now create the automation object, and let it (one
			// hopes!) make a commit itself.
			updateKey := types.NamespacedName{
				Namespace: namespace,
				Name:      "update-" + randStringRunes(5),
			}
			err = createImageUpdateAutomation(testEnv, updateKey.Name, namespace, gitRepoName, namespace, branch, "", commitMessage, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())
			defer func() {
				g.Expect(deleteImageUpdateAutomation(testEnv, updateKey.Name, namespace)).To(Succeed())
			}()

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, branch, preChangeCommitId)

			// Check if the repo head matches with the ImageUpdateAutomation
			// last push commit status.
			commit, err := headCommit(localRepo)
			g.Expect(err).ToNot(HaveOccurred())
			defer commit.Free()
			g.Expect(commit.Message()).To(Equal(commitMessage))

			var newObj imagev1.ImageUpdateAutomation
			g.Expect(testEnv.Get(context.Background(), updateKey, &newObj)).To(Succeed())
			g.Expect(newObj.Status.LastPushCommit).To(Equal(commit.Id().String()))
			g.Expect(newObj.Status.LastPushTime).ToNot(BeNil())

			compareRepoWithExpected(g, cloneLocalRepoURL, branch, "testdata/appconfig-setters-expected", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})
		})

		t.Run("no reconciliation when object is suspended", func(t *testing.T) {
			err = createImagePolicyWithLatestImage(testEnv, imagePolicyName, namespace, "not-expected-to-exist", "1.x", latestImage)
			g.Expect(err).ToNot(HaveOccurred(), "failed to create ImagePolicy resource")

			defer func() {
				g.Expect(deleteImagePolicy(testEnv, imagePolicyName, namespace)).ToNot(HaveOccurred())
			}()

			// Create the automation object.
			updateKey := types.NamespacedName{
				Namespace: namespace,
				Name:      "update-" + randStringRunes(5),
			}
			err = createImageUpdateAutomation(testEnv, updateKey.Name, namespace, gitRepoName, namespace, branch, "", commitMessage, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())
			defer func() {
				g.Expect(deleteImageUpdateAutomation(testEnv, updateKey.Name, namespace)).To(Succeed())
			}()

			// Wait for the object to be available in the cache before
			// attempting update.
			g.Eventually(func() bool {
				obj := &imagev1.ImageUpdateAutomation{}
				if err := testEnv.Get(context.Background(), updateKey, obj); err != nil {
					return false
				}
				return true
			}, timeout, time.Second).Should(BeTrue())

			// Suspend the automation object.
			var updatePatch imagev1.ImageUpdateAutomation
			g.Expect(testEnv.Get(context.TODO(), updateKey, &updatePatch)).To(Succeed())
			updatePatch.Spec.Suspend = true
			g.Expect(testEnv.Patch(context.Background(), &updatePatch, client.Merge)).To(Succeed())

			// Create a new image automation reconciler and run it
			// explicitly.
			imageAutoReconciler := &ImageUpdateAutomationReconciler{
				Client: testEnv,
				Scheme: scheme.Scheme,
			}

			// Wait for the suspension to reach the cache
			var newUpdate imagev1.ImageUpdateAutomation
			g.Eventually(func() bool {
				if err := imageAutoReconciler.Get(context.Background(), updateKey, &newUpdate); err != nil {
					return false
				}
				return newUpdate.Spec.Suspend
			}, timeout, time.Second).Should(BeTrue())
			// Run the reconciliation explicitly, and make sure it
			// doesn't do anything
			result, err := imageAutoReconciler.Reconcile(logr.NewContext(context.TODO(), ctrl.Log), ctrl.Request{
				NamespacedName: updateKey,
			})
			g.Expect(err).To(BeNil())
			// This ought to fail if suspend is not working, since the item would be requeued;
			// but if not, additional checks lie below.
			g.Expect(result).To(Equal(ctrl.Result{}))

			var checkUpdate imagev1.ImageUpdateAutomation
			g.Expect(testEnv.Get(context.Background(), updateKey, &checkUpdate)).To(Succeed())
			g.Expect(checkUpdate.Status.ObservedGeneration).NotTo(Equal(checkUpdate.ObjectMeta.Generation))
		})
	}

	// Run the protocol based e2e tests against the git implementations.
	for _, gitImpl := range gitImpls {
		for _, proto := range protos {
			t.Run(fmt.Sprintf("%s_%s", gitImpl, proto), func(t *testing.T) {
				testFunc(t, proto, gitImpl)
			})
		}
	}
}

func TestImageAutomationReconciler_defaulting(t *testing.T) {
	g := NewWithT(t)

	branch := randStringRunes(8)
	namespace := &corev1.Namespace{}
	namespace.Name = "image-auto-test-" + randStringRunes(5)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create a test namespace.
	g.Expect(testEnv.Create(ctx, namespace)).To(Succeed())
	defer func() {
		g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed())
	}()

	// Create an instance of ImageUpdateAutomation.
	key := types.NamespacedName{
		Name:      "update-" + randStringRunes(5),
		Namespace: namespace.Name,
	}
	auto := &imagev1.ImageUpdateAutomation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: imagev1.ImageUpdateAutomationSpec{
			SourceRef: imagev1.CrossNamespaceSourceReference{
				Kind: "GitRepository",
				Name: "garbage",
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
						Email: testAuthorEmail,
					},
					MessageTemplate: "nothing",
				},
			},
		},
	}
	g.Expect(testEnv.Create(ctx, auto)).To(Succeed())
	defer func() {
		g.Expect(testEnv.Delete(ctx, auto)).To(Succeed())
	}()

	// Should default .spec.update to {strategy: Setters}.
	var fetchedAuto imagev1.ImageUpdateAutomation
	g.Eventually(func() bool {
		err := testEnv.Get(ctx, key, &fetchedAuto)
		return err == nil
	}, timeout, time.Second).Should(BeTrue())
	g.Expect(fetchedAuto.Spec.Update).
		To(Equal(&imagev1.UpdateStrategy{Strategy: imagev1.UpdateStrategySetters}))
}

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

func compareRepoWithExpected(g *WithT, repoURL, branch, fixture string, changeFixture func(tmp string)) {
	expected, err := os.MkdirTemp("", "gotest-imageauto-expected")
	g.Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(expected)

	copy.Copy(fixture, expected)
	changeFixture(expected)

	repo, err := clone(repoURL, "origin", branch)
	g.Expect(err).ToNot(HaveOccurred())
	// NOTE: The workdir contains a trailing /. Clean it to not confuse the
	// DiffDirectories().
	actual := filepath.Clean(repo.Workdir())
	defer os.RemoveAll(actual)

	g.Expect(err).ToNot(HaveOccurred())
	test.ExpectMatchingDirectories(g, actual, expected)
}

func commitInRepo(g *WithT, repoURL, branch, msg string, changeFiles func(path string)) {
	originRemote := "origin"
	repo, err := clone(repoURL, originRemote, branch)
	g.Expect(err).ToNot(HaveOccurred())

	changeFiles(repo.Workdir())

	sig := &git2go.Signature{
		Name:  "Testbot",
		Email: "test@example.com",
		When:  time.Now(),
	}
	_, err = commitWorkDir(repo, branch, msg, sig)
	g.Expect(err).ToNot(HaveOccurred())

	origin, err := repo.Remotes.Lookup(originRemote)
	if err != nil {
		panic(fmt.Errorf("cannot find origin: %v", err))
	}
	defer origin.Free()

	g.Expect(origin.Push([]string{branchRefName(branch)}, &libgit2.PushOptions{})).To(Succeed())
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

func waitForNewHead(g *WithT, repo *git2go.Repository, branch, preChangeHash string) {
	var commitToResetTo *git2go.Commit

	// Now try to fetch new commits from that remote branch
	g.Eventually(func() bool {
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
		g.Expect(repo.ResetToCommit(commitToResetTo, libgit2.ResetHard,
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
func rebase(g *WithT, repo *git2go.Repository, sourceBranch, targetBranch string) (*git2go.Rebase, error) {
	rebaseOpts, err := git2go.DefaultRebaseOptions()
	g.Expect(err).NotTo(HaveOccurred())

	err = checkoutBranch(repo, sourceBranch)
	g.Expect(err).NotTo(HaveOccurred())

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
		if err != nil {
			return nil, err
		}

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

type repoAndPolicyArgs struct {
	namespace, imagePolicyName, gitRepoName, branch, gitRepoNamespace string
}

// newRepoAndPolicyArgs generates random namespace, git repo, branch and image
// policy names to be used in the test. The gitRepoNamespace is set the same
// as the overall namespace. For different git repo namespace, the caller may
// assign it as per the needs.
func newRepoAndPolicyArgs() repoAndPolicyArgs {
	args := repoAndPolicyArgs{
		namespace:       "image-auto-test-" + randStringRunes(5),
		gitRepoName:     "image-auto-test-" + randStringRunes(5),
		branch:          randStringRunes(8),
		imagePolicyName: "policy-" + randStringRunes(5),
	}
	args.gitRepoNamespace = args.namespace
	return args
}

// testWithRepoAndImagePolicyTestFunc is the test closure function type passed
// to testWithRepoAndImagePolicy.
type testWithRepoAndImagePolicyTestFunc func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *libgit2.Repository)

// testWithRepoAndImagePolicy generates a repoAndPolicyArgs with all the
// resource in the same namespace and runs the given repo and image policy test.
func testWithRepoAndImagePolicy(
	g *WithT,
	kClient client.Client,
	fixture string,
	policySpec imagev1_reflect.ImagePolicySpec,
	latest string,
	testFunc testWithRepoAndImagePolicyTestFunc) {
	// Generate unique repo and policy arguments.
	args := newRepoAndPolicyArgs()
	testWithCustomRepoAndImagePolicy(g, kClient, fixture, policySpec, latest, args, testFunc)
}

// testWithRepoAndImagePolicy sets up a git server, a repository in the git
// server, a GitRepository object for the created git repo, and an ImagePolicy
// with the given policy spec based on a repoAndPolicyArgs. It calls testFunc
// to run the test in the created environment.
func testWithCustomRepoAndImagePolicy(
	g *WithT,
	kClient client.Client,
	fixture string,
	policySpec imagev1_reflect.ImagePolicySpec,
	latest string,
	args repoAndPolicyArgs,
	testFunc testWithRepoAndImagePolicyTestFunc) {
	repositoryPath := "/config-" + randStringRunes(6) + ".git"

	// Create test git server.
	gitServer, err := setupGitTestServer()
	g.Expect(err).ToNot(HaveOccurred(), "failed to create test git server")
	defer os.RemoveAll(gitServer.Root())
	defer gitServer.StopHTTP()

	// Create test namespace.
	nsCleanup, err := createNamespace(kClient, args.namespace)
	g.Expect(err).ToNot(HaveOccurred(), "failed to create test namespace")
	defer func() {
		g.Expect(nsCleanup()).To(Succeed())
	}()

	// Create gitRepoNamespace if it's not the same as the overall test
	// namespace.
	if args.namespace != args.gitRepoNamespace {
		gitNSCleanup, err := createNamespace(kClient, args.gitRepoNamespace)
		g.Expect(err).ToNot(HaveOccurred(), "failed to create test git repo namespace")
		defer func() {
			g.Expect(gitNSCleanup()).To(Succeed())
		}()
	}

	// Create a git repo.
	g.Expect(initGitRepo(gitServer, fixture, args.branch, repositoryPath)).To(Succeed())

	// Clone the repo.
	repoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
	localRepo, err := clone(repoURL, "origin", args.branch)
	g.Expect(err).ToNot(HaveOccurred(), "failed to clone git repo")

	// Create GitRepository resource for the above repo.
	err = createGitRepository(kClient, args.gitRepoName, args.gitRepoNamespace, "", repoURL, "")
	g.Expect(err).ToNot(HaveOccurred(), "failed to create GitRepository resource")

	// Create ImagePolicy with populated latest image in the status.
	err = createImagePolicyWithLatestImageForSpec(kClient, args.imagePolicyName, args.namespace, policySpec, latest)
	g.Expect(err).ToNot(HaveOccurred(), "failed to create ImagePolicy resource")

	testFunc(g, args, repoURL, localRepo)
}

// setupGitTestServer creates and returns a git test server. The caller must
// ensure it's stopped and cleaned up.
func setupGitTestServer() (*gittestserver.GitServer, error) {
	gitServer, err := gittestserver.NewTempGitServer()
	if err != nil {
		return nil, err
	}
	username := randStringRunes(5)
	password := randStringRunes(5)
	// Using authentication makes using the server more fiddly in
	// general, but is required for testing SSH.
	gitServer.Auth(username, password)
	gitServer.AutoCreate()
	if err := gitServer.StartHTTP(); err != nil {
		return nil, err
	}
	gitServer.KeyDir(filepath.Join(gitServer.Root(), "keys"))
	if err := gitServer.ListenSSH(); err != nil {
		return nil, err
	}
	return gitServer, nil
}

// cleanup is used to return closures for cleaning up.
type cleanup func() error

// createNamespace creates a namespace and returns a closure for deleting the
// namespace.
func createNamespace(kClient client.Client, name string) (cleanup, error) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	if err := kClient.Create(context.Background(), namespace); err != nil {
		return nil, err
	}
	cleanup := func() error {
		return kClient.Delete(context.Background(), namespace)
	}
	return cleanup, nil
}

func createGitRepository(kClient client.Client, name, namespace, impl, repoURL, secretRef string) error {
	gitRepo := &sourcev1.GitRepository{
		Spec: sourcev1.GitRepositorySpec{
			URL:      repoURL,
			Interval: metav1.Duration{Duration: time.Minute},
		},
	}
	gitRepo.Name = name
	gitRepo.Namespace = namespace
	if secretRef != "" {
		gitRepo.Spec.SecretRef = &meta.LocalObjectReference{Name: secretRef}
	}
	if impl != "" {
		gitRepo.Spec.GitImplementation = impl
	}
	return kClient.Create(context.Background(), gitRepo)
}

func createImagePolicyWithLatestImage(kClient client.Client, name, namespace, repoRef, semverRange, latest string) error {
	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: repoRef,
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: semverRange,
			},
		},
	}
	return createImagePolicyWithLatestImageForSpec(kClient, name, namespace, policySpec, latest)
}

func createImagePolicyWithLatestImageForSpec(kClient client.Client, name, namespace string, policySpec imagev1_reflect.ImagePolicySpec, latest string) error {
	policy := &imagev1_reflect.ImagePolicy{
		Spec: policySpec,
	}
	policy.Name = name
	policy.Namespace = namespace
	err := kClient.Create(context.Background(), policy)
	if err != nil {
		return err
	}
	policy.Status.LatestImage = latest
	return kClient.Status().Update(context.Background(), policy)
}

func updateImagePolicyWithLatestImage(kClient client.Client, name, namespace, latest string) error {
	policy := &imagev1_reflect.ImagePolicy{}
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	if err := kClient.Get(context.Background(), key, policy); err != nil {
		return err
	}
	policy.Status.LatestImage = latest
	return kClient.Status().Update(context.Background(), policy)
}

func createImageUpdateAutomation(kClient client.Client, name, namespace,
	gitRepo, gitRepoNamespace, checkoutBranch, pushBranch, commitTemplate, signingKeyRef string,
	updateStrategy *imagev1.UpdateStrategy) error {
	updateAutomation := &imagev1.ImageUpdateAutomation{
		Spec: imagev1.ImageUpdateAutomationSpec{
			Interval: metav1.Duration{Duration: 2 * time.Hour}, // This is to ensure any subsequent run should be outside the scope of the testing.
			SourceRef: imagev1.CrossNamespaceSourceReference{
				Kind:      "GitRepository",
				Name:      gitRepo,
				Namespace: gitRepoNamespace,
			},
			GitSpec: &imagev1.GitSpec{
				Checkout: &imagev1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{
						Branch: checkoutBranch,
					},
				},
				Commit: imagev1.CommitSpec{
					MessageTemplate: commitTemplate,
					Author: imagev1.CommitUser{
						Name:  testAuthorName,
						Email: testAuthorEmail,
					},
				},
			},
			Update: updateStrategy,
		},
	}
	updateAutomation.Name = name
	updateAutomation.Namespace = namespace
	if pushBranch != "" {
		updateAutomation.Spec.GitSpec.Push = &imagev1.PushSpec{
			Branch: pushBranch,
		}
	}
	if signingKeyRef != "" {
		updateAutomation.Spec.GitSpec.Commit.SigningKey = &imagev1.SigningKey{
			SecretRef: meta.LocalObjectReference{Name: signingKeyRef},
		}
	}
	return kClient.Create(context.Background(), updateAutomation)
}

func deleteImageUpdateAutomation(kClient client.Client, name, namespace string) error {
	update := &imagev1.ImageUpdateAutomation{}
	update.Name = name
	update.Namespace = namespace
	return kClient.Delete(context.Background(), update)
}

func deleteImagePolicy(kClient client.Client, name, namespace string) error {
	imagePolicy := &imagev1_reflect.ImagePolicy{}
	imagePolicy.Name = name
	imagePolicy.Namespace = namespace
	return kClient.Delete(context.Background(), imagePolicy)
}

func createSigningKeyPair(kClient client.Client, name, namespace string) (*openpgp.Entity, error) {
	pgpEntity, err := openpgp.NewEntity("", "", "", nil)
	if err != nil {
		return nil, err
	}
	// Configure OpenPGP armor encoder.
	b := bytes.NewBuffer(nil)
	w, err := armor.Encode(b, openpgp.PrivateKeyType, nil)
	if err != nil {
		return nil, err
	}
	// Serialize private key.
	if err := pgpEntity.SerializePrivate(w, nil); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	// Create the secret containing signing key.
	sec := &corev1.Secret{
		Data: map[string][]byte{
			"git.asc": b.Bytes(),
		},
	}
	sec.Name = name
	sec.Namespace = namespace
	if err := kClient.Create(ctx, sec); err != nil {
		return nil, err
	}
	return pgpEntity, nil
}

func createSSHIdentitySecret(kClient client.Client, name, namespace, repoURL string) error {
	url, err := url.Parse(repoURL)
	if err != nil {
		return err
	}
	knownhosts, err := ssh.ScanHostKey(url.Host, 5*time.Second)
	if err != nil {
		return err
	}
	keygen := ssh.NewRSAGenerator(2048)
	pair, err := keygen.Generate()
	if err != nil {
		return err
	}
	sec := &corev1.Secret{
		StringData: map[string]string{
			"known_hosts":  string(knownhosts),
			"identity":     string(pair.PrivateKey),
			"identity.pub": string(pair.PublicKey),
		},
	}
	sec.Name = name
	sec.Namespace = namespace
	return kClient.Create(ctx, sec)
}

func getRepoURL(gitServer *gittestserver.GitServer, repoPath, proto string) (string, error) {
	if proto == "http" {
		return gitServer.HTTPAddressWithCredentials() + repoPath, nil
	} else if proto == "ssh" {
		return getSSHRepoURL(gitServer.SSHAddress(), repoPath), nil
	}
	return "", fmt.Errorf("proto not set to http or ssh")
}

func getSSHRepoURL(sshAddress, repoPath string) string {
	// This is expected to use 127.0.0.1, but host key
	// checking usually wants a hostname, so use
	// "localhost".
	sshURL := strings.Replace(sshAddress, "127.0.0.1", "localhost", 1)
	return sshURL + repoPath
}
