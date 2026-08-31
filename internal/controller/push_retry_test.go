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
	"os"
	"path/filepath"
	"testing"
	"time"

	extgogit "github.com/go-git/go-git/v5"
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
)

// newPushRetryFixture sets up a git server with a seeded repo and a
// SourceManager checked out against it - the common starting point for every
// commitAndPushWithRetry test below. The returned gitServer lets a test break
// connectivity deliberately (e.g. to force a non-conflict push error).
func newPushRetryFixture(t *testing.T, interval time.Duration) (*WithT, *gittestserver.GitServer, *source.SourceManager, *imagev1.ImageUpdateAutomation, []reflectorv1.ImagePolicy, string) {
	g := NewWithT(t)
	ctx := context.TODO()

	gitServer := testutil.SetUpGitTestServer(g)
	t.Cleanup(func() {
		g.Expect(os.RemoveAll(gitServer.Root())).ToNot(HaveOccurred())
		gitServer.StopHTTP()
	})

	testNS := "test-ns"
	workDir := t.TempDir()
	branch := "main"

	imgPolicy := &reflectorv1.ImagePolicy{}
	imgPolicy.Name = "policy1"
	imgPolicy.Namespace = testNS
	imgPolicy.Status = reflectorv1.ImagePolicyStatus{
		LatestRef: testutil.ImageToRef("helloworld:1.0.1"),
	}
	policyKey := client.ObjectKeyFromObject(imgPolicy)

	fixture := "../source/testdata/appconfig"
	g.Expect(copy.Copy(fixture, workDir)).ToNot(HaveOccurred())
	g.Expect(testutil.ReplaceMarker(filepath.Join(workDir, "deploy.yaml"), policyKey))

	repoPath := "/config-" + rand.String(5) + ".git"
	testutil.InitGitRepo(g, gitServer, workDir, branch, repoPath)
	cloneLocalRepoURL := gitServer.HTTPAddressWithCredentials() + repoPath

	gitRepo := &sourcev1.GitRepository{}
	gitRepo.Name = "test-repo"
	gitRepo.Namespace = testNS
	gitRepo.Spec = sourcev1.GitRepositorySpec{
		URL:       cloneLocalRepoURL,
		Reference: &sourcev1.GitRepositoryRef{Branch: branch},
	}

	updateAuto := &imagev1.ImageUpdateAutomation{}
	updateAuto.Name = "test-update"
	updateAuto.Namespace = testNS
	updateAuto.Spec = imagev1.ImageUpdateAutomationSpec{
		Interval: metav1.Duration{Duration: interval},
		SourceRef: imagev1.CrossNamespaceSourceReference{
			Kind: sourcev1.GitRepositoryKind,
			Name: gitRepo.Name,
		},
		Update: &imagev1.UpdateStrategy{
			Strategy: imagev1.UpdateStrategySetters,
		},
		GitSpec: &imagev1.GitSpec{
			Push: &imagev1.PushSpec{Branch: branch},
		},
	}

	kClient := fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(imgPolicy, gitRepo, updateAuto).Build()

	sm, err := source.NewSourceManager(ctx, kClient, updateAuto)
	g.Expect(err).ToNot(HaveOccurred())
	t.Cleanup(func() {
		g.Expect(sm.Cleanup()).ToNot(HaveOccurred())
	})

	_, err = sm.CheckoutSource(ctx)
	g.Expect(err).ToNot(HaveOccurred())

	return g, gitServer, sm, updateAuto, []reflectorv1.ImagePolicy{*imgPolicy}, cloneLocalRepoURL
}

// pushCompetingCommit pushes a change directly to the remote branch,
// out-of-band from sm, simulating another ImageUpdateAutomation winning the
// push race.
func pushCompetingCommit(g *WithT, repoURL, branch, filename string) {
	ctx := context.TODO()
	repo, dir, err := testutil.Clone(ctx, repoURL, branch, originRemote)
	g.Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(dir)
	g.Expect(os.WriteFile(filepath.Join(dir, filename), []byte("competing change"), 0o644)).To(Succeed())
	testutil.CommitWorkDir(g, repo, branch, "competing change")
	remote, err := repo.Remote(originRemote)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(remote.PushContext(ctx, &extgogit.PushOptions{RemoteName: originRemote})).To(Succeed())
}

func TestCommitAndPushWithRetry_RecoversFromConflict(t *testing.T) {
	g, _, sm, obj, policies, repoURL := newPushRetryFixture(t, time.Hour)
	ctx := context.TODO()

	result, err := policy.ApplyPolicies(ctx, sm.WorkDirectory(), obj, policies)
	g.Expect(err).ToNot(HaveOccurred())

	// One competing commit lands before our first attempt - we should
	// recover on the second attempt.
	pushCompetingCommit(g, repoURL, "main", "competitor-1.txt")

	r := &ImageUpdateAutomationReconciler{features: map[string]bool{features.GitPushRetryOnConflict: true}}
	start := time.Now()
	pushResult, err := r.commitAndPushWithRetry(ctx, sm, obj, policies, result, nil)
	elapsed := time.Since(start)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(pushResult).ToNot(BeNil())
	// One retry means exactly one backoff sleep (pushRetryBaseDelay).
	g.Expect(elapsed).To(BeNumerically(">=", pushRetryBaseDelay))
	g.Expect(elapsed).To(BeNumerically("<", 2*pushRetryBaseDelay))
}

