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
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"

	reflectorv1 "github.com/fluxcd/image-reflector-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1"
	"github.com/fluxcd/image-automation-controller/internal/features"
	"github.com/fluxcd/image-automation-controller/internal/policy"
	"github.com/fluxcd/image-automation-controller/internal/source"
	"github.com/fluxcd/image-automation-controller/internal/update"
)

const (
	// pushRetryMaxAttempts is the maximum number of push attempts, including
	// the first one.
	pushRetryMaxAttempts = 5
	// pushRetryJitter is the maximum fraction of the delay added to it, so
	// that writers whose pushes failed at the same time retry at different
	// times.
	pushRetryJitter = 0.5
	// pushRetryMaxBudget caps how long after the first failed push a retry
	// can be started.
	pushRetryMaxBudget = 2 * time.Minute
)

// pushRetryBaseDelay is the delay before the first retry, doubled for each
// further retry. It is a variable so that tests can shorten it.
var pushRetryBaseDelay = 2 * time.Second

// commitAndPushWithRetry commits and pushes the changes in policyResult. With
// the GitPushRetryOnFailure feature gate enabled, it retries when pushing to
// the push branch fails: it waits, clones the source again to catch up with
// the remote, applies the policies again, and commits and pushes the result.
//
// Any failure to push to the push branch is retried, as Git servers report a
// push that lost a race against another writer in too many ways to tell it
// apart reliably. The exceptions are authentication and authorization
// failures and a missing repository, as the source was just cloned with the
// same credentials and retrying can't succeed. Other errors, such as failing
// to render the commit message or to push to the push refspec, are returned
// without retrying.
//
// A failed push can still have reached the remote, for example when it timed
// out after the remote accepted it. If the push branch is at the commit of
// the failed push after catching up, the commit is pushed to the push
// refspec, if any, and its result is returned without recomputing the
// changes.
//
// Retrying stops when the working directory can't be caught up with the
// remote, after pushRetryMaxAttempts attempts, or at min(interval/2,
// pushRetryMaxBudget) after the first attempt failed, cancelling any clone or
// push in progress, so that a contended branch or an unresponsive Git server
// can't hold up a reconcile worker for long.
func (r *ImageUpdateAutomationReconciler) commitAndPushWithRetry(ctx context.Context, sm *source.SourceManager,
	obj *imagev1.ImageUpdateAutomation, policies []reflectorv1.ImagePolicy, policyResult update.Result,
	pushCfg []source.PushConfig) (*source.PushResult, error) {
	pushResult, err := sm.CommitAndPush(ctx, obj, policyResult, pushCfg...)
	if err == nil || !r.features[features.GitPushRetryOnFailure] {
		return pushResult, err
	}

	log := ctrl.LoggerFrom(ctx)
	deadline := time.Now().Add(min(obj.GetRequeueAfter()/2, pushRetryMaxBudget))
	retryCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	delay := pushRetryBaseDelay
	for attempt := 1; ; attempt++ {
		var pushErr *source.PushBranchError
		if !errors.As(err, &pushErr) || isPermanentPushError(err) {
			return nil, err
		}
		jitteredDelay := wait.Jitter(delay, pushRetryJitter)
		if attempt == pushRetryMaxAttempts || time.Now().Add(jitteredDelay).After(deadline) {
			if attempt > 1 {
				err = fmt.Errorf("giving up after %d attempts: %w", attempt, err)
			}
			return nil, err
		}
		delay *= 2

		log.Info("push failed, retrying", "attempt", attempt, "delay", jitteredDelay.String(), "error", err.Error())
		select {
		case <-retryCtx.Done():
			return nil, err
		case <-time.After(jitteredDelay):
		}

		if refreshErr := sm.RefreshToRemote(retryCtx); refreshErr != nil {
			return nil, fmt.Errorf("%w; failed to refresh the source to retry the push: %w", err, refreshErr)
		}
		var pushed bool
		pushResult, pushed, err = sm.CompletePush(retryCtx, pushErr, pushCfg...)
		if err != nil {
			return nil, err
		}
		if pushed {
			log.Info("the failed push reached the remote", "attempts", attempt)
			return pushResult, nil
		}
		policyResult, err = policy.ApplyPolicies(retryCtx, sm.WorkDirectory(), obj, policies)
		if err != nil {
			return nil, fmt.Errorf("failed to apply policies to retry the push: %w", err)
		}
		pushResult, err = sm.CommitAndPush(retryCtx, obj, policyResult, pushCfg...)
		if err == nil {
			if pushResult == nil {
				// The failed push may have reached the remote before another
				// writer pushed on top of it. It can't be told apart from
				// another writer pushing the same changes, as the clone is
				// shallow, so it's neither recorded nor pushed to the refspec.
				log.Info("the remote already has the changes, nothing left to push", "attempts", attempt)
			} else {
				log.Info("push succeeded after retrying", "attempts", attempt+1)
			}
			return pushResult, nil
		}
	}
}

// isPermanentPushError reports whether err means that the remote denied
// access to the repository or can't find it. Retrying such a push can't
// succeed. Only the HTTP(S) transport reports these errors in a way that can
// be told apart.
func isPermanentPushError(err error) bool {
	return errors.Is(err, transport.ErrAuthenticationRequired) ||
		errors.Is(err, transport.ErrAuthorizationFailed) ||
		errors.Is(err, transport.ErrRepositoryNotFound)
}
