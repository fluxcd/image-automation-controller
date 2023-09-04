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

package controller

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

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	securejoin "github.com/cyphar/filepath-securejoin"
	extgogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	"github.com/otiai10/copy"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	"github.com/fluxcd/pkg/apis/acl"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/gogit"
	"github.com/fluxcd/pkg/git/gogit/fs"
	"github.com/fluxcd/pkg/gittestserver"
	"github.com/fluxcd/pkg/ssh"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta1"
	"github.com/fluxcd/image-automation-controller/internal/features"
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

func TestImageUpdateAutomationReconciler_deleteBeforeFinalizer(t *testing.T) {
	g := NewWithT(t)

	namespaceName := "imageupdate-" + randStringRunes(5)
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespaceName},
	}
	g.Expect(k8sClient.Create(ctx, namespace)).ToNot(HaveOccurred())
	t.Cleanup(func() {
		g.Expect(k8sClient.Delete(ctx, namespace)).NotTo(HaveOccurred())
	})

	imageUpdate := &imagev1.ImageUpdateAutomation{}
	imageUpdate.Name = "test-imageupdate"
	imageUpdate.Namespace = namespaceName
	imageUpdate.Spec = imagev1.ImageUpdateAutomationSpec{
		Interval: metav1.Duration{Duration: time.Second},
		SourceRef: imagev1.CrossNamespaceSourceReference{
			Kind: "GitRepository",
			Name: "foo",
		},
	}
	// Add a test finalizer to prevent the object from getting deleted.
	imageUpdate.SetFinalizers([]string{"test-finalizer"})
	g.Expect(k8sClient.Create(ctx, imageUpdate)).NotTo(HaveOccurred())
	// Add deletion timestamp by deleting the object.
	g.Expect(k8sClient.Delete(ctx, imageUpdate)).NotTo(HaveOccurred())

	r := &ImageUpdateAutomationReconciler{
		Client:        k8sClient,
		EventRecorder: record.NewFakeRecorder(32),
	}
	// NOTE: Only a real API server responds with an error in this scenario.
	g.Eventually(func() error {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(imageUpdate)})
		return err
	}, timeout).Should(Succeed())
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

	t.Run(gogit.ClientName, func(t *testing.T) {
		testWithRepoAndImagePolicy(
			NewWithT(t), testEnv, fixture, policySpec, latest, gogit.ClientName,
			func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
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
				err := createImageUpdateAutomation(testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, s.branch, "", testCommitTemplate, "", updateStrategy)
				g.Expect(err).ToNot(HaveOccurred())

				// Wait for a new commit to be made by the controller.
				waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

				head, _ := localRepo.Head()
				commit, err := localRepo.CommitObject(head.Hash())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(commit.Message).To(Equal(commitMessage))

				signature := commit.Author
				g.Expect(signature).NotTo(BeNil())
				g.Expect(signature.Name).To(Equal(testAuthorName))
				g.Expect(signature.Email).To(Equal(testAuthorEmail))

				// Regression check to ensure the status message contains the branch name
				// if checkout branch is the same as push branch.
				imageUpdateKey := types.NamespacedName{
					Namespace: s.namespace,
					Name:      "update-test",
				}
				var imageUpdate imagev1.ImageUpdateAutomation
				_ = testEnv.Get(context.TODO(), imageUpdateKey, &imageUpdate)
				ready := apimeta.FindStatusCondition(imageUpdate.Status.Conditions, meta.ReadyCondition)
				g.Expect(ready.Message).To(Equal(fmt.Sprintf("committed and pushed commit '%s' to branch '%s'", head.Hash().String(), s.branch)))
			},
		)
	})
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
	t.Run(gogit.ClientName, func(t *testing.T) {
		testWithCustomRepoAndImagePolicy(
			NewWithT(t), testEnv, fixture, policySpec, latest, gogit.ClientName, args,
			func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
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
				err := createImageUpdateAutomation(testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", "", testCommitTemplate, "", updateStrategy)
				g.Expect(err).ToNot(HaveOccurred())

				// Wait for a new commit to be made by the controller.
				waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

				head, _ := localRepo.Head()
				commit, err := localRepo.CommitObject(head.Hash())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(commit.Message).To(Equal(commitMessage))

				signature := commit.Author
				g.Expect(signature).NotTo(BeNil())
				g.Expect(signature.Name).To(Equal(testAuthorName))
				g.Expect(signature.Email).To(Equal(testAuthorEmail))
			},
		)

		// Test cross namespace reference failure when NoCrossNamespaceRef=true.
		r := &ImageUpdateAutomationReconciler{
			Client: fakeclient.NewClientBuilder().
				WithScheme(testEnv.Scheme()).
				WithStatusSubresource(&imagev1.ImageUpdateAutomation{}, &imagev1_reflect.ImagePolicy{}).
				Build(),
			EventRecorder:       testEnv.GetEventRecorderFor("image-automation-controller"),
			NoCrossNamespaceRef: true,
		}
		args = newRepoAndPolicyArgs()
		args.gitRepoNamespace = "cross-ns-git-repo" + randStringRunes(5)
		testWithCustomRepoAndImagePolicy(
			NewWithT(t), r.Client, fixture, policySpec, latest, gogit.ClientName, args,
			func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
				updateStrategy := &imagev1.UpdateStrategy{
					Strategy: imagev1.UpdateStrategySetters,
				}
				err := createImageUpdateAutomation(r.Client, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", "", testCommitTemplate, "", updateStrategy)
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
	})
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

	t.Run(gogit.ClientName, func(t *testing.T) {
		testWithRepoAndImagePolicy(
			NewWithT(t), testEnv, fixture, policySpec, latest, gogit.ClientName,
			func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
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
				err := createImageUpdateAutomation(testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", "", testCommitTemplate, "", updateStrategy)
				g.Expect(err).ToNot(HaveOccurred())

				// Wait for a new commit to be made by the controller.
				waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

				head, _ := localRepo.Head()
				commit, err := localRepo.CommitObject(head.Hash())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(commit.Message).ToNot(ContainSubstring("update-no"))
				g.Expect(commit.Message).To(ContainSubstring("update-yes"))
			},
		)
	})
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

	t.Run(gogit.ClientName, func(t *testing.T) {
		testWithRepoAndImagePolicy(
			NewWithT(t), testEnv, fixture, policySpec, latest, gogit.ClientName,
			func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
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
				err = createImageUpdateAutomation(testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", "", testCommitTemplate, signingKeySecretName, updateStrategy)
				g.Expect(err).ToNot(HaveOccurred())

				// Wait for a new commit to be made by the controller.
				waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

				head, _ := localRepo.Head()
				g.Expect(err).ToNot(HaveOccurred())
				commit, err := localRepo.CommitObject(head.Hash())
				g.Expect(err).ToNot(HaveOccurred())

				c2 := *commit
				c2.PGPSignature = ""

				encoded := &plumbing.MemoryObject{}
				err = c2.Encode(encoded)
				g.Expect(err).ToNot(HaveOccurred())
				content, err := encoded.Reader()
				g.Expect(err).ToNot(HaveOccurred())

				kr := openpgp.EntityList([]*openpgp.Entity{pgpEntity})
				signature := strings.NewReader(commit.PGPSignature)

				_, err = openpgp.CheckArmoredDetachedSignature(kr, content, signature, nil)
				g.Expect(err).ToNot(HaveOccurred())
			},
		)
	})
}

func TestImageAutomationReconciler_push_refspec(t *testing.T) {
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

	t.Run(gogit.ClientName, func(t *testing.T) {
		testWithRepoAndImagePolicy(
			NewWithT(t), testEnv, fixture, policySpec, latest, gogit.ClientName,
			func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
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
				preChangeCommitId = commitIdFromBranch(localRepo, s.branch)

				// Create the automation object and let it make a commit itself.
				updateStrategy := &imagev1.UpdateStrategy{
					Strategy: imagev1.UpdateStrategySetters,
				}
				pushBranch := "auto"
				refspec := fmt.Sprintf("refs/heads/%s:refs/heads/smth/else", pushBranch)
				err := createImageUpdateAutomation(testEnv, "push-refspec", s.namespace,
					s.gitRepoName, s.gitRepoNamespace, s.branch, pushBranch, refspec,
					testCommitTemplate, "", updateStrategy)
				g.Expect(err).ToNot(HaveOccurred())

				// Wait for a new commit to be made by the controller to the destination
				// ref specified in refspec (the stuff after the colon) and the push branch.
				pushBranchHash := getRemoteRef(g, repoURL, pushBranch)
				refspecHash := getRemoteRef(g, repoURL, "smth/else")
				g.Expect(pushBranchHash.String()).ToNot(Equal(preChangeCommitId))
				g.Expect(pushBranchHash.String()).To(Equal(refspecHash.String()))

				imageUpdateKey := types.NamespacedName{
					Namespace: s.namespace,
					Name:      "push-refspec",
				}
				var imageUpdate imagev1.ImageUpdateAutomation
				_ = testEnv.Get(context.TODO(), imageUpdateKey, &imageUpdate)
				ready := apimeta.FindStatusCondition(imageUpdate.Status.Conditions, meta.ReadyCondition)
				g.Expect(ready.Message).To(Equal(
					fmt.Sprintf("committed and pushed commit '%s' to branch '%s' and using refspec '%s'",
						pushBranchHash.String(), pushBranch, refspec)))
			},
		)
	})
}

