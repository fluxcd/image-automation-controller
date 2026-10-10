/*
Copyright 2026 The Flux authors

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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	. "github.com/onsi/gomega"
	"github.com/otiai10/copy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	reflectorv1 "github.com/fluxcd/image-reflector-controller/api/v1"
	"github.com/fluxcd/pkg/gittestserver"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1"
	"github.com/fluxcd/image-automation-controller/internal/features"
	"github.com/fluxcd/image-automation-controller/internal/policy"
	"github.com/fluxcd/image-automation-controller/internal/source"
	"github.com/fluxcd/image-automation-controller/internal/testutil"
	"github.com/fluxcd/image-automation-controller/internal/update"
)

// pushRetryFixture is a source checked out from a Git server, with changes
// applied by an automation that pushes to branch main.
type pushRetryFixture struct {
	gitServer *gittestserver.GitServer
	repoPath  string
	repoURL   string
	sm        *source.SourceManager
	obj       *imagev1.ImageUpdateAutomation
	policies  []reflectorv1.ImagePolicy
	result    update.Result
}

func newPushRetryFixture(t *testing.T, push imagev1.PushSpec) *pushRetryFixture {
	g := NewWithT(t)
	ctx := context.TODO()

	gitServer := testutil.SetUpGitTestServer(g)
	t.Cleanup(func() {
		// Stopping the server waits for its Git processes, which can still
		// be writing to the repository after a push returned.
		gitServer.StopHTTP()
		g.Expect(os.RemoveAll(gitServer.Root())).To(Succeed())
	})

	testNS := "test-ns"
	imgPolicy := &reflectorv1.ImagePolicy{}
	imgPolicy.Name = "policy1"
	imgPolicy.Namespace = testNS
	imgPolicy.Status = reflectorv1.ImagePolicyStatus{
		LatestRef: testutil.ImageToRef("helloworld:1.0.1"),
	}

	workDir := t.TempDir()
	g.Expect(copy.Copy("../source/testdata/appconfig", workDir)).To(Succeed())
	g.Expect(testutil.ReplaceMarker(filepath.Join(workDir, "deploy.yaml"), client.ObjectKeyFromObject(imgPolicy))).To(Succeed())
	repoPath := "/config-" + rand.String(5) + ".git"
	testutil.InitGitRepo(g, gitServer, workDir, "main", repoPath)
	repoURL := gitServer.HTTPAddressWithCredentials() + repoPath

	gitRepo := &sourcev1.GitRepository{}
	gitRepo.Name = "test-repo"
	gitRepo.Namespace = testNS
	gitRepo.Spec = sourcev1.GitRepositorySpec{
		URL:       repoURL,
		Reference: &sourcev1.GitRepositoryRef{Branch: "main"},
	}

	obj := &imagev1.ImageUpdateAutomation{}
	obj.Name = "test-update"
	obj.Namespace = testNS
	obj.Spec = imagev1.ImageUpdateAutomationSpec{
		Interval: metav1.Duration{Duration: time.Hour},
		SourceRef: imagev1.CrossNamespaceSourceReference{
			Kind: sourcev1.GitRepositoryKind,
			Name: gitRepo.Name,
		},
		Update: &imagev1.UpdateStrategy{
			Strategy: imagev1.UpdateStrategySetters,
		},
		GitSpec: &imagev1.GitSpec{
			Push: &push,
		},
	}

	kClient := fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(imgPolicy, gitRepo, obj).Build()

	sm, err := source.NewSourceManager(ctx, kClient, obj)
	g.Expect(err).ToNot(HaveOccurred())
	t.Cleanup(func() {
		g.Expect(sm.Cleanup()).To(Succeed())
	})
	_, err = sm.CheckoutSource(ctx)
	g.Expect(err).ToNot(HaveOccurred())

	policies := []reflectorv1.ImagePolicy{*imgPolicy}
	result, err := policy.ApplyPolicies(ctx, sm.WorkDirectory(), obj, policies)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result.FileChanges).ToNot(BeEmpty())

	return &pushRetryFixture{
		gitServer: gitServer,
		repoPath:  repoPath,
		repoURL:   repoURL,
		sm:        sm,
		obj:       obj,
		policies:  policies,
		result:    result,
	}
}

// installUpdateHook installs an update hook in the repository, which accepts
// a ref update if the shell condition accept is true. The condition can use
// $1, the name of the updated ref, and $count, the number of updates the hook
// has seen, including the current one. It returns a function reporting that
// number.
func (f *pushRetryFixture) installUpdateHook(t *testing.T, accept string) func() int {
	g := NewWithT(t)

	countFile := filepath.Join(t.TempDir(), "count")
	hook := fmt.Sprintf("#!/bin/sh\necho \"$1\" >> '%[1]s'\ncount=$(wc -l < '%[1]s')\n%[2]s\n", countFile, accept)
	hooksDir := filepath.Join(f.gitServer.Root(), f.repoPath, ".git", "hooks")
	g.Expect(os.MkdirAll(hooksDir, 0o700)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(hooksDir, "update"), []byte(hook), 0o700)).To(Succeed())

	return func() int {
		b, err := os.ReadFile(countFile)
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		g.Expect(err).ToNot(HaveOccurred())
		return strings.Count(string(b), "\n")
	}
}

// shortenPushRetryDelay shortens the delays between push attempts for the
// duration of the test.
func shortenPushRetryDelay(t *testing.T) {
	delay := pushRetryBaseDelay
	pushRetryBaseDelay = 10 * time.Millisecond
	t.Cleanup(func() {
		pushRetryBaseDelay = delay
	})
}

func TestCommitAndPushWithRetry_FeatureGateDisabled(t *testing.T) {
	g := NewWithT(t)
	f := newPushRetryFixture(t, imagev1.PushSpec{Branch: "main"})
	pushes := f.installUpdateHook(t, "false")

	r := &ImageUpdateAutomationReconciler{features: map[string]bool{}}
	_, err := r.commitAndPushWithRetry(context.TODO(), f.sm, f.obj, f.policies, f.result, nil)
	g.Expect(err).To(HaveOccurred())
	g.Expect(pushes()).To(Equal(1))
}

func TestCommitAndPushWithRetry_RecoversFromLostRace(t *testing.T) {
	g := NewWithT(t)
	ctx := context.TODO()
	shortenPushRetryDelay(t)
	f := newPushRetryFixture(t, imagev1.PushSpec{Branch: "main"})

	// Another writer pushes after the checkout, so the first push is not a
	// fast-forward.
	winner := testutil.CommitInRepo(ctx, g, f.repoURL, "main", originRemote, "Add winner.txt", func(path string) {
		g.Expect(os.WriteFile(filepath.Join(path, "winner.txt"), nil, 0o600)).To(Succeed())
	})

	r := &ImageUpdateAutomationReconciler{features: map[string]bool{features.GitPushRetryOnFailure: true}}
	pushResult, err := r.commitAndPushWithRetry(ctx, f.sm, f.obj, f.policies, f.result, nil)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(pushResult).ToNot(BeNil())

	repo, dir, err := testutil.Clone(ctx, f.repoURL, "main", originRemote)
	g.Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(dir)
	head, err := repo.Head()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(head.Hash().String()).To(Equal(pushResult.Commit().Hash.String()))
	pushed, err := repo.CommitObject(head.Hash())
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(pushed.ParentHashes).To(Equal([]plumbing.Hash{winner}))
}

func TestCommitAndPushWithRetry_RejectedPushes(t *testing.T) {
	tests := []struct {
		name         string
		accept       string
		wantErr      string
		wantAttempts int
	}{
		{
			name:         "succeeds after rejections",
			accept:       "[ $count -gt 2 ]",
			wantAttempts: 3,
		},
		{
			name:         "gives up after the maximum number of attempts",
			accept:       "false",
			wantErr:      fmt.Sprintf("giving up after %d attempts", pushRetryMaxAttempts),
			wantAttempts: pushRetryMaxAttempts,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			shortenPushRetryDelay(t)
			f := newPushRetryFixture(t, imagev1.PushSpec{Branch: "main"})
			pushes := f.installUpdateHook(t, tt.accept)

			r := &ImageUpdateAutomationReconciler{features: map[string]bool{features.GitPushRetryOnFailure: true}}
			pushResult, err := r.commitAndPushWithRetry(context.TODO(), f.sm, f.obj, f.policies, f.result, nil)
			if tt.wantErr != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
				var pushErr *source.PushBranchError
				g.Expect(errors.As(err, &pushErr)).To(BeTrue())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(pushResult).ToNot(BeNil())
			}
			g.Expect(pushes()).To(Equal(tt.wantAttempts))
		})
	}
}

func TestCommitAndPushWithRetry_StopsAtRetryBudget(t *testing.T) {
	g := NewWithT(t)
	f := newPushRetryFixture(t, imagev1.PushSpec{Branch: "main"})
	pushes := f.installUpdateHook(t, "false")
	// Half the interval is shorter than the delay before the first retry.
	f.obj.Spec.Interval = metav1.Duration{Duration: pushRetryBaseDelay}

	r := &ImageUpdateAutomationReconciler{features: map[string]bool{features.GitPushRetryOnFailure: true}}
	start := time.Now()
	_, err := r.commitAndPushWithRetry(context.TODO(), f.sm, f.obj, f.policies, f.result, nil)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).ToNot(ContainSubstring("giving up"))
	g.Expect(pushes()).To(Equal(1))
	g.Expect(time.Since(start)).To(BeNumerically("<", pushRetryBaseDelay))
}

func TestCommitAndPushWithRetry_RefreshFailureStopsRetrying(t *testing.T) {
	g := NewWithT(t)
	shortenPushRetryDelay(t)
	f := newPushRetryFixture(t, imagev1.PushSpec{Branch: "main"})
	// Pushing, fetching and cloning all fail.
	f.gitServer.StopHTTP()

	r := &ImageUpdateAutomationReconciler{features: map[string]bool{features.GitPushRetryOnFailure: true}}
	_, err := r.commitAndPushWithRetry(context.TODO(), f.sm, f.obj, f.policies, f.result, nil)
	g.Expect(err).To(MatchError(ContainSubstring("failed to refresh the source to retry the push")))
}

func TestCommitAndPushWithRetry_DoesNotRetryOtherErrors(t *testing.T) {
	tests := []struct {
		name            string
		push            imagev1.PushSpec
		messageTemplate string
		accept          string
		wantErr         error
		wantPushes      int
	}{
		{
			name:            "commit message template fails",
			push:            imagev1.PushSpec{Branch: "main"},
			messageTemplate: "{{ .Updated }}",
			accept:          "true",
			wantErr:         source.ErrRemovedTemplateField,
			wantPushes:      0,
		},
		{
			name: "push to the refspec fails",
			push: imagev1.PushSpec{
				Branch:  "main",
				Refspec: "refs/heads/main:refs/heads/release",
			},
			accept:     `[ "$1" != refs/heads/release ]`,
			wantPushes: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			f := newPushRetryFixture(t, tt.push)
			pushes := f.installUpdateHook(t, tt.accept)
			f.obj.Spec.GitSpec.Commit.MessageTemplate = tt.messageTemplate

			r := &ImageUpdateAutomationReconciler{features: map[string]bool{features.GitPushRetryOnFailure: true}}
			start := time.Now()
			_, err := r.commitAndPushWithRetry(context.TODO(), f.sm, f.obj, f.policies, f.result, nil)
			g.Expect(err).To(HaveOccurred())
			if tt.wantErr != nil {
				g.Expect(errors.Is(err, tt.wantErr)).To(BeTrue(), err.Error())
			}
			g.Expect(pushes()).To(Equal(tt.wantPushes))
			g.Expect(time.Since(start)).To(BeNumerically("<", pushRetryBaseDelay))
		})
	}
}
