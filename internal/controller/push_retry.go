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
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	reflectorv1 "github.com/fluxcd/image-reflector-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1"
	"github.com/fluxcd/image-automation-controller/internal/features"
	"github.com/fluxcd/image-automation-controller/internal/policy"
	"github.com/fluxcd/image-automation-controller/internal/source"
	"github.com/fluxcd/image-automation-controller/internal/update"
)

// These are vars, not consts, so tests can shrink them for determinism and
// speed without changing the retry mechanics itself.
var (
	// pushRetryMaxAttempts mirrors Staffbase's gitops-github-action
	// retry_with_backoff(5, 2, push_to_gitops_repo) pattern: 5 attempts,
	// base delay 2s, doubling each retry.
	pushRetryMaxAttempts = 5
	// pushRetryBaseDelay is the initial backoff, doubled after each retry:
	// 2s, 4s, 8s, 16s.
	pushRetryBaseDelay = 2 * time.Second
	// maxPushRetryBudget bounds how long a single reconcile() call may block
	// retrying a push, independent of the object's configured interval, so
	// heavy contention on one branch cannot starve other ImageUpdateAutomation
	// objects sharing the same reconcile worker pool.
	maxPushRetryBudget = 2 * time.Minute
)

// pushRetryTestHook, when set, is called immediately before each attempt's
// CommitAndPush. It exists only so tests can deterministically land a
// competing commit inside the race window, instead of relying on wall-clock
// timing against a background pusher. Always nil in production.
var pushRetryTestHook func(attempt int)

// commitAndPushWithRetry wraps SourceManager.CommitAndPush with a
// fetch+reset+reapply retry loop, engaged only when the push is rejected
// because another writer already advanced the same branch
// (source.IsPushConflict). Any other error, or exhaustion of
// pushRetryMaxAttempts, is returned unchanged for the caller's existing
// error handling.
//
// On retry, only the cheap SourceManager.RefreshToRemote (fetch + hard
// reset) is used to catch the working directory up to the new remote tip;
// SourceManager.CheckoutSource's full clone is deliberately not repeated.
func (r *ImageUpdateAutomationReconciler) commitAndPushWithRetry(
	ctx context.Context,
	sm *source.SourceManager,
	obj *imagev1.ImageUpdateAutomation,
	policies []reflectorv1.ImagePolicy,
	policyResult update.Result,
	pushCfg []source.PushConfig,
) (*source.PushResult, error) {
	if !r.features[features.GitPushRetryOnConflict] {
		return sm.CommitAndPush(ctx, obj, policyResult, pushCfg...)
	}

	retryBudget := obj.GetRequeueAfter() / 2
	if retryBudget > maxPushRetryBudget {
		retryBudget = maxPushRetryBudget
	}
	retryCtx, cancel := context.WithTimeout(ctx, retryBudget)
	defer cancel()

	log := ctrl.LoggerFrom(ctx)

	for attempt := 1; attempt <= pushRetryMaxAttempts; attempt++ {
		if pushRetryTestHook != nil {
			pushRetryTestHook(attempt)
		}
		pushResult, err := sm.CommitAndPush(retryCtx, obj, policyResult, pushCfg...)
		if err == nil {
			if attempt > 1 {
				log.Info("push succeeded after retry", "attempts", attempt)
			}
			return pushResult, nil
		}
		if !source.IsPushConflict(err) || attempt == pushRetryMaxAttempts {
			return nil, err
		}

		delay := pushRetryBaseDelay * time.Duration(uint64(1)<<uint(attempt-1)) // 2s, 4s, 8s, 16s
		log.Info("push rejected, remote branch has moved; retrying after refresh",
			"attempt", attempt, "maxAttempts", pushRetryMaxAttempts, "delay", delay)

		select {
		case <-time.After(delay):
		case <-retryCtx.Done():
			return nil, fmt.Errorf("push retry budget exceeded after %d attempt(s): %w", attempt, retryCtx.Err())
		}

		if err := sm.RefreshToRemote(retryCtx); err != nil {
			return nil, fmt.Errorf("failed to refresh working tree after push conflict: %w", err)
		}

		policyResult, err = policy.ApplyPolicies(retryCtx, sm.WorkDirectory(), obj, policies)
		if err != nil {
			return nil, err
		}
		// If policyResult is now empty (a concurrent commit already achieved
		// the desired end state), the next loop iteration's CommitAndPush
		// returns (nil, nil) via its existing no-file-changes handling, which
		// the err == nil branch above already treats as success.
	}
	panic("unreachable: loop always returns before falling through")
}