func TestImageAutomationReconciler_e2e(t *testing.T) {
	protos := []string{"http", "ssh"}

	testFunc := func(t *testing.T, proto string, feats map[string]bool) {
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

		controllerName := "image-automation-controller"
		// Create ImagePolicy and ImageUpdateAutomation resource for each of the
		// test cases and cleanup at the end.
		r := &ImageUpdateAutomationReconciler{
			Client: fakeclient.NewClientBuilder().
				WithScheme(testEnv.Scheme()).
				WithStatusSubresource(&imagev1.ImageUpdateAutomation{}, &imagev1_reflect.ImagePolicy{}).
				Build(),
			EventRecorder: testEnv.GetEventRecorderFor(controllerName),
			features:      feats,
		}

		// Create a test namespace.
		nsCleanup, err := createNamespace(r.Client, namespace)
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
			err = createSSHIdentitySecret(r.Client, gitSecretName, namespace, repoURL)
			g.Expect(err).ToNot(HaveOccurred())
			err = createGitRepository(r.Client, gitRepoName, namespace, repoURL, gitSecretName)
			g.Expect(err).ToNot(HaveOccurred())
		} else {
			err = createGitRepository(r.Client, gitRepoName, namespace, repoURL, "")
			g.Expect(err).ToNot(HaveOccurred())
		}

		// Create an image policy.
		policyKey := types.NamespacedName{
			Name:      imagePolicyName,
			Namespace: namespace,
		}

		t.Run("PushSpec", func(t *testing.T) {
			g := NewWithT(t)

			// Clone the repo locally.
			cloneCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			localRepo, err := clone(cloneCtx, cloneLocalRepoURL, branch)
			g.Expect(err).ToNot(HaveOccurred(), "failed to clone git repo")

			// NB not testing the image reflector controller; this
			// will make a "fully formed" ImagePolicy object.
			err = createImagePolicyWithLatestImage(r.Client, imagePolicyName, namespace, "not-expected-to-exist", "1.x", latestImage)
			g.Expect(err).ToNot(HaveOccurred(), "failed to create ImagePolicy resource")

			defer func() {
				g.Expect(deleteImagePolicy(r.Client, imagePolicyName, namespace)).ToNot(HaveOccurred())
			}()

			imageUpdateAutomationName := "update-" + randStringRunes(5)
			pushBranch := "pr-" + randStringRunes(5)

			automationKey := types.NamespacedName{
				Name:      imageUpdateAutomationName,
				Namespace: namespace,
			}

			t.Run("update with PushSpec", func(t *testing.T) {
				g := NewWithT(t)

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
				err = createImageUpdateAutomation(r.Client, imageUpdateAutomationName, namespace, gitRepoName, namespace, branch, pushBranch, "", commitMessage, "", updateStrategy)
				g.Expect(err).ToNot(HaveOccurred())

				_, err = r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: automationKey})
				g.Expect(err).To(BeNil())

				initialHead, err := headFromBranch(localRepo, branch)
				g.Expect(err).ToNot(HaveOccurred())

				preChangeCommitId = commitIdFromBranch(localRepo, branch)
				// Wait for a new commit to be made by the controller.
				waitForNewHead(g, localRepo, pushBranch, preChangeCommitId)

				head, err := getRemoteHead(localRepo, pushBranch)
				g.Expect(err).NotTo(HaveOccurred())
				commit, err := localRepo.CommitObject(head)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(commit.Message).To(Equal(commitMessage))

				// previous commits should still exist in the tree.
				// regression check to ensure previous commits were not squashed.
				oldCommit, err := localRepo.CommitObject(initialHead.Hash)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(oldCommit).ToNot(BeNil())
			})

			t.Run("push branch gets updated", func(t *testing.T) {
				if !feats[features.GitAllBranchReferences] {
					t.Skip("GitAllBranchReferences feature not enabled")
				}

				g := NewWithT(t)

				initialHead, err := headFromBranch(localRepo, branch)
				g.Expect(err).ToNot(HaveOccurred())

				// Get the head hash before update.
				head, err := getRemoteHead(localRepo, pushBranch)
				g.Expect(err).NotTo(HaveOccurred())
				headHash := head.String()

				preChangeCommitId := commitIdFromBranch(localRepo, branch)

				// Update the policy and expect another commit in the push
				// branch.
				err = updateImagePolicyWithLatestImage(r.Client, imagePolicyName, namespace, "helloworld:v1.3.0")
				g.Expect(err).ToNot(HaveOccurred())

				_, err = r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: automationKey})
				g.Expect(err).To(BeNil())

				waitForNewHead(g, localRepo, pushBranch, preChangeCommitId)

				head, err = getRemoteHead(localRepo, pushBranch)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(head.String()).NotTo(Equal(headHash))

				// previous commits should still exist in the tree.
				// regression check to ensure previous commits were not squashed.
				oldCommit, err := localRepo.CommitObject(initialHead.Hash)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(oldCommit).ToNot(BeNil())
			})

			t.Run("still pushes to the push branch after it's merged", func(t *testing.T) {
				if !feats[features.GitAllBranchReferences] {
					t.Skip("GitAllBranchReferences feature not enabled")
				}

				g := NewWithT(t)

				initialHead, err := headFromBranch(localRepo, branch)
				g.Expect(err).ToNot(HaveOccurred())

				// Get the head hash before.
				head, err := getRemoteHead(localRepo, pushBranch)
				g.Expect(err).NotTo(HaveOccurred())
				headHash := head.String()

				// Merge the push branch into checkout branch, and push the merge commit
				// upstream.
				// waitForNewHead() leaves the repo at the head of the branch given, i.e., the
				// push branch), so we have to check out the "main" branch first.
				w, err := localRepo.Worktree()
				g.Expect(err).ToNot(HaveOccurred())
				w.Pull(&extgogit.PullOptions{
					RemoteName:    originRemote,
					ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/remotes/origin/%s", pushBranch)),
				})
				err = localRepo.Push(&extgogit.PushOptions{
					RemoteName: originRemote,
					RefSpecs: []config.RefSpec{
						config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/remotes/origin/%s", branch, pushBranch))},
				})
				g.Expect(err).ToNot(HaveOccurred())

				preChangeCommitId := commitIdFromBranch(localRepo, branch)

				// Update the policy and expect another commit in the push
				// branch.
				err = updateImagePolicyWithLatestImage(r.Client, imagePolicyName, namespace, "helloworld:v1.3.1")
				g.Expect(err).ToNot(HaveOccurred())

				_, err = r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: automationKey})
				g.Expect(err).To(BeNil())

				waitForNewHead(g, localRepo, pushBranch, preChangeCommitId)

				head, err = getRemoteHead(localRepo, pushBranch)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(head.String()).NotTo(Equal(headHash))

				// previous commits should still exist in the tree.
				// regression check to ensure previous commits were not squashed.
				oldCommit, err := localRepo.CommitObject(initialHead.Hash)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(oldCommit).ToNot(BeNil())
			})

			// Cleanup the image update automation used above.
			g.Expect(deleteImageUpdateAutomation(r.Client, imageUpdateAutomationName, namespace)).To(Succeed())
		})

		t.Run("with update strategy setters", func(t *testing.T) {
			g := NewWithT(t)

			// Clone the repo locally.
			// NOTE: A new localRepo is created here instead of reusing the one
			// in the previous case due to a bug in some of the git operations
			// test helper. When switching branches, the localRepo seems to get
			// stuck in one particular branch. As a workaround, create a
			// separate localRepo.
			cloneCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			localRepo, err := clone(cloneCtx, cloneLocalRepoURL, branch)
			g.Expect(err).ToNot(HaveOccurred(), "failed to clone git repo")

			g.Expect(checkoutBranch(localRepo, branch)).ToNot(HaveOccurred())
			err = createImagePolicyWithLatestImage(r.Client, imagePolicyName, namespace, "not-expected-to-exist", "1.x", latestImage)
			g.Expect(err).ToNot(HaveOccurred(), "failed to create ImagePolicy resource")

			defer func() {
				g.Expect(deleteImagePolicy(r.Client, imagePolicyName, namespace)).ToNot(HaveOccurred())
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
			err = createImageUpdateAutomation(r.Client, updateKey.Name, namespace, gitRepoName, namespace, branch, "", "", commitMessage, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())
			defer func() {
				g.Expect(deleteImageUpdateAutomation(r.Client, updateKey.Name, namespace)).To(Succeed())
			}()

			_, err = r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: updateKey})
			g.Expect(err).To(BeNil())

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, branch, preChangeCommitId)

			// Check if the repo head matches with the ImageUpdateAutomation
			// last push commit status.
			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Message).To(Equal(commitMessage))

			var newObj imagev1.ImageUpdateAutomation
			g.Expect(r.Client.Get(context.Background(), updateKey, &newObj)).To(Succeed())
			g.Expect(newObj.Status.LastPushCommit).To(Equal(commit.Hash.String()))
			g.Expect(newObj.Status.LastPushTime).ToNot(BeNil())

			compareRepoWithExpected(g, cloneLocalRepoURL, branch, "testdata/appconfig-setters-expected", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})
		})

		t.Run("no reconciliation when object is suspended", func(t *testing.T) {
			g := NewWithT(t)

			nsCleanup, err := createNamespace(testEnv, namespace)
			g.Expect(err).ToNot(HaveOccurred(), "failed to create test namespace")
			defer func() {
				g.Expect(nsCleanup()).To(Succeed())
			}()

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
			err = createImageUpdateAutomation(testEnv, updateKey.Name, namespace, gitRepoName, namespace, branch, "", "", commitMessage, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())
			defer func() {
				g.Expect(deleteImageUpdateAutomation(testEnv, updateKey.Name, namespace)).To(Succeed())
			}()

			_, err = r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: updateKey})
			g.Expect(err).To(BeNil())

			// Wait for the object to be available in the cache before
			// attempting update.
			g.Eventually(func() bool {
				obj := &imagev1.ImageUpdateAutomation{}
				if err := testEnv.Get(context.Background(), updateKey, obj); err != nil {
					return false
				}
				if len(obj.Finalizers) == 0 {
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

	for _, enabled := range []bool{true, false} {
		feats := features.FeatureGates()
		for k := range feats {
			feats[k] = enabled
		}
		for _, proto := range protos {
			t.Run(fmt.Sprintf("%s/features=%t", proto, enabled), func(t *testing.T) {
				testFunc(t, proto, feats)
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

func TestImageUpdateAutomationReconciler_getProxyOpts(t *testing.T) {
	invalidProxy := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-proxy",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"url": []byte("https://example.com"),
		},
	}
	validProxy := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valid-proxy",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"address":  []byte("https://example.com"),
			"username": []byte("user"),
			"password": []byte("pass"),
		},
	}

	clientBuilder := fakeclient.NewClientBuilder().
		WithScheme(testEnv.GetScheme()).
		WithObjects(invalidProxy, validProxy)

	r := &ImageUpdateAutomationReconciler{
		Client: clientBuilder.Build(),
	}

	tests := []struct {
		name      string
		secret    string
		err       string
		proxyOpts *transport.ProxyOptions
	}{
		{
			name:   "non-existent secret",
			secret: "non-existent",
			err:    "failed to get proxy secret 'default/non-existent': ",
		},
		{
			name:   "invalid proxy secret",
			secret: "invalid-proxy",
			err:    "invalid proxy secret 'default/invalid-proxy': key 'address' is missing",
		},
		{
			name:   "valid proxy secret",
			secret: "valid-proxy",
			proxyOpts: &transport.ProxyOptions{
				URL:      "https://example.com",
				Username: "user",
				Password: "pass",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			opts, err := r.getProxyOpts(context.TODO(), tt.secret, "default")
			if opts != nil {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(opts).To(Equal(tt.proxyOpts))
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.err))
			}
		})
	}
}

func TestImageAutomationReconciler_getGitClientOpts(t *testing.T) {
	tests := []struct {
		name           string
		gitTransport   git.TransportType
		proxyOpts      *transport.ProxyOptions
		diffPushBranch bool
		clientOptsN    int
	}{
		{
			name:         "default client opts",
			gitTransport: git.HTTPS,
			clientOptsN:  1,
		},
		{
			name:         "http transport adds insecure credentials client opt",
			gitTransport: git.HTTP,
			clientOptsN:  2,
		},
		{
			name:         "http transport and providing proxy options adds insecure crednetials and proxy client opt",
			gitTransport: git.HTTP,
			proxyOpts:    &transport.ProxyOptions{},
			clientOptsN:  3,
		},
		{
			name:           "push branch different from checkout branch adds single branch client opt",
			gitTransport:   git.HTTPS,
			diffPushBranch: true,
			clientOptsN:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			r := &ImageUpdateAutomationReconciler{
				features: map[string]bool{
					features.GitAllBranchReferences: true,
				},
			}
			clientOpts := r.getGitClientOpts(tt.gitTransport, tt.proxyOpts, tt.diffPushBranch)
			g.Expect(len(clientOpts)).To(Equal(tt.clientOptsN))
		})
	}
}