func TestCommitAndPushWithRetry_ExhaustsAttempts(t *testing.T) {
	g, _, sm, obj, policies, repoURL := newPushRetryFixture(t, time.Hour)
	ctx := context.TODO()

	origMaxAttempts, origBaseDelay := pushRetryMaxAttempts, pushRetryBaseDelay
	pushRetryMaxAttempts = 3
	pushRetryBaseDelay = 10 * time.Millisecond
	t.Cleanup(func() { pushRetryMaxAttempts, pushRetryBaseDelay = origMaxAttempts, origBaseDelay })

	result, err := policy.ApplyPolicies(ctx, sm.WorkDirectory(), obj, policies)
	g.Expect(err).ToNot(HaveOccurred())

	competitorRepo, competitorDir, err := testutil.Clone(ctx, repoURL, "main", originRemote)
	g.Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(competitorDir)
	remote, err := competitorRepo.Remote(originRemote)
	g.Expect(err).ToNot(HaveOccurred())

	// A competitor wins every single attempt: the test hook fires
	// synchronously right before each attempt's CommitAndPush, so a fresh
	// competing commit deterministically lands inside the race window
	// instead of racing wall-clock timing against a background pusher.
	pushRetryTestHook = func(attempt int) {
		name := filepath.Join(competitorDir, "competitor-"+rand.String(5)+".txt")
		g.Expect(os.WriteFile(name, []byte("competing change"), 0o644)).To(Succeed())
		testutil.CommitWorkDir(g, competitorRepo, "main", "competing change")
		g.Expect(remote.PushContext(ctx, &extgogit.PushOptions{RemoteName: originRemote})).To(Succeed())
	}
	t.Cleanup(func() { pushRetryTestHook = nil })

	r := &ImageUpdateAutomationReconciler{features: map[string]bool{features.GitPushRetryOnConflict: true}}
	_, err = r.commitAndPushWithRetry(ctx, sm, obj, policies, result, nil)

	g.Expect(err).To(HaveOccurred())
	g.Expect(source.IsPushConflict(err)).To(BeTrue())
}

func TestCommitAndPushWithRetry_NonConflictErrorNotRetried(t *testing.T) {
	g, gitServer, sm, obj, policies, _ := newPushRetryFixture(t, time.Hour)
	ctx := context.TODO()

	result, err := policy.ApplyPolicies(ctx, sm.WorkDirectory(), obj, policies)
	g.Expect(err).ToNot(HaveOccurred())

	// Break connectivity so the push fails for a reason other than a
	// conflict (connection refused, not "someone else pushed first").
	gitServer.StopHTTP()

	r := &ImageUpdateAutomationReconciler{features: map[string]bool{features.GitPushRetryOnConflict: true}}
	start := time.Now()
	_, err = r.commitAndPushWithRetry(ctx, sm, obj, policies, result, nil)
	elapsed := time.Since(start)

	g.Expect(err).To(HaveOccurred())
	g.Expect(source.IsPushConflict(err)).To(BeFalse())
	// No backoff/refresh should have been attempted for a non-conflict error.
	g.Expect(elapsed).To(BeNumerically("<", pushRetryBaseDelay))
}

func TestCommitAndPushWithRetry_BudgetExceeded(t *testing.T) {
	// A very short interval collapses the retry budget below the first
	// backoff delay, so the loop must bail out via its own context timeout
	// rather than sleeping the full pushRetryBaseDelay.
	g, _, sm, obj, policies, repoURL := newPushRetryFixture(t, 1*time.Second)
	ctx := context.TODO()

	result, err := policy.ApplyPolicies(ctx, sm.WorkDirectory(), obj, policies)
	g.Expect(err).ToNot(HaveOccurred())

	pushCompetingCommit(g, repoURL, "main", "competitor-1.txt")

	r := &ImageUpdateAutomationReconciler{features: map[string]bool{features.GitPushRetryOnConflict: true}}
	start := time.Now()
	_, err = r.commitAndPushWithRetry(ctx, sm, obj, policies, result, nil)
	elapsed := time.Since(start)

	g.Expect(err).To(HaveOccurred())
	g.Expect(elapsed).To(BeNumerically("<", pushRetryBaseDelay))
}

func TestCommitAndPushWithRetry_FeatureGateDisabled(t *testing.T) {
	g, _, sm, obj, policies, repoURL := newPushRetryFixture(t, time.Hour)
	ctx := context.TODO()

	result, err := policy.ApplyPolicies(ctx, sm.WorkDirectory(), obj, policies)
	g.Expect(err).ToNot(HaveOccurred())

	pushCompetingCommit(g, repoURL, "main", "competitor-1.txt")

	r := &ImageUpdateAutomationReconciler{features: map[string]bool{features.GitPushRetryOnConflict: false}}
	_, err = r.commitAndPushWithRetry(ctx, sm, obj, policies, result, nil)
	g.Expect(err).To(HaveOccurred())
	g.Expect(source.IsPushConflict(err)).To(BeTrue())
}
