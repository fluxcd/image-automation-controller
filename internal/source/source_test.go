/*
Copyright 2024 The Flux authors

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

package source

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	"github.com/ProtonMail/go-crypto/openpgp"
	extgogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	"github.com/otiai10/copy"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/gittestserver"
	"github.com/fluxcd/pkg/ssh"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta2"
	"github.com/fluxcd/image-automation-controller/internal/policy"
	"github.com/fluxcd/image-automation-controller/internal/testutil"
)

const (
	originRemote       = "origin"
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
	testCommitTemplateResultV2 = `Commit summary with ResultV2

Automation: {{ .AutomationObject }}

{{ range $filename, $objchange := .Changed.FileChanges -}}
- File: {{ $filename }}
{{- range $obj, $changes := $objchange }}
  - Object: {{ $obj.Kind }}/{{ $obj.Namespace }}/{{ $obj.Name }}
    Changes:
{{- range $_ , $change := $changes }}
    - {{ $change.OldValue }} -> {{ $change.NewValue }}
{{ end -}}
{{ end -}}
{{ end -}}
`

	testCommitTemplateWithValues = `Commit summary

Automation: {{ .AutomationObject }}

Cluster: {{ index .Values "cluster" }}
Testing: {{ .Values.testing }}
`
)

func init() {
	utilruntime.Must(imagev1_reflect.AddToScheme(scheme.Scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme.Scheme))
	utilruntime.Must(imagev1.AddToScheme(scheme.Scheme))

	log.SetLogger(logr.New(log.NullLogSink{}))
}

func Fuzz_templateMsg(f *testing.F) {
	f.Add("template", []byte{})
	f.Add("", []byte{})

	f.Fuzz(func(t *testing.T, template string, seed []byte) {
		var values TemplateData
		fuzz.NewConsumer(seed).GenerateStruct(&values)

		_, _ = templateMsg(template, &values)
	})
}

func TestNewSourceManager(t *testing.T) {
	namespace := "test-ns"
	gitRepoName := "foo"

	tests := []struct {
		name            string
		objSpec         imagev1.ImageUpdateAutomationSpec
		opts            []SourceOption
		sourceNamespace string
		wantErr         bool
	}{
		{
			name: "unsupported source ref kind",
			objSpec: imagev1.ImageUpdateAutomationSpec{
				SourceRef: imagev1.CrossNamespaceSourceReference{
					Kind: "HelmChart",
				},
			},
			wantErr: true,
		},
		{
			name: "empty gitSpec",
			objSpec: imagev1.ImageUpdateAutomationSpec{
				SourceRef: imagev1.CrossNamespaceSourceReference{
					Kind: sourcev1.GitRepositoryKind,
				},
				GitSpec: nil,
			},
			wantErr: true,
		},
		{
			name: "refer cross namespace source",
			objSpec: imagev1.ImageUpdateAutomationSpec{
				SourceRef: imagev1.CrossNamespaceSourceReference{
					Kind:      sourcev1.GitRepositoryKind,
					Name:      gitRepoName,
					Namespace: "foo-ns",
				},
				GitSpec: &imagev1.GitSpec{},
			},
			sourceNamespace: "foo-ns",
		},
		{
			name: "refer cross namespace source with crossnamespace disabled",
			objSpec: imagev1.ImageUpdateAutomationSpec{
				SourceRef: imagev1.CrossNamespaceSourceReference{
					Kind:      sourcev1.GitRepositoryKind,
					Name:      gitRepoName,
					Namespace: "foo-ns",
				},
				GitSpec: &imagev1.GitSpec{},
			},
			sourceNamespace: "foo-ns",
			opts:            []SourceOption{WithSourceOptionNoCrossNamespaceRef()},
			wantErr:         true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			gitRepo := &sourcev1.GitRepository{}
			gitRepo.Name = gitRepoName
			gitRepo.Namespace = tt.sourceNamespace
			gitRepo.Spec = sourcev1.GitRepositorySpec{
				URL:       "https://example.com",
				Reference: &sourcev1.GitRepositoryRef{Branch: "main"},
			}

			clientBuilder := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(gitRepo)
			c := clientBuilder.Build()

			obj := &imagev1.ImageUpdateAutomation{}
			obj.Name = "test-update"
			obj.Namespace = namespace
			obj.Spec = tt.objSpec

			sm, err := NewSourceManager(context.TODO(), c, obj, tt.opts...)
			if (err != nil) != tt.wantErr {
				g.Fail(fmt.Sprintf("unexpected error: %v", err))
				return
			}
			if err == nil {
				g.Expect(os.RemoveAll(sm.WorkDirectory()))
			}
		})
	}
}

func TestSourceManager_CheckoutSource(t *testing.T) {
	test_sourceManager_CheckoutSource(t, "http")
	test_sourceManager_CheckoutSource(t, "ssh")
}

func test_sourceManager_CheckoutSource(t *testing.T, proto string) {
	tests := []struct {
		name         string
		autoGitSpec  *imagev1.GitSpec
		gitRepoRef   *sourcev1.GitRepositoryRef
		shallowClone bool
		lastObserved bool
		wantErr      bool
		wantRef      string
	}{
		{
			name: "checkout for single branch",
			autoGitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{Branch: "main"},
				Checkout: &imagev1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{Branch: "main"},
				},
			},
			wantErr: false,
			wantRef: "main",
		},
		{
			name: "checkout for different push branch",
			autoGitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{Branch: "foo"},
				Checkout: &imagev1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{Branch: "main"},
				},
			},
			wantErr: false,
			wantRef: "foo",
		},
		{
			name: "checkout from gitrepo ref",
			autoGitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{Branch: "main"},
			},
			gitRepoRef: &sourcev1.GitRepositoryRef{
				Branch: "main",
			},
			wantErr: false,
			wantRef: "main",
		},
		{
			name: "with shallow clone",
			autoGitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{Branch: "main"},
				Checkout: &imagev1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{Branch: "main"},
				},
			},
			shallowClone: true,
			wantErr:      false,
			wantRef:      "main",
		},
		{
			name: "with last observed commit",
			autoGitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{Branch: "main"},
				Checkout: &imagev1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{Branch: "main"},
				},
			},
			lastObserved: true,
			wantErr:      false,
			wantRef:      "main",
		},
		{
			name: "checkout non-existing branch",
			autoGitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{Branch: "main"},
				Checkout: &imagev1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{Branch: "non-existing"},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s(%s)", tt.name, proto), func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.TODO()
			testObjects := []client.Object{}
			testNS := "test-ns"

			// Run git server.
			gitServer := testutil.SetUpGitTestServer(g)
			t.Cleanup(func() {
				g.Expect(os.RemoveAll(gitServer.Root())).ToNot(HaveOccurred())
				gitServer.StopHTTP()
			})

			// Start the ssh server if needed.
			if proto == "ssh" {
				go func() {
					gitServer.StartSSH()
				}()
				defer func() {
					g.Expect(gitServer.StopSSH()).To(Succeed())
				}()
			}

			// Create a git repo on the server.
			fixture := "testdata/appconfig"
			branch := rand.String(5)
			repoPath := "/config-" + rand.String(5) + ".git"
			initRepo := testutil.InitGitRepo(g, gitServer, fixture, branch, repoPath)
			// Obtain the head revision reference.
			initHead, err := initRepo.Head()
			g.Expect(err).ToNot(HaveOccurred())
			headRev := fmt.Sprintf("%s@sha1:%s", initHead.Name().Short(), initHead.Hash().String())

			repoURL, err := getRepoURL(gitServer, repoPath, proto)
			g.Expect(err).ToNot(HaveOccurred())

			// Create GitRepository for the above git repository.
			gitRepo := &sourcev1.GitRepository{}
			gitRepo.Name = "test-repo"
			gitRepo.Namespace = testNS
			gitRepo.Spec = sourcev1.GitRepositorySpec{
				URL: repoURL,
			}
			if tt.gitRepoRef != nil {
				gitRepo.Spec.Reference = tt.gitRepoRef
			}
			// Create ssh Secret for the GitRepository.
			if proto == "ssh" {
				sshSecretName := "ssh-key-" + rand.String(5)
				sshSecret, err := getSSHIdentitySecret(sshSecretName, testNS, repoURL)
				g.Expect(err).ToNot(HaveOccurred())
				testObjects = append(testObjects, sshSecret)

				gitRepo.Spec.SecretRef = &meta.LocalObjectReference{Name: sshSecretName}
			}
			testObjects = append(testObjects, gitRepo)

			// Create an ImageUpdateAutomation to checkout the above git
			// repository.
			updateAuto := &imagev1.ImageUpdateAutomation{}
			updateAuto.Name = "test-update"
			updateAuto.Namespace = testNS
			updateAuto.Spec = imagev1.ImageUpdateAutomationSpec{
				GitSpec: tt.autoGitSpec,
				SourceRef: imagev1.CrossNamespaceSourceReference{
					Kind: sourcev1.GitRepositoryKind,
					Name: gitRepo.Name,
				},
			}
			testObjects = append(testObjects, updateAuto)

			kClient := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(testObjects...).
				Build()

			sm, err := NewSourceManager(ctx, kClient, updateAuto, WithSourceOptionGitAllBranchReferences())
			g.Expect(err).ToNot(HaveOccurred())

			defer func() {
				g.Expect(sm.Cleanup()).ToNot(HaveOccurred())
			}()

			opts := []CheckoutOption{}
			if tt.shallowClone {
				opts = append(opts, WithCheckoutOptionShallowClone())
			}
			if tt.lastObserved {
				opts = append(opts, WithCheckoutOptionLastObserved(headRev))
			}
			commit, err := sm.CheckoutSource(ctx, opts...)
			if (err != nil) != tt.wantErr {
				g.Fail("unexpected error")
				return
			}
			if err == nil {
				if tt.lastObserved {
					g.Expect(git.IsConcreteCommit(*commit)).To(BeFalse())
					// Didn't download anything, can't check anything.
				} else {
					g.Expect(git.IsConcreteCommit(*commit)).To(BeTrue())
					// Inspect the cloned repository.
					r, err := extgogit.PlainOpen(sm.workingDir)
					g.Expect(err).ToNot(HaveOccurred())
					ref, err := r.Head()
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(ref.Name().Short()).To(Equal(tt.wantRef))
				}
			}
		})
	}
}

func TestSourceManager_CommitAndPush(t *testing.T) {
	test_sourceManager_CommitAndPush(t, "http")
	test_sourceManager_CommitAndPush(t, "ssh")
}

func test_sourceManager_CommitAndPush(t *testing.T, proto string) {
	tests := []struct {
		name               string
		gitSpec            *imagev1.GitSpec
		gitRepoReference   *sourcev1.GitRepositoryRef
		latestImage        string
		noChange           bool
		wantErr            bool
		wantCommitMsg      string
		checkRefSpecBranch string
	}{
		{
			name: "push to cloned branch with custom template",
			gitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{
					Branch: "main",
				},
				Commit: imagev1.CommitSpec{
					MessageTemplate: testCommitTemplate,
				},
			},
			gitRepoReference: &sourcev1.GitRepositoryRef{
				Branch: "main",
			},
			latestImage: "helloworld:1.0.1",
			wantErr:     false,
			wantCommitMsg: `Commit summary

Automation: test-ns/test-update

Files:
- deploy.yaml
Objects:
- deployment test
Images:
- helloworld:1.0.1 (policy1)
`,
		},
		{
			name: "commit with update ResultV2 template",
			gitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{
					Branch: "main",
				},
				Commit: imagev1.CommitSpec{
					MessageTemplate: testCommitTemplateResultV2,
				},
			},
			gitRepoReference: &sourcev1.GitRepositoryRef{
				Branch: "main",
			},
			latestImage: "helloworld:1.0.1",
			wantErr:     false,
			wantCommitMsg: `Commit summary with ResultV2

Automation: test-ns/test-update

- File: deploy.yaml
  - Object: Deployment//test
    Changes:
    - helloworld:1.0.0 -> helloworld:1.0.1
`,
		},
		{
			name: "push to cloned branch with template and values",
			gitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{
					Branch: "main",
				},
				Commit: imagev1.CommitSpec{
					MessageTemplate: testCommitTemplateWithValues,
					MessageTemplateValues: map[string]string{
						"cluster": "prod",
						"testing": "value",
					},
				},
			},
			gitRepoReference: &sourcev1.GitRepositoryRef{
				Branch: "main",
			},
			latestImage: "helloworld:1.0.1",
			wantErr:     false,
			wantCommitMsg: `Commit summary

Automation: test-ns/test-update

Cluster: prod
Testing: value
`,
		},

		{
			name: "push to different branch",
			gitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{
					Branch: "main2",
				},
			},
			gitRepoReference: &sourcev1.GitRepositoryRef{
				Branch: "main",
			},
			latestImage:   "helloworld:1.0.1",
			wantErr:       false,
			wantCommitMsg: defaultMessageTemplate,
		},
		{
			name: "push to cloned branch+refspec",
			gitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{
					Branch:  "main",
					Refspec: "refs/heads/main:refs/heads/smth/else",
				},
			},
			gitRepoReference: &sourcev1.GitRepositoryRef{
				Branch: "main",
			},
			latestImage:        "helloworld:1.0.1",
			wantErr:            false,
			wantCommitMsg:      defaultMessageTemplate,
			checkRefSpecBranch: "smth/else",
		},
		{
			name: "push to different branch+refspec",
			gitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{
					Branch:  "auto",
					Refspec: "refs/heads/auto:refs/heads/smth/else",
				},
			},
			gitRepoReference: &sourcev1.GitRepositoryRef{
				Branch: "main",
			},
			latestImage:        "helloworld:1.0.1",
			wantErr:            false,
			wantCommitMsg:      defaultMessageTemplate,
			checkRefSpecBranch: "smth/else",
		},
		{
			name: "push to branch from tag",
			gitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{
					Branch: "main2",
				},
			},
			gitRepoReference: &sourcev1.GitRepositoryRef{
				Tag: "v1.0.0",
			},
			latestImage:   "helloworld:1.0.1",
			wantErr:       false,
			wantCommitMsg: defaultMessageTemplate,
		},
		{
			name: "push signed commit to branch",
			gitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{
					Branch: "main",
				},
				Commit: imagev1.CommitSpec{
					Author: imagev1.CommitUser{
						Name:  "Flux B Ot",
						Email: "fluxbot@example.com",
					},
					SigningKey: &imagev1.SigningKey{
						SecretRef: meta.LocalObjectReference{
							Name: "test-signing-key",
						},
					},
				},
			},
			gitRepoReference: &sourcev1.GitRepositoryRef{
				Branch: "main",
			},
			latestImage:   "helloworld:1.0.1",
			wantErr:       false,
			wantCommitMsg: defaultMessageTemplate,
		},
		{
			name: "no change to push",
			gitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{
					Branch: "main",
				},
			},
			gitRepoReference: &sourcev1.GitRepositoryRef{
				Branch: "main",
			},
			latestImage: "helloworld:1.0.0",
			noChange:    true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s(%s)", tt.name, proto), func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.TODO()
			testObjects := []client.Object{}

			// Run git server.
			gitServer := testutil.SetUpGitTestServer(g)
			t.Cleanup(func() {
				g.Expect(os.RemoveAll(gitServer.Root())).ToNot(HaveOccurred())
				gitServer.StopHTTP()
			})

			// Start the ssh server if needed.
			if proto == "ssh" {
				go func() {
					gitServer.StartSSH()
				}()
				defer func() {
					g.Expect(gitServer.StopSSH()).To(Succeed())
				}()
			}

			// Prepare test directory.
			workDir := t.TempDir()
			testNS := "test-ns"

			imgPolicy := &imagev1_reflect.ImagePolicy{}
			imgPolicy.Name = "policy1"
			imgPolicy.Namespace = testNS
			imgPolicy.Status = imagev1_reflect.ImagePolicyStatus{
				LatestImage: tt.latestImage,
			}
			testObjects = append(testObjects, imgPolicy)
			policyKey := client.ObjectKeyFromObject(imgPolicy)

			fixture := "testdata/appconfig"
			g.Expect(copy.Copy(fixture, workDir)).ToNot(HaveOccurred())
			// Update the setters in the test data.
			g.Expect(testutil.ReplaceMarker(filepath.Join(workDir, "deploy.yaml"), policyKey))

			// Create a git repo with the test directory content.
			branch := "main"
			repoPath := "/config-" + rand.String(5) + ".git"
			repo := testutil.InitGitRepo(g, gitServer, workDir, branch, repoPath)

			// Create a tag.
			if tt.gitRepoReference.Tag != "" {
				h, err := repo.Head()
				g.Expect(err).ToNot(HaveOccurred())
				testutil.TagCommit(g, repo, h.Hash(), false, tt.gitRepoReference.Tag, time.Now())
			}

			cloneLocalRepoURL := gitServer.HTTPAddressWithCredentials() + repoPath

			repoURL, err := getRepoURL(gitServer, repoPath, proto)
			g.Expect(err).ToNot(HaveOccurred())

			// Create GitRepository for the above git repository.
			gitRepo := &sourcev1.GitRepository{}
			gitRepo.Name = "test-repo"
			gitRepo.Namespace = testNS
			gitRepo.Spec = sourcev1.GitRepositorySpec{
				URL: repoURL,
			}
			if tt.gitRepoReference != nil {
				gitRepo.Spec.Reference = tt.gitRepoReference
			}
			// Create ssh Secret for the GitRepository.
			if proto == "ssh" {
				sshSecretName := "ssh-key-" + rand.String(5)
				sshSecret, err := getSSHIdentitySecret(sshSecretName, testNS, repoURL)
				g.Expect(err).ToNot(HaveOccurred())
				testObjects = append(testObjects, sshSecret)

				gitRepo.Spec.SecretRef = &meta.LocalObjectReference{Name: sshSecretName}
			}
			testObjects = append(testObjects, gitRepo)

			// Create an ImageUpdateAutomation to update the above git repository.
			updateAuto := &imagev1.ImageUpdateAutomation{}
			updateAuto.Name = "test-update"
			updateAuto.Namespace = testNS
			updateAuto.Spec = imagev1.ImageUpdateAutomationSpec{
				SourceRef: imagev1.CrossNamespaceSourceReference{
					Kind: sourcev1.GitRepositoryKind,
					Name: gitRepo.Name,
				},
				Update: &imagev1.UpdateStrategy{
					Strategy: imagev1.UpdateStrategySetters,
				},
			}
			testObjects = append(testObjects, updateAuto)

			var pgpEntity *openpgp.Entity
			var signingSecret *corev1.Secret
			if tt.gitSpec != nil {
				updateAuto.Spec.GitSpec = tt.gitSpec

				if tt.gitSpec.Commit.SigningKey != nil {
					signingSecret, pgpEntity = testutil.GetSigningKeyPairSecret(g, tt.gitSpec.Commit.SigningKey.SecretRef.Name, testNS)
					testObjects = append(testObjects, signingSecret)
				}
			}

			kClient := fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(testObjects...).Build()

			sm, err := NewSourceManager(ctx, kClient, updateAuto, WithSourceOptionGitAllBranchReferences())
			g.Expect(err).ToNot(HaveOccurred())
			defer func() {
				g.Expect(sm.Cleanup()).ToNot(HaveOccurred())
			}()

			_, err = sm.CheckoutSource(ctx)
			g.Expect(err).ToNot(HaveOccurred())

			policies := []imagev1_reflect.ImagePolicy{*imgPolicy}
			result, err := policy.ApplyPolicies(ctx, sm.workingDir, updateAuto, policies)
			g.Expect(err).ToNot(HaveOccurred())

			pushResult, err := sm.CommitAndPush(ctx, updateAuto, result)
			g.Expect(err != nil).To(Equal(tt.wantErr))
			if tt.noChange {
				g.Expect(pushResult).To(BeNil())
				return
			}

			// Inspect the pushed commit in the repository.
			localRepo, cloneDir, err := testutil.Clone(ctx, cloneLocalRepoURL, sm.srcCfg.pushBranch, originRemote)
			g.Expect(err).ToNot(HaveOccurred())
			defer func() { os.RemoveAll(cloneDir) }()

			head, _ := localRepo.Head()
			pushBranchHash := head.Hash()
			commit, err := localRepo.CommitObject(pushBranchHash)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Hash.String()).To(Equal(pushResult.Commit().Hash.String()))
			g.Expect(commit.Message).To(Equal(tt.wantCommitMsg))
			// Verify commit signature.
			if pgpEntity != nil {
				// Separate the commit signature and content, and verify with
				// the known PGP Entity.
				c := *commit
				c.PGPSignature = ""
				encoded := &plumbing.MemoryObject{}
				g.Expect(c.Encode(encoded)).ToNot(HaveOccurred())
				content, err := encoded.Reader()
				g.Expect(err).ToNot(HaveOccurred())

				kr := openpgp.EntityList([]*openpgp.Entity{pgpEntity})
				signature := strings.NewReader(commit.PGPSignature)

				_, err = openpgp.CheckArmoredDetachedSignature(kr, content, signature, nil)
				g.Expect(err).ToNot(HaveOccurred())
			}

			// Clone the repo at refspec and verify its commit.
			if tt.gitSpec.Push.Refspec != "" {
				refLocalRepo, cloneDir, err := testutil.Clone(ctx, cloneLocalRepoURL, tt.checkRefSpecBranch, originRemote)
				g.Expect(err).ToNot(HaveOccurred())
				defer func() { os.RemoveAll(cloneDir) }()
				refName := plumbing.NewRemoteReferenceName(extgogit.DefaultRemoteName, tt.checkRefSpecBranch)
				ref, err := refLocalRepo.Reference(refName, true)
				g.Expect(err).ToNot(HaveOccurred())
				refspecHash := ref.Hash()
				g.Expect(pushBranchHash).To(Equal(refspecHash))
			}
		})
	}
}

// Test_pushBranchUpdateScenarios tests the push operation for different states
// of the remote repository.
func Test_pushBranchUpdateScenarios(t *testing.T) {
	// This test requires all branch references to be enabled.
	sourceOpts := []SourceOption{WithSourceOptionGitAllBranchReferences()}

	testcases := []struct {
		name         string
		checkoutOpts []CheckoutOption
		pushConfig   []PushConfig
	}{
		{
			name: "default checkout and push configs",
		},
		{
			name: "shallow clone and force push",
			checkoutOpts: []CheckoutOption{
				WithCheckoutOptionShallowClone(),
			},
			pushConfig: []PushConfig{
				WithPushConfigForce(),
			},
		},
	}

	for _, tt := range testcases {
		for _, proto := range []string{"http", "ssh"} {
			t.Run(fmt.Sprintf("%s(%s)", tt.name, proto), func(t *testing.T) {
				test_pushBranchUpdateScenarios(t, proto, sourceOpts, tt.checkoutOpts, tt.pushConfig)
			})
		}
	}
}

func test_pushBranchUpdateScenarios(t *testing.T, proto string, srcOpts []SourceOption, checkoutOpts []CheckoutOption, pushCfg []PushConfig) {
	g := NewWithT(t)
	ctx := context.TODO()
	testObjects := []client.Object{}

	// Run git server.
	gitServer := testutil.SetUpGitTestServer(g)
	t.Cleanup(func() {
		g.Expect(os.RemoveAll(gitServer.Root())).ToNot(HaveOccurred())
		gitServer.StopHTTP()
	})

	// Start the ssh server if needed.
	if proto == "ssh" {
		go func() {
			gitServer.StartSSH()
		}()
		defer func() {
			g.Expect(gitServer.StopSSH()).To(Succeed())
		}()
	}

	// Prepare test directory.
	workDir := t.TempDir()
	testNS := "test-ns"
	fixture := "testdata/appconfig"
	g.Expect(copy.Copy(fixture, workDir)).ToNot(HaveOccurred())

	// Create a git repo with the test directory content.
	branch := "main"
	repoPath := "/config-" + rand.String(5) + ".git"
	_ = testutil.InitGitRepo(g, gitServer, workDir, branch, repoPath)
	pushBranch := "pr-" + rand.String(5)

	cloneLocalRepoURL := gitServer.HTTPAddressWithCredentials() + repoPath

	repoURL, err := getRepoURL(gitServer, repoPath, proto)
	g.Expect(err).ToNot(HaveOccurred())

	// Clone the repo locally.
	localRepo, cloneDir, err := testutil.Clone(ctx, cloneLocalRepoURL, branch, originRemote)
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { os.RemoveAll(cloneDir) }()

	// Create ImagePolicy, GitRepository and ImageUpdateAutomation objects.
	latestImage := "helloworld:1.0.1"

	imgPolicy := &imagev1_reflect.ImagePolicy{}
	imgPolicy.Name = "policy1"
	imgPolicy.Namespace = testNS
	imgPolicy.Status = imagev1_reflect.ImagePolicyStatus{
		LatestImage: latestImage,
	}
	testObjects = append(testObjects, imgPolicy)
	// Take the policyKey to update the setter marker with.
	policyKey := client.ObjectKeyFromObject(imgPolicy)

	gitRepo := &sourcev1.GitRepository{}
	gitRepo.Name = "test-repo"
	gitRepo.Namespace = testNS
	gitRepo.Spec = sourcev1.GitRepositorySpec{
		URL: repoURL,
		// Set a reference to main branch explicitly. If unspecified, it'll
		// default to "master". The test repo above is set up against "main"
		// branch.
		Reference: &sourcev1.GitRepositoryRef{
			Branch: "main",
		},
	}
	// Create ssh Secret for the GitRepository.
	if proto == "ssh" {
		sshSecretName := "ssh-key-" + rand.String(5)
		sshSecret, err := getSSHIdentitySecret(sshSecretName, testNS, repoURL)
		g.Expect(err).ToNot(HaveOccurred())
		testObjects = append(testObjects, sshSecret)

		gitRepo.Spec.SecretRef = &meta.LocalObjectReference{Name: sshSecretName}
	}
	testObjects = append(testObjects, gitRepo)

	commitTemplate := "Commit a difference " + rand.String(5)

	updateAuto := &imagev1.ImageUpdateAutomation{}
	updateAuto.Name = "test-update"
	updateAuto.Namespace = testNS
	updateAuto.Spec = imagev1.ImageUpdateAutomationSpec{
		SourceRef: imagev1.CrossNamespaceSourceReference{
			Kind: sourcev1.GitRepositoryKind,
			Name: gitRepo.Name,
		},
		Update: &imagev1.UpdateStrategy{
			Strategy: imagev1.UpdateStrategySetters,
		},
		GitSpec: &imagev1.GitSpec{
			Push: &imagev1.PushSpec{
				Branch: pushBranch,
			},
			Commit: imagev1.CommitSpec{
				MessageTemplate: commitTemplate,
			},
		},
	}
	testObjects = append(testObjects, updateAuto)

	kClient := fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(testObjects...).Build()

	// Commit in the repository, updating the source with setter markers.
	preChangeCommitId := testutil.CommitIdFromBranch(localRepo, branch)
	testutil.CommitInRepo(ctx, g, cloneLocalRepoURL, branch, originRemote, "Install setter marker", func(tmp string) {
		g.Expect(testutil.ReplaceMarker(filepath.Join(tmp, "deploy.yaml"), policyKey)).To(Succeed())
	})
	// Pull the pushed changes in the local repo.
	testutil.WaitForNewHead(g, localRepo, branch, originRemote, preChangeCommitId)

	// ======= Scenario 1 =======
	// Push to a separate push branch.

	checkoutBranchHead, err := testutil.HeadFromBranch(localRepo, branch)
	g.Expect(err).ToNot(HaveOccurred())

	policies := []imagev1_reflect.ImagePolicy{*imgPolicy}
	checkoutAndUpdate(ctx, g, kClient, updateAuto, policies, srcOpts, checkoutOpts, pushCfg)

	// Pull the new changes to the local repo.
	preChangeCommitId = testutil.CommitIdFromBranch(localRepo, branch)
	testutil.WaitForNewHead(g, localRepo, pushBranch, originRemote, preChangeCommitId)

	// Check the commits in the branches.
	pushBranchHead, err := testutil.GetRemoteHead(localRepo, pushBranch, originRemote)
	g.Expect(err).NotTo(HaveOccurred())
	commit, err := localRepo.CommitObject(pushBranchHead)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(commit.Message).To(Equal(commitTemplate))

	// previous commits should still exist in the tree.
	// regression check to ensure previous commits were not squashed.
	oldCommit, err := localRepo.CommitObject(checkoutBranchHead.Hash)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(oldCommit).ToNot(BeNil())

	// ======= Scenario 2 =======
	// Push branch gets updated.

	checkoutBranchHead, err = testutil.HeadFromBranch(localRepo, branch)
	g.Expect(err).ToNot(HaveOccurred())

	// Get the head of push branch before update.
	pushBranchHead, err = testutil.GetRemoteHead(localRepo, pushBranch, originRemote)
	g.Expect(err).ToNot(HaveOccurred())

	// Update latest image.
	latestImage = "helloworld:v1.3.0"
	imgPolicy.Status.LatestImage = latestImage
	g.Expect(kClient.Update(ctx, imgPolicy)).To(Succeed())

	policies = []imagev1_reflect.ImagePolicy{*imgPolicy}
	checkoutAndUpdate(ctx, g, kClient, updateAuto, policies, srcOpts, checkoutOpts, pushCfg)

	// Pull the new changes to the local repo.
	preChangeCommitId = testutil.CommitIdFromBranch(localRepo, branch)
	testutil.WaitForNewHead(g, localRepo, pushBranch, originRemote, preChangeCommitId)

	newPushBranchHead, err := testutil.GetRemoteHead(localRepo, pushBranch, originRemote)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(newPushBranchHead.String()).NotTo(Equal(pushBranchHead))

	// previous commits should still exist in the tree.
	// regression check to ensure previous commits were not squashed.
	oldCommit, err = localRepo.CommitObject(checkoutBranchHead.Hash)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(oldCommit).ToNot(BeNil())

	// ======= Scenario 3 =======
	// Still pushes to push branch after it's merged.
	checkoutBranchHead, err = testutil.HeadFromBranch(localRepo, branch)
	g.Expect(err).ToNot(HaveOccurred())

	// Get the head of push branch before update.
	pushBranchHead, err = testutil.GetRemoteHead(localRepo, pushBranch, originRemote)
	g.Expect(err).ToNot(HaveOccurred())

	// Merge the push branch into checkout branch, and push the merge commit
	// upstream.
	// WaitForNewHead() leaves the repo at the head of the branch given, i.e., the
	// push branch, so we have to check out the "main" branch first.
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

	// Update latest image.
	latestImage = "helloworld:v1.3.1"
	imgPolicy.Status.LatestImage = latestImage
	g.Expect(kClient.Update(ctx, imgPolicy)).To(Succeed())

	policies = []imagev1_reflect.ImagePolicy{*imgPolicy}
	checkoutAndUpdate(ctx, g, kClient, updateAuto, policies, srcOpts, checkoutOpts, pushCfg)

	// Pull the new changes to the local repo.
	preChangeCommitId = testutil.CommitIdFromBranch(localRepo, branch)
	testutil.WaitForNewHead(g, localRepo, pushBranch, originRemote, preChangeCommitId)

	newPushBranchHead, err = testutil.GetRemoteHead(localRepo, pushBranch, originRemote)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(newPushBranchHead.String()).NotTo(Equal(pushBranchHead))

	// previous commits should still exist in the tree.
	// regression check to ensure previous commits were not squashed.
	oldCommit, err = localRepo.CommitObject(checkoutBranchHead.Hash)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(oldCommit).ToNot(BeNil())
}

func TestPushResult_Summary(t *testing.T) {
	testRev := "a47b32f4814810acac804df5054ec37cbfdbfb53"
	testRevShort := testRev[:7]
	testBranch := "test-branch"

	tests := []struct {
		name        string
		rev         string
		commitMsg   string
		refspecs    []string
		wantSummary string
		wantErr     bool
	}{
		{
			name:        "only push branch",
			rev:         testRev,
			commitMsg:   defaultMessageTemplate,
			wantSummary: fmt.Sprintf("pushed commit '%s' to branch '%s'\nUpdate from image update automation", testRevShort, testBranch),
		},
		{
			name:      "with custom template",
			rev:       testRev,
			commitMsg: "test commit message",
			wantSummary: fmt.Sprintf(`pushed commit '%s' to branch '%s'
test commit message`,
				testRevShort, testBranch),
		},
		{
			name:        "no template",
			rev:         testRev,
			wantSummary: fmt.Sprintf("pushed commit '%s' to branch '%s'", testRevShort, testBranch),
		},
		{
			name:      "with refspec",
			rev:       testRev,
			commitMsg: defaultMessageTemplate,
			refspecs:  []string{"refs/heads/auto:refs/heads/smth/else", "refs/heads/auto:refs/heads/foo"},
			wantSummary: fmt.Sprintf(`pushed commit '%s' to branch '%s' and refspecs 'refs/heads/auto:refs/heads/smth/else', 'refs/heads/auto:refs/heads/foo'
Update from image update automation`, testRevShort, testBranch),
		},
		{
			name:      "short rev",
			rev:       "foo",
			commitMsg: defaultMessageTemplate,
			wantSummary: fmt.Sprintf(`pushed commit '%s' to branch '%s'
Update from image update automation`, "foo", testBranch),
		},
		{
			name:    "empty rev",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			prOpts := []PushResultOption{WithPushResultRefspec(tt.refspecs)}
			pr, err := NewPushResult(testBranch, tt.rev, tt.commitMsg, prOpts...)
			if (err != nil) != tt.wantErr {
				g.Fail("unexpected error")
				return
			}
			if err == nil {
				g.Expect(pr.Summary()).To(Equal(tt.wantSummary))
			}
		})
	}
}

// checkoutAndUpdate performs source checkout, update and push for the given
// arguments.
func checkoutAndUpdate(ctx context.Context, g *WithT, kClient client.Client,
	updateAuto *imagev1.ImageUpdateAutomation, policies []imagev1_reflect.ImagePolicy,
	srcOpts []SourceOption, checkoutOpts []CheckoutOption, pushCfg []PushConfig) {
	g.THelper()

	sm, err := NewSourceManager(ctx, kClient, updateAuto, srcOpts...)
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { g.Expect(sm.Cleanup()).ToNot(HaveOccurred()) }()

	_, err = sm.CheckoutSource(ctx, checkoutOpts...)
	g.Expect(err).ToNot(HaveOccurred())

	result, err := policy.ApplyPolicies(ctx, sm.WorkDirectory(), updateAuto, policies)
	g.Expect(err).ToNot(HaveOccurred())

	_, err = sm.CommitAndPush(ctx, updateAuto, result, pushCfg...)
	g.Expect(err).ToNot(HaveOccurred())
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

func getSSHIdentitySecret(name, namespace, repoURL string) (*corev1.Secret, error) {
	url, err := url.Parse(repoURL)
	if err != nil {
		return nil, err
	}
	knownhosts, err := ssh.ScanHostKey(url.Host, 5*time.Second, []string{}, false)
	if err != nil {
		return nil, err
	}
	keygen := ssh.NewRSAGenerator(2048)
	pair, err := keygen.Generate()
	if err != nil {
		return nil, err
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
	return sec, nil
}