func checkoutBranch(repo *extgogit.Repository, branch string) error {
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}

	status, err := wt.Status()
	if err != nil {
		return err
	}

	for _, s := range status {
		fmt.Println(s)
	}

	return wt.Checkout(&extgogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
	})
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
	cloneCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	repo, err := clone(cloneCtx, repoURL, branch)
	g.Expect(err).ToNot(HaveOccurred())

	// NOTE: The workdir contains a trailing /. Clean it to not confuse the
	// DiffDirectories().
	wt, err := repo.Worktree()
	g.Expect(err).ToNot(HaveOccurred())

	defer wt.Filesystem.Remove(".")

	g.Expect(err).ToNot(HaveOccurred())
	test.ExpectMatchingDirectories(g, wt.Filesystem.Root(), expected)
}

func commitInRepo(g *WithT, repoURL, branch, msg string, changeFiles func(path string)) plumbing.Hash {
	cloneCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	repo, err := clone(cloneCtx, repoURL, branch)
	g.Expect(err).ToNot(HaveOccurred())

	wt, err := repo.Worktree()
	g.Expect(err).ToNot(HaveOccurred())

	changeFiles(wt.Filesystem.Root())

	id, err := commitWorkDir(repo, branch, msg)
	g.Expect(err).ToNot(HaveOccurred())

	origin, err := repo.Remote(originRemote)
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(origin.Push(&extgogit.PushOptions{
		RemoteName: originRemote,
		RefSpecs:   []config.RefSpec{config.RefSpec(branchRefName(branch))},
	})).To(Succeed())
	return id
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

	headRef, err := repo.Head()
	if err != nil {
		return err
	}

	ref := plumbing.NewHashReference(
		plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branch)),
		headRef.Hash())

	return repo.Storer.SetReference(ref)
}

