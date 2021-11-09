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
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
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
	. "github.com/onsi/gomega"
	"github.com/otiai10/copy"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
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
- {{ $resource.Kind }} {{ $resource.Name }}
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
- Deployment test
Images:
- helloworld:v1.0.0 (%s)
`
)

func TestImageUpdateAutomation_commit_message(t *testing.T) {
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
		NewWithT(t), fixture, policySpec, latest,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *git.Repository) {
			commitMessage := fmt.Sprintf(testCommitMessageFmt, s.namespace, s.imagePolicyName)

			// Update the setter marker in the repo.
			policyKey := types.NamespacedName{
				Name:      s.imagePolicyName,
				Namespace: s.namespace,
			}
			commitInRepo(g, repoURL, s.branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// Pull the head commit that was just pushed, so it's not considered a new
			// commit when checking for a commit made by automation.
			waitForNewHead(g, localRepo, s.branch)

			// Create the automation object and let it make a commit itself.
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			}
			err := createImageUpdateAutomation("update-test", s.namespace, s.gitRepoName, s.branch, "", testCommitTemplate, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, s.branch)

			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Author).NotTo(BeNil())
			g.Expect(commit.Author.Name).To(Equal(testAuthorName))
			g.Expect(commit.Author.Email).To(Equal(testAuthorEmail))
			g.Expect(commit.Message).To(Equal(commitMessage))
		})
}

func TestImageUpdateAutomation_update_path(t *testing.T) {
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
		NewWithT(t), fixture, policySpec, latest,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *git.Repository) {
			// Update the setter marker in the repo.
			policyKey := types.NamespacedName{
				Name:      s.imagePolicyName,
				Namespace: s.namespace,
			}
			commitInRepo(g, repoURL, s.branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(path.Join(tmp, "yes"), policyKey)).To(Succeed())
			})
			commitInRepo(g, repoURL, s.branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(path.Join(tmp, "no"), policyKey)).To(Succeed())
			})

			// Pull the head commit that was just pushed, so it's not considered a new
			// commit when checking for a commit made by automation.
			waitForNewHead(g, localRepo, s.branch)

			// Create the automation object and let it make a commit itself.
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
				Path:     "./yes",
			}
			err := createImageUpdateAutomation("update-test", s.namespace, s.gitRepoName, s.branch, "", testCommitTemplate, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, s.branch)

			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Message).ToNot(ContainSubstring("update-no"))
			g.Expect(commit.Message).To(ContainSubstring("update-yes"))
		})
}

func TestImageUpdateAutomation_signed_commit(t *testing.T) {
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
		NewWithT(t), fixture, policySpec, latest,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *git.Repository) {
			signingKeySecretName := "signing-key-secret-" + randStringRunes(5)
			// Update the setter marker in the repo.
			policyKey := types.NamespacedName{
				Name:      s.imagePolicyName,
				Namespace: s.namespace,
			}
			commitInRepo(g, repoURL, s.branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// Pull the head commit that was just pushed, so it's not considered a new
			// commit when checking for a commit made by automation.
			waitForNewHead(g, localRepo, s.branch)

			pgpEntity, err := createSigningKeyPair(signingKeySecretName, s.namespace)
			g.Expect(err).ToNot(HaveOccurred(), "failed to create signing key pair")

			// Create the automation object and let it make a commit itself.
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			}
			err = createImageUpdateAutomation("update-test", s.namespace, s.gitRepoName, s.branch, "", testCommitTemplate, signingKeySecretName, updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, s.branch)

			head, err := localRepo.Head()
			g.Expect(err).ToNot(HaveOccurred())
			commit, err := localRepo.CommitObject(head.Hash())
			g.Expect(err).ToNot(HaveOccurred())

			// Configure OpenPGP armor encoder.
			b := bytes.NewBuffer(nil)
			w, err := armor.Encode(b, openpgp.PrivateKeyType, nil)
			g.Expect(err).ToNot(HaveOccurred())

			// Serialize public key.
			err = pgpEntity.Serialize(w)
			g.Expect(err).ToNot(HaveOccurred())
			err = w.Close()
			g.Expect(err).ToNot(HaveOccurred())

			// Verify commit.
			ent, err := commit.Verify(b.String())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(ent.PrimaryKey.Fingerprint).To(Equal(pgpEntity.PrimaryKey.Fingerprint))
		})
}

func TestImageUpdateAutomation_e2e(t *testing.T) {
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
		nsCleanup, err := createNamespace(namespace)
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

		// Clone the repo locally.
		localRepo, err := cloneRepo(cloneLocalRepoURL, branch)
		g.Expect(err).ToNot(HaveOccurred(), "failed to clone git repo")

		// Create GitRepository resource for the above repo.
		if proto == "ssh" {
			// SSH requires an identity (private key) and known_hosts file
			// in a secret.
			err = createSSHIdentitySecret(gitSecretName, namespace, repoURL)
			g.Expect(err).ToNot(HaveOccurred())
			err = createGitRepository(gitRepoName, namespace, impl, repoURL, gitSecretName)
			g.Expect(err).ToNot(HaveOccurred())
		} else {
			err = createGitRepository(gitRepoName, namespace, impl, repoURL, "")
			g.Expect(err).ToNot(HaveOccurred())
		}

		// Create an image policy.
		policyKey := types.NamespacedName{
			Name:      imagePolicyName,
			Namespace: namespace,
		}
		// NB not testing the image reflector controller; this
		// will make a "fully formed" ImagePolicy object.
		err = createImagePolicyWithLatestImage(imagePolicyName, namespace, "not-expected-to-exist", "1.x", latestImage)
		g.Expect(err).ToNot(HaveOccurred(), "failed to create ImagePolicy resource")

		// Create ImageUpdateAutomation resource for each of the test cases
		// and cleanup at the end.

		t.Run("PushSpec", func(t *testing.T) {
			imageUpdateAutomationName := "update-" + randStringRunes(5)
			pushBranch := "pr-" + randStringRunes(5)

			t.Run("update with PushSpec", func(t *testing.T) {
				commitInRepo(g, cloneLocalRepoURL, branch, "Install setter marker", func(tmp string) {
					g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
				})
				// Pull the head commit we just pushed, so it's not
				// considered a new commit when checking for a commit
				// made by automation.
				waitForNewHead(g, localRepo, branch)

				// Now create the automation object, and let it (one
				// hopes!) make a commit itself.
				err = createImageUpdateAutomation(imageUpdateAutomationName, namespace, gitRepoName, branch, pushBranch, commitMessage, "", updateStrategy)
				g.Expect(err).ToNot(HaveOccurred())

				// Wait for a new commit to be made by the controller.
				waitForNewHead(g, localRepo, pushBranch)

				head, err := localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
				g.Expect(err).NotTo(HaveOccurred())
				commit, err := localRepo.CommitObject(head.Hash())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(commit.Message).To(Equal(commitMessage))
			})

			t.Run("push branch gets updated", func(t *testing.T) {
				// Get the head hash before update.
				head, err := localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
				headHash := head.String()
				g.Expect(err).NotTo(HaveOccurred())

				// Update the policy and expect another commit in the push
				// branch.
				err = updateImagePolicyWithLatestImage(imagePolicyName, namespace, "helloworld:v1.3.0")
				g.Expect(err).ToNot(HaveOccurred())
				waitForNewHead(g, localRepo, pushBranch)
				head, err = localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(head.String()).NotTo(Equal(headHash))
			})

			t.Run("still pushes to the push branch after it's merged", func(t *testing.T) {
				// Get the head hash before.
				head, err := localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
				headHash := head.String()
				g.Expect(err).NotTo(HaveOccurred())

				// Merge the push branch into checkout branch, and push the merge commit
				// upstream.
				// waitForNewHead() leaves the repo at the head of the branch given, i.e., the
				// push branch), so we have to check out the "main" branch first.
				g.Expect(checkoutBranch(localRepo, branch)).To(Succeed())
				mergeBranchIntoHead(g, localRepo, pushBranch)

				// Update the policy and expect another commit in the push
				// branch.
				err = updateImagePolicyWithLatestImage(imagePolicyName, namespace, "helloworld:v1.3.1")
				g.Expect(err).ToNot(HaveOccurred())

				waitForNewHead(g, localRepo, pushBranch)

				head, err = localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(head.String()).NotTo(Equal(headHash))
			})

			// Cleanup the image update automation used above.
			g.Expect(deleteImageUpdateAutomation(imageUpdateAutomationName, namespace)).To(Succeed())
		})

		t.Run("with update strategy setters", func(t *testing.T) {
			// Insert a setter reference into the deployment file,
			// before creating the automation object itself.
			commitInRepo(g, cloneLocalRepoURL, branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// Pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			waitForNewHead(g, localRepo, branch)

			// Now create the automation object, and let it (one
			// hopes!) make a commit itself.
			updateKey := types.NamespacedName{
				Namespace: namespace,
				Name:      "update-" + randStringRunes(5),
			}
			err = createImageUpdateAutomation(updateKey.Name, namespace, gitRepoName, branch, "", commitMessage, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())
			defer func() {
				g.Expect(deleteImageUpdateAutomation(updateKey.Name, namespace)).To(Succeed())
			}()

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, branch)

			// Check if the repo head matches with the ImageUpdateAutomation
			// last push commit status.
			head, err := localRepo.Head()
			g.Expect(err).ToNot(HaveOccurred())

			var newObj imagev1.ImageUpdateAutomation
			g.Expect(testEnv.Get(context.Background(), updateKey, &newObj)).To(Succeed())
			g.Expect(newObj.Status.LastPushCommit).To(Equal(head.Hash().String()))
			g.Expect(newObj.Status.LastPushTime).ToNot(BeNil())

			compareRepoWithExpected(g, cloneLocalRepoURL, branch, "testdata/appconfig-setters-expected", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})
		})

		t.Run("no reconciliation when object is suspended", func(t *testing.T) {
			// Create the automation object.
			updateKey := types.NamespacedName{
				Namespace: namespace,
				Name:      "update-" + randStringRunes(5),
			}
			err = createImageUpdateAutomation(updateKey.Name, namespace, gitRepoName, branch, "", commitMessage, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())
			defer func() {
				g.Expect(deleteImageUpdateAutomation(updateKey.Name, namespace)).To(Succeed())
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

		t.Run("reconciles with reconcile request annotation", func(t *testing.T) {
			// The automation has run, and is not expected to run
			// again for 2 hours. Make a commit to the git repo
			// which needs to be undone by automation, then add
			// the annotation and make sure it runs again.

			// TODO: Implement adding request annotation.
			// Refer: https://github.com/fluxcd/image-automation-controller/pull/82/commits/4fde199362b42fa37068f2e6c6885cfea474a3d1#diff-1168fadffa18bd096582ae7f8b6db744fd896bd5600ee1d1ac6ac4474af251b9L292-L334
		})
	}

	// Run the protocol based e2e tests against the git implementations.
	for _, gitImpl := range gitImpls {
		for _, proto := range protos {
			t.Run(gitImpl+"_"+proto, func(t *testing.T) {
				testFunc(t, proto, gitImpl)
			})
		}
	}
}

func TestImageUpdateAutomation_defaulting(t *testing.T) {
	g := NewWithT(t)

	branch := randStringRunes(8)
	namespace := &corev1.Namespace{}
	namespace.Name = "image-auto-test-" + randStringRunes(5)

	ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
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
			SourceRef: imagev1.SourceReference{
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
						Email: "fluxbot@example.com",
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

func expectCommittedAndPushed(conditions []metav1.Condition) {
	rc := apimeta.FindStatusCondition(conditions, meta.ReadyCondition)
	Expect(rc).ToNot(BeNil())
	Expect(rc.Message).To(ContainSubstring("committed and pushed"))
}

func replaceMarker(path string, policyKey types.NamespacedName) error {
	// NB this requires knowledge of what's in the git repo, so a little brittle
	deployment := filepath.Join(path, "deploy.yaml")
	filebytes, err := ioutil.ReadFile(deployment)
	if err != nil {
		return err
	}
	newfilebytes := bytes.ReplaceAll(filebytes, []byte("SETTER_SITE"), []byte(setterRef(policyKey)))
	if err = ioutil.WriteFile(deployment, newfilebytes, os.FileMode(0666)); err != nil {
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
func waitForNewHead(g *WithT, repo *git.Repository, branch string) {
	working, err := repo.Worktree()
	g.Expect(err).ToNot(HaveOccurred())

	// Try to find the remote branch in the repo locally; this will
	// fail if we're on a branch that didn't exist when we cloned the
	// repo (e.g., if the automation is pushing to another branch).
	remoteHeadHash := ""
	remoteBranch := plumbing.NewRemoteReferenceName(originRemote, branch)
	remoteHead, err := repo.Reference(remoteBranch, false)
	if err != plumbing.ErrReferenceNotFound {
		g.Expect(err).ToNot(HaveOccurred())
	}
	if err == nil {
		remoteHeadHash = remoteHead.Hash().String()
	} // otherwise, any reference fetched will do.

	// Now try to fetch new commits from that remote branch
	g.Eventually(func() bool {
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
	g.Expect(working.Reset(&git.ResetOptions{
		Commit: remoteHead.Hash(),
		Mode:   git.HardReset,
	})).To(Succeed())
}

func compareRepoWithExpected(g *WithT, repoURL, branch, fixture string, changeFixture func(tmp string)) {
	expected, err := ioutil.TempDir("", "gotest-imageauto-expected")
	g.Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(expected)
	copy.Copy(fixture, expected)
	changeFixture(expected)

	tmp, err := ioutil.TempDir("", "gotest-imageauto")
	g.Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tmp)
	_, err = git.PlainClone(tmp, false, &git.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
	})
	g.Expect(err).ToNot(HaveOccurred())
	test.ExpectMatchingDirectories(g, tmp, expected)
}

func commitInRepo(g *WithT, repoURL, branch, msg string, changeFiles func(path string)) {
	tmp, err := ioutil.TempDir("", "gotest-imageauto")
	g.Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tmp)
	repo, err := git.PlainClone(tmp, false, &git.CloneOptions{
		URL:           repoURL,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
	})
	g.Expect(err).ToNot(HaveOccurred())

	changeFiles(tmp)

	worktree, err := repo.Worktree()
	g.Expect(err).ToNot(HaveOccurred())
	_, err = worktree.Add(".")
	g.Expect(err).ToNot(HaveOccurred())
	_, err = worktree.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Testbot",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(repo.Push(&git.PushOptions{RemoteName: "origin"})).To(Succeed())
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
func mergeBranchIntoHead(g *WithT, repo *git.Repository, pushBranch string) {
	// hash of head
	headRef, err := repo.Head()
	g.Expect(err).NotTo(HaveOccurred())
	pushBranchRef, err := repo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), false)
	g.Expect(err).NotTo(HaveOccurred())

	// You need the worktree to be able to create a commit
	worktree, err := repo.Worktree()
	g.Expect(err).NotTo(HaveOccurred())
	_, err = worktree.Commit(fmt.Sprintf("Merge %s", pushBranch), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Testbot",
			Email: "test@example.com",
			When:  time.Now(),
		},
		Parents: []plumbing.Hash{headRef.Hash(), pushBranchRef.Hash()},
	})
	g.Expect(err).NotTo(HaveOccurred())

	// push upstream
	err = repo.Push(&git.PushOptions{
		RemoteName: originRemote,
	})
	g.Expect(err).NotTo(HaveOccurred())
}

type repoAndPolicyArgs struct {
	namespace, gitRepoName, branch, imagePolicyName string
}

func newRepoAndPolicyArgs() repoAndPolicyArgs {
	return repoAndPolicyArgs{
		namespace:       "image-auto-test-" + randStringRunes(5),
		gitRepoName:     "image-auto-test-" + randStringRunes(5),
		branch:          randStringRunes(8),
		imagePolicyName: "policy-" + randStringRunes(5),
	}
}

// testWithRepoAndImagePolicyTestFunc is the test closure function type passed
// to testWithRepoAndImagePolicy.
type testWithRepoAndImagePolicyTestFunc func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *git.Repository)

// testWithRepoAndImagePolicy sets up a git server, a repository in the git
// server, a GitRepository object for the created git repo, and an ImagePolicy
// with the given policy spec. It calls testFunc to run the test in the created
// environment.
func testWithRepoAndImagePolicy(
	g *WithT,
	fixture string,
	policySpec imagev1_reflect.ImagePolicySpec,
	latest string,
	testFunc testWithRepoAndImagePolicyTestFunc) {
	repositoryPath := "/config-" + randStringRunes(6) + ".git"

	s := newRepoAndPolicyArgs()

	// Create test git server.
	gitServer, err := setupGitTestServer()
	g.Expect(err).ToNot(HaveOccurred(), "failed to create test git server")
	defer os.RemoveAll(gitServer.Root())
	defer gitServer.StopHTTP()

	// Create test namespace.
	nsCleanup, err := createNamespace(s.namespace)
	g.Expect(err).ToNot(HaveOccurred(), "failed to create test namespace")
	defer func() {
		g.Expect(nsCleanup()).To(Succeed())
	}()

	// Create a git repo.
	g.Expect(initGitRepo(gitServer, fixture, s.branch, repositoryPath)).To(Succeed())

	// Clone the repo.
	repoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
	localRepo, err := cloneRepo(repoURL, s.branch)
	g.Expect(err).ToNot(HaveOccurred(), "failed to clone git repo")

	// Create GitRepository resource for the above repo.
	err = createGitRepository(s.gitRepoName, s.namespace, "", repoURL, "")
	g.Expect(err).ToNot(HaveOccurred(), "failed to create GitRepository resource")

	// Create ImagePolicy with populated latest image in the status.
	err = createImagePolicyWithLatestImageForSpec(s.imagePolicyName, s.namespace, policySpec, latest)
	g.Expect(err).ToNot(HaveOccurred(), "failed to create ImagePolicy resource")

	testFunc(g, s, repoURL, localRepo)
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
func createNamespace(name string) (cleanup, error) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	if err := testEnv.Create(context.Background(), namespace); err != nil {
		return nil, err
	}
	cleanup := func() error {
		return testEnv.Delete(context.Background(), namespace)
	}
	return cleanup, nil
}

func cloneRepo(repoURL, branch string) (*git.Repository, error) {
	return git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
		URL:           repoURL,
		RemoteName:    "origin",
		ReferenceName: plumbing.NewBranchReferenceName(branch),
	})
}

func createGitRepository(name, namespace, impl, repoURL, secretRef string) error {
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
	return testEnv.Create(context.Background(), gitRepo)
}

func createImagePolicyWithLatestImage(name, namespace, repoRef, semverRange, latest string) error {
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
	return createImagePolicyWithLatestImageForSpec(name, namespace, policySpec, latest)
}

func createImagePolicyWithLatestImageForSpec(name, namespace string, policySpec imagev1_reflect.ImagePolicySpec, latest string) error {
	policy := &imagev1_reflect.ImagePolicy{
		Spec: policySpec,
	}
	policy.Name = name
	policy.Namespace = namespace
	err := testEnv.Create(context.Background(), policy)
	if err != nil {
		return err
	}
	policy.Status.LatestImage = latest
	return testEnv.Status().Update(context.Background(), policy)
}

func updateImagePolicyWithLatestImage(name, namespace, latest string) error {
	policy := &imagev1_reflect.ImagePolicy{}
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	if err := testEnv.Get(context.Background(), key, policy); err != nil {
		return err
	}
	policy.Status.LatestImage = latest
	return testEnv.Status().Update(context.Background(), policy)
}

func createImageUpdateAutomation(
	name, namespace, gitRepo, checkoutBranch, pushBranch, commitTemplate, signingKeyRef string,
	updateStrategy *imagev1.UpdateStrategy) error {
	updateAutomation := &imagev1.ImageUpdateAutomation{
		Spec: imagev1.ImageUpdateAutomationSpec{
			Interval: metav1.Duration{Duration: 2 * time.Hour}, // This is to ensure any subsequent run should be outside the scope of the testing.
			SourceRef: imagev1.SourceReference{
				Kind: "GitRepository",
				Name: gitRepo,
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
	return testEnv.Create(context.Background(), updateAutomation)
}

func deleteImageUpdateAutomation(name, namespace string) error {
	update := &imagev1.ImageUpdateAutomation{}
	update.Name = name
	update.Namespace = namespace
	return testEnv.Delete(context.Background(), update)
}

func createSigningKeyPair(name, namespace string) (*openpgp.Entity, error) {
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
	if err := testEnv.Create(ctx, sec); err != nil {
		return nil, err
	}
	return pgpEntity, nil
}

func createSSHIdentitySecret(name, namespace, repoURL string) error {
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
	return testEnv.Create(ctx, sec)
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
