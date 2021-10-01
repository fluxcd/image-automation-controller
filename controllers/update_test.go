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

func TestImageUpdateAutomation_commit_spec(t *testing.T) {
	g := NewWithT(t)

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
- {{ $resource.Kind }} {{ $resource.Name }}
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
- Deployment test
Images:
- helloworld:v1.0.0 (%s)
`
	)

	tests := []struct {
		name           string
		testdataPath   string
		updateStrategy *imagev1.UpdateStrategy
		sign           bool
	}{
		{
			name:         "with non-path update setter",
			testdataPath: "testdata/appconfig",
			updateStrategy: &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			},
		},
		{
			name:         "with path update setter",
			testdataPath: "testdata/pathconfig",
			updateStrategy: &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
				Path:     "./yes",
			},
		},
		{
			name:         "with signed commit",
			testdataPath: "testdata/appconfig",
			updateStrategy: &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			},
			sign: true,
		},
	}

	// Create test git server.
	gitServer, err := gittestserver.NewTempGitServer()
	g.Expect(err).NotTo(HaveOccurred())
	defer os.RemoveAll(gitServer.Root())

	username := randStringRunes(5)
	password := randStringRunes(5)
	// using authentication makes using the server more fiddly in
	// general, but is required for testing SSH.
	gitServer.Auth(username, password)
	gitServer.AutoCreate()

	g.Expect(gitServer.StartHTTP()).To(Succeed())
	defer gitServer.StopHTTP()

	gitServer.KeyDir(filepath.Join(gitServer.Root(), "keys"))
	g.Expect(gitServer.ListenSSH()).To(Succeed())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			namespace := &corev1.Namespace{}
			namespace.Name = "image-auto-test-" + randStringRunes(5)

			ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
			defer cancel()

			// Create a test namespace.
			g.Expect(testEnv.Create(ctx, namespace)).To(Succeed())
			defer func() {
				g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed())
			}()

			branch := randStringRunes(8)
			repositoryPath := "/config-" + randStringRunes(6) + ".git"

			// Initialize a git repo.
			g.Expect(initGitRepo(gitServer, tt.testdataPath, branch, repositoryPath)).To(Succeed())

			// Clone the repo.
			repoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
			localRepo, err := git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
				URL:           repoURL,
				RemoteName:    "origin",
				ReferenceName: plumbing.NewBranchReferenceName(branch),
			})
			g.Expect(err).ToNot(HaveOccurred())

			// Create GitRepository resource for the above repo.
			gitRepoKey := types.NamespacedName{
				Name:      "image-auto-" + randStringRunes(5),
				Namespace: namespace.Name,
			}
			gitRepo := &sourcev1.GitRepository{
				Spec: sourcev1.GitRepositorySpec{
					URL:      repoURL,
					Interval: metav1.Duration{Duration: time.Minute},
				},
			}
			gitRepo.Name = gitRepoKey.Name
			gitRepo.Namespace = namespace.Name
			g.Expect(testEnv.Create(ctx, gitRepo)).To(Succeed())

			// Create an image policy.
			policyKey := types.NamespacedName{
				Name:      "policy-" + randStringRunes(5),
				Namespace: namespace.Name,
			}
			// NB not testing the image reflector controller; this
			// will make a "fully formed" ImagePolicy object.
			policy := &imagev1_reflect.ImagePolicy{
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
			policy.Name = policyKey.Name
			policy.Namespace = policyKey.Namespace
			g.Expect(testEnv.Create(ctx, policy)).To(Succeed())
			policy.Status.LatestImage = "helloworld:v1.0.0"
			g.Expect(testEnv.Status().Update(ctx, policy)).To(Succeed())

			// Format the expected message given the generated values
			commitMessage := fmt.Sprintf(commitMessageFmt, namespace.Name, policyKey.Name)

			// Insert a setter reference into the deployment file,
			// before creating the automation object itself.
			if tt.updateStrategy.Path == "" {
				commitInRepo(g, repoURL, branch, "Install setter marker", func(tmp string) {
					g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
				})
			} else {
				commitInRepo(g, repoURL, branch, "Install setter marker", func(tmp string) {
					g.Expect(replaceMarker(path.Join(tmp, "yes"), policyKey)).To(Succeed())
				})
				commitInRepo(g, repoURL, branch, "Install setter marker", func(tmp string) {
					g.Expect(replaceMarker(path.Join(tmp, "no"), policyKey)).To(Succeed())
				})
			}

			// Pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			waitForNewHead(g, localRepo, branch)

			var pgpEntity *openpgp.Entity
			var sec *corev1.Secret
			if tt.sign {
				var err error
				// generate keypair for signing
				pgpEntity, err = openpgp.NewEntity("", "", "", nil)
				g.Expect(err).ToNot(HaveOccurred())

				// configure OpenPGP armor encoder
				b := bytes.NewBuffer(nil)
				w, err := armor.Encode(b, openpgp.PrivateKeyType, nil)
				g.Expect(err).ToNot(HaveOccurred())

				// serialize private key
				err = pgpEntity.SerializePrivate(w, nil)
				g.Expect(err).ToNot(HaveOccurred())
				err = w.Close()
				g.Expect(err).ToNot(HaveOccurred())

				// create the secret containing signing key
				sec = &corev1.Secret{
					Data: map[string][]byte{
						"git.asc": b.Bytes(),
					},
				}
				sec.Name = "signing-key-secret-" + randStringRunes(5)
				sec.Namespace = namespace.Name
				g.Expect(testEnv.Create(ctx, sec)).To(Succeed())
			}

			// Now create the automation object, and let it (one
			// hopes!) make a commit itself.
			updateKey := types.NamespacedName{
				Namespace: namespace.Name,
				Name:      "update-test",
			}
			updateBySetters := &imagev1.ImageUpdateAutomation{
				Spec: imagev1.ImageUpdateAutomationSpec{
					Interval: metav1.Duration{Duration: 2 * time.Hour}, // this is to ensure any subsequent run should be outside the scope of the testing
					SourceRef: imagev1.SourceReference{
						Kind: "GitRepository",
						Name: gitRepoKey.Name,
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
					Update: tt.updateStrategy,
				},
			}
			updateBySetters.Name = updateKey.Name
			updateBySetters.Namespace = updateKey.Namespace

			// Add commit signing info.
			if tt.sign {
				updateBySetters.Spec.GitSpec.Commit.SigningKey = &imagev1.SigningKey{
					SecretRef: meta.LocalObjectReference{Name: sec.Name},
				}
			}

			// Create the image update automation object.
			g.Expect(testEnv.Create(ctx, updateBySetters)).To(Succeed())
			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, branch)

			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Author).NotTo(BeNil())
			g.Expect(commit.Author.Name).To(Equal(authorName))
			g.Expect(commit.Author.Email).To(Equal(authorEmail))

			if tt.updateStrategy.Path == "" {
				g.Expect(commit.Message).To(Equal(commitMessage))
			} else {
				g.Expect(commit.Message).To(Not(ContainSubstring("update-no")))
				g.Expect(commit.Message).To(ContainSubstring("update-yes"))
			}

			if tt.sign {
				// configure OpenPGP armor encoder
				b := bytes.NewBuffer(nil)
				w, err := armor.Encode(b, openpgp.PublicKeyType, nil)
				g.Expect(err).ToNot(HaveOccurred())

				// serialize public key
				err = pgpEntity.Serialize(w)
				g.Expect(err).ToNot(HaveOccurred())
				err = w.Close()
				g.Expect(err).ToNot(HaveOccurred())

				// verify commit
				ent, err := commit.Verify(b.String())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(ent.PrimaryKey.Fingerprint).To(Equal(pgpEntity.PrimaryKey.Fingerprint))
			}
		})
	}
}

func TestImageUpdateAutomation_e2e(t *testing.T) {
	tests := []struct {
		name  string
		proto string
		impl  string
	}{
		{
			name:  "go-git with HTTP",
			proto: "http",
			impl:  sourcev1.GoGitImplementation,
		},
		{
			name:  "go-git with SSH",
			proto: "ssh",
			impl:  sourcev1.GoGitImplementation,
		},
		{
			name:  "libgit2 with HTTP",
			proto: "http",
			impl:  sourcev1.LibGit2Implementation,
		},
		{
			name:  "libgit2 with SSH",
			proto: "ssh",
			impl:  sourcev1.LibGit2Implementation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			const latestImage = "helloworld:1.0.1"

			ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
			defer cancel()

			// Create a test namespace.
			namespace := &corev1.Namespace{}
			namespace.Name = "image-auto-test-" + randStringRunes(5)
			g.Expect(testEnv.Create(ctx, namespace)).To(Succeed())
			defer func() {
				g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed())
			}()

			branch := randStringRunes(8)
			repositoryPath := "/config-" + randStringRunes(6) + ".git"

			// Create git server.
			gitServer, err := gittestserver.NewTempGitServer()
			g.Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(gitServer.Root())

			username := randStringRunes(5)
			password := randStringRunes(5)
			// Using authentication makes using the server more fiddly in
			// general, but is required for testing SSH.
			gitServer.Auth(username, password)
			gitServer.AutoCreate()

			g.Expect(gitServer.StartHTTP()).To(Succeed())
			defer gitServer.StopHTTP()

			gitServer.KeyDir(filepath.Join(gitServer.Root(), "keys"))
			g.Expect(gitServer.ListenSSH()).To(Succeed())

			var repoURL string
			cloneLocalRepoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath

			if tt.proto == "http" {
				repoURL = cloneLocalRepoURL // NB not testing auth for git over HTTP
			} else if tt.proto == "ssh" {
				sshURL := gitServer.SSHAddress()
				// this is expected to use 127.0.0.1, but host key
				// checking usually wants a hostname, so use
				// "localhost".
				sshURL = strings.Replace(sshURL, "127.0.0.1", "localhost", 1)
				repoURL = sshURL + repositoryPath

				// NOTE: Check how this is done in source-controller.
				go func() {
					gitServer.StartSSH()
				}()
				defer func() {
					g.Expect(gitServer.StopSSH()).To(Succeed())
				}()
			} else {
				t.Fatal("proto not set to http or ssh")
			}

			commitMessage := "Commit a difference " + randStringRunes(5)

			// Initialize a git repo.
			g.Expect(initGitRepo(gitServer, "testdata/appconfig", branch, repositoryPath)).To(Succeed())

			localRepo, err := git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
				URL:           cloneLocalRepoURL,
				RemoteName:    "origin",
				ReferenceName: plumbing.NewBranchReferenceName(branch),
			})
			g.Expect(err).ToNot(HaveOccurred())

			// Create git repo resource for the above repo.
			gitRepoKey := types.NamespacedName{
				Name:      "image-auto-" + randStringRunes(5),
				Namespace: namespace.Name,
			}
			gitRepo := &sourcev1.GitRepository{
				Spec: sourcev1.GitRepositorySpec{
					URL:               repoURL,
					Interval:          metav1.Duration{Duration: time.Minute},
					GitImplementation: tt.impl,
				},
			}
			gitRepo.Name = gitRepoKey.Name
			gitRepo.Namespace = namespace.Name

			// If using SSH, we need to provide an identity (private
			// key) and known_hosts file in a secret.
			if tt.proto == "ssh" {
				url, err := url.Parse(repoURL)
				g.Expect(err).ToNot(HaveOccurred())
				knownhosts, err := ssh.ScanHostKey(url.Host, 5*time.Second)
				g.Expect(err).ToNot(HaveOccurred())
				keygen := ssh.NewRSAGenerator(2048)
				pair, err := keygen.Generate()
				g.Expect(err).ToNot(HaveOccurred())

				sec := &corev1.Secret{
					StringData: map[string]string{
						"known_hosts":  string(knownhosts),
						"identity":     string(pair.PrivateKey),
						"identity.pub": string(pair.PublicKey),
					},
				}
				sec.Name = "git-secret-" + randStringRunes(5)
				sec.Namespace = namespace.Name
				g.Expect(testEnv.Create(ctx, sec)).To(Succeed())
				gitRepo.Spec.SecretRef = &meta.LocalObjectReference{Name: sec.Name}
			}

			g.Expect(testEnv.Create(ctx, gitRepo)).To(Succeed())

			// Create an image policy.
			policyKey := types.NamespacedName{
				Name:      "policy-" + randStringRunes(5),
				Namespace: namespace.Name,
			}
			// NB not testing the image reflector controller; this
			// will make a "fully formed" ImagePolicy object.
			policy := &imagev1_reflect.ImagePolicy{
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
			policy.Name = policyKey.Name
			policy.Namespace = policyKey.Namespace
			g.Expect(testEnv.Create(ctx, policy)).To(Succeed())
			policy.Status.LatestImage = latestImage
			g.Expect(testEnv.Status().Update(ctx, policy)).To(Succeed())
			defer func() {
				g.Expect(testEnv.Delete(ctx, policy)).To(Succeed())
			}()

			// --- update with PushSpec

			commitInRepo(g, cloneLocalRepoURL, branch, "Install setter marker", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// Pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			waitForNewHead(g, localRepo, branch)

			pushBranch := "pr-" + randStringRunes(5)

			// Now create the automation object, and let it (one
			// hopes!) make a commit itself.
			updateKey := types.NamespacedName{
				Namespace: namespace.Name,
				Name:      "update-" + randStringRunes(5),
			}
			updateBySetters := &imagev1.ImageUpdateAutomation{
				Spec: imagev1.ImageUpdateAutomationSpec{
					Interval: metav1.Duration{Duration: 2 * time.Hour}, // this is to ensure any subsequent run should be outside the scope of the testing
					SourceRef: imagev1.SourceReference{
						Kind: "GitRepository",
						Name: gitRepoKey.Name,
					},
					GitSpec: &imagev1.GitSpec{
						Checkout: &imagev1.GitCheckoutSpec{
							Reference: sourcev1.GitRepositoryRef{
								Branch: branch,
							},
						},
						Commit: imagev1.CommitSpec{
							MessageTemplate: commitMessage,
							Author: imagev1.CommitUser{
								Email: "fluxbot@example.com",
							},
						},
						Push: &imagev1.PushSpec{
							Branch: pushBranch,
						},
					},
					Update: &imagev1.UpdateStrategy{
						Strategy: imagev1.UpdateStrategySetters,
					},
				},
			}
			updateBySetters.Name = updateKey.Name
			updateBySetters.Namespace = updateKey.Namespace
			g.Expect(testEnv.Create(ctx, updateBySetters)).To(Succeed())

			// -- Creates and pushes the push branch.

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, pushBranch)
			head, err := localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
			g.Expect(err).NotTo(HaveOccurred())
			commit, err := localRepo.CommitObject(head.Hash())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Message).To(Equal(commitMessage))

			// -- Pushes another commit to the existing push branch.

			head, err = localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
			headHash := head.String()
			g.Expect(err).NotTo(HaveOccurred())

			// Update the policy and expect another commit in the push branch.
			policy.Status.LatestImage = "helloworld:v1.3.0"
			g.Expect(testEnv.Status().Update(ctx, policy)).To(Succeed())
			waitForNewHead(g, localRepo, pushBranch)
			head, err = localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(head.String()).NotTo(Equal(headHash))

			// -- Still pushes to the push branch after it's merged.

			head, err = localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
			headHash = head.String()
			g.Expect(err).NotTo(HaveOccurred())

			// Merge the push branch into checkout branch, and push the merge commit
			// upstream.
			// waitForNewHead() leaves the repo at the head of the branch given, i.e., the
			// push branch), so we have to check out the "main" branch first.
			g.Expect(checkoutBranch(localRepo, branch)).To(Succeed())
			mergeBranchIntoHead(g, localRepo, pushBranch)

			// Update the policy and expect another commit in the push branch
			policy.Status.LatestImage = "helloworld:v1.3.1"
			g.Expect(testEnv.Status().Update(ctx, policy)).To(Succeed())
			waitForNewHead(g, localRepo, pushBranch)
			head, err = localRepo.Reference(plumbing.NewRemoteReferenceName(originRemote, pushBranch), true)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(head.String()).NotTo(Equal(headHash))

			// Cleanup the image update automation used above.
			g.Expect(testEnv.Delete(ctx, updateBySetters)).To(Succeed())

			// --- with update strategy setters

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
			updateKey = types.NamespacedName{
				Namespace: gitRepoKey.Namespace,
				Name:      "update-" + randStringRunes(5),
			}
			updateBySetters = &imagev1.ImageUpdateAutomation{
				Spec: imagev1.ImageUpdateAutomationSpec{
					Interval: metav1.Duration{Duration: 2 * time.Hour}, // this is to ensure any subsequent run should be outside the scope of the testing
					SourceRef: imagev1.SourceReference{
						Kind: "GitRepository",
						Name: gitRepoKey.Name,
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
			updateBySetters.Name = updateKey.Name
			updateBySetters.Namespace = updateKey.Namespace
			g.Expect(testEnv.Create(context.Background(), updateBySetters)).To(Succeed())
			// wait for a new commit to be made by the controller
			waitForNewHead(g, localRepo, branch)

			// -- Update to the most recent image.

			head, _ = localRepo.Head()
			commit, err = localRepo.CommitObject(head.Hash())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Message).To(Equal(commitMessage))

			var newObj imagev1.ImageUpdateAutomation
			g.Expect(testEnv.Get(ctx, updateKey, &newObj)).To(Succeed())
			g.Expect(newObj.Status.LastPushCommit).To(Equal(head.Hash().String()))
			g.Expect(newObj.Status.LastPushTime).ToNot(BeNil())

			compareRepoWithExpected(g, cloneLocalRepoURL, branch, "testdata/appconfig-setters-expected", func(tmp string) {
				g.Expect(replaceMarker(tmp, policyKey)).To(Succeed())
			})

			// -- Suspend the update object - reconciliation should not run.

			var updatePatch imagev1.ImageUpdateAutomation
			g.Expect(testEnv.Get(context.TODO(), updateKey, &updatePatch)).To(Succeed())
			updatePatch.Spec.Suspend = true
			g.Expect(testEnv.Patch(context.Background(), &updatePatch, client.Merge)).To(Succeed())

			// Create a new image automation reconciler and run it explicitly.
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

			// -- Reconciles when reconcile request annotation is added.

			// The automation has run, and is not expected to run
			// again for 2 hours. Make a commit to the git repo
			// which needs to be undone by automation, then add
			// the annotation and make sure it runs again.

			// TODO: Implement adding request annotation.
			// Refer: https://github.com/fluxcd/image-automation-controller/pull/82/commits/4fde199362b42fa37068f2e6c6885cfea474a3d1#diff-1168fadffa18bd096582ae7f8b6db744fd896bd5600ee1d1ac6ac4474af251b9L292-L334

			g.Expect(testEnv.Get(context.Background(), updateKey, updateBySetters)).To(Succeed())
			g.Expect(updateBySetters.Status.LastAutomationRunTime).ToNot(BeNil())
		})
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