func initGitRepoPlain(fixture, repositoryPath string) (*extgogit.Repository, error) {
	wt := fs.New(repositoryPath)
	dot := fs.New(filepath.Join(repositoryPath, extgogit.GitDirName))
	storer := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	repo, err := extgogit.Init(storer, wt)
	if err != nil {
		return nil, err
	}

	err = copyDir(fixture, repositoryPath)
	if err != nil {
		return nil, err
	}

	_, err = commitWorkDir(repo, "main", "Initial commit")
	if err != nil {
		return nil, err
	}

	return repo, nil
}

func headFromBranch(repo *extgogit.Repository, branchName string) (*object.Commit, error) {
	ref, err := repo.Storer.Reference(plumbing.ReferenceName("refs/heads/" + branchName))
	if err != nil {
		return nil, err
	}

	return repo.CommitObject(ref.Hash())
}

func commitWorkDir(repo *extgogit.Repository, branchName, message string) (plumbing.Hash, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return plumbing.ZeroHash, err
	}

	// Checkout to an existing branch. If this is the first commit,
	// this is a no-op.
	_ = wt.Checkout(&extgogit.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + branchName),
	})

	status, err := wt.Status()
	if err != nil {
		return plumbing.ZeroHash, err
	}

	for file := range status {
		wt.Add(file)
	}

	sig := mockSignature(time.Now())
	c, err := wt.Commit(message, &extgogit.CommitOptions{
		All:       true,
		Author:    sig,
		Committer: sig,
	})
	if err != nil {
		return plumbing.ZeroHash, err
	}

	_, err = repo.Branch(branchName)
	if err == extgogit.ErrBranchNotFound {
		ref := plumbing.NewHashReference(
			plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branchName)), c)

		err = repo.Storer.SetReference(ref)
	}
	if err != nil {
		return plumbing.ZeroHash, err
	}

	// Now the target branch exists, we can checkout to it.
	err = wt.Checkout(&extgogit.CheckoutOptions{
		Branch: plumbing.ReferenceName("refs/heads/" + branchName),
	})
	if err != nil {
		return plumbing.ZeroHash, err
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

func commitFile(repo *extgogit.Repository, path, content string, time time.Time) (plumbing.Hash, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return plumbing.ZeroHash, err
	}

	f, err := wt.Filesystem.Create(path)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	if _, err := f.Write([]byte(content)); err != nil {
		return plumbing.ZeroHash, err
	}

	wt.Add(path)
	sig := mockSignature(time)
	c, err := wt.Commit("Committing "+path, &extgogit.CommitOptions{
		Author:    sig,
		Committer: sig,
	})
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return c, nil
}

func mockSignature(time time.Time) *object.Signature {
	return &object.Signature{
		Name:  "Jane Doe",
		Email: "author@example.com",
		When:  time,
	}
}

func clone(ctx context.Context, repoURL, branchName string) (*extgogit.Repository, error) {
	dir, err := os.MkdirTemp("", "iac-clone-*")
	if err != nil {
		return nil, err
	}

	opts := &extgogit.CloneOptions{
		URL:           repoURL,
		RemoteName:    originRemote,
		ReferenceName: plumbing.NewBranchReferenceName(branchName),
	}

	wt := fs.New(dir)
	dot := fs.New(filepath.Join(dir, extgogit.GitDirName))
	storer := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	repo, err := extgogit.Clone(storer, wt, opts)
	if err != nil {
		return nil, err
	}

	w, err := repo.Worktree()
	if err != nil {
		return nil, err
	}

	err = w.Checkout(&extgogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branchName),
		Create: false,
	})
	if err != nil {
		return nil, err
	}

	return repo, nil
}

func getRemoteRef(g *WithT, repoURL, ref string) plumbing.Hash {
	var hash plumbing.Hash
	g.Eventually(func() bool {
		cloneCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		repo, err := clone(cloneCtx, repoURL, ref)
		if err != nil {
			return false
		}

		remRefName := plumbing.NewRemoteReferenceName(extgogit.DefaultRemoteName, ref)
		remRef, err := repo.Reference(remRefName, true)
		if err != nil {
			return false
		}
		hash = remRef.Hash()
		return true
	}, timeout, time.Second).Should(BeTrue())
	return hash
}

func waitForNewHead(g *WithT, repo *extgogit.Repository, branch, preChangeHash string) {
	var commitToResetTo *object.Commit

	origin, err := repo.Remote(originRemote)
	g.Expect(err).ToNot(HaveOccurred())

	// Now try to fetch new commits from that remote branch
	g.Eventually(func() bool {
		err := origin.Fetch(&extgogit.FetchOptions{
			RemoteName: originRemote,
			RefSpecs:   []config.RefSpec{config.RefSpec(branchRefName(branch))},
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
		if err != nil {
			return false
		}

		if preChangeHash != remoteHeadHash.String() {
			commitToResetTo, _ = repo.CommitObject(remoteHeadHash)
			return true
		}
		return false
	}, timeout, time.Second).Should(BeTrue())

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

func headCommit(repo *extgogit.Repository) (*object.Commit, error) {
	head, err := repo.Head()
	if err != nil {
		return nil, err

	}
	c, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err

	}
	return c, nil
}

func commitIdFromBranch(repo *extgogit.Repository, branchName string) string {
	commitId := ""
	head, err := headFromBranch(repo, branchName)

	if err == nil {
		commitId = head.Hash.String()
	}
	return commitId
}

func getRemoteHead(repo *extgogit.Repository, branchName string) (plumbing.Hash, error) {
	remote, err := repo.Remote(originRemote)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	err = remote.Fetch(&extgogit.FetchOptions{
		RemoteName: originRemote,
		RefSpecs:   []config.RefSpec{config.RefSpec(branchRefName(branchName))},
	})
	if err != nil && !errors.Is(err, extgogit.NoErrAlreadyUpToDate) {
		return plumbing.ZeroHash, err
	}

	remoteHeadRef, err := headFromBranch(repo, branchName)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	return remoteHeadRef.Hash, nil
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
type testWithRepoAndImagePolicyTestFunc func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository)

// testWithRepoAndImagePolicy generates a repoAndPolicyArgs with all the
// resource in the same namespace and runs the given repo and image policy test.
func testWithRepoAndImagePolicy(
	g *WithT,
	kClient client.Client,
	fixture string,
	policySpec imagev1_reflect.ImagePolicySpec,
	latest, gitImpl string,
	testFunc testWithRepoAndImagePolicyTestFunc) {
	// Generate unique repo and policy arguments.
	args := newRepoAndPolicyArgs()
	testWithCustomRepoAndImagePolicy(g, kClient, fixture, policySpec, latest, gitImpl, args, testFunc)
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
	latest, gitImpl string,
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
	cloneCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	localRepo, err := clone(cloneCtx, repoURL, args.branch)
	g.Expect(err).ToNot(HaveOccurred(), "failed to clone git repo")

	err = localRepo.DeleteRemote(originRemote)
	g.Expect(err).ToNot(HaveOccurred(), "failed to delete existing remote origin")
	localRepo.CreateRemote(&config.RemoteConfig{
		Name: originRemote,
		URLs: []string{repoURL},
	})
	g.Expect(err).ToNot(HaveOccurred(), "failed to create new remote origin")

	// Create GitRepository resource for the above repo.
	err = createGitRepository(kClient, args.gitRepoName, args.gitRepoNamespace, repoURL, "")
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

func createGitRepository(kClient client.Client, name, namespace, repoURL, secretRef string) error {
	gitRepo := &sourcev1.GitRepository{
		Spec: sourcev1.GitRepositorySpec{
			URL:      repoURL,
			Interval: metav1.Duration{Duration: time.Minute},
			Timeout:  &metav1.Duration{Duration: time.Minute},
		},
	}
	gitRepo.Name = name
	gitRepo.Namespace = namespace
	if secretRef != "" {
		gitRepo.Spec.SecretRef = &meta.LocalObjectReference{Name: secretRef}
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
	patch := client.MergeFrom(policy.DeepCopy())
	policy.Status.LatestImage = latest
	return kClient.Status().Patch(context.Background(), policy, patch)
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
	patch := client.MergeFrom(policy.DeepCopy())
	policy.Status.LatestImage = latest
	return kClient.Status().Patch(context.Background(), policy, patch)
}

func createImageUpdateAutomation(kClient client.Client, name, namespace,
	gitRepo, gitRepoNamespace, checkoutBranch, pushBranch, pushRefspec, commitTemplate, signingKeyRef string,
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
	if pushRefspec != "" || pushBranch != "" {
		updateAutomation.Spec.GitSpec.Push = &imagev1.PushSpec{
			Refspec: pushRefspec,
			Branch:  pushBranch,
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

	passphrase := "abcde12345"
	if err = pgpEntity.PrivateKey.Encrypt([]byte(passphrase)); err != nil {
		return nil, err
	}
	// Create the secret containing signing key.
	sec := &corev1.Secret{
		Data: map[string][]byte{
			signingSecretKey:     b.Bytes(),
			signingPassphraseKey: []byte(passphrase),
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
	knownhosts, err := ssh.ScanHostKey(url.Host, 5*time.Second, []string{}, false)
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
		// Without KAS, StringData and Data must be kept in sync manually.
		Data: map[string][]byte{
			"known_hosts":  knownhosts,
			"identity":     pair.PrivateKey,
			"identity.pub": pair.PublicKey,
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
		// This is expected to use 127.0.0.1, but host key
		// checking usually wants a hostname, so use
		// "localhost".
		sshURL := strings.Replace(gitServer.SSHAddress(), "127.0.0.1", "localhost", 1)
		return sshURL + repoPath, nil
	}
	return "", fmt.Errorf("proto not set to http or ssh")
}
