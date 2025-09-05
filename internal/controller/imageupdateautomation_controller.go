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

package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	kuberecorder "k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	reflectorv1 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	aclapi "github.com/fluxcd/pkg/apis/acl"
	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/auth"
	"github.com/fluxcd/pkg/cache"
	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/runtime/acl"
	"github.com/fluxcd/pkg/runtime/conditions"
	helper "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/fluxcd/pkg/runtime/predicates"
	runtimereconcile "github.com/fluxcd/pkg/runtime/reconcile"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta2"
	"github.com/fluxcd/image-automation-controller/internal/features"
	"github.com/fluxcd/image-automation-controller/internal/policy"
	"github.com/fluxcd/image-automation-controller/internal/source"
)

const repoRefKey = ".spec.gitRepository"

const readyMessage = "repository up-to-date"

// imageUpdateAutomationOwnedConditions is a list of conditions owned by the
// ImageUpdateAutomationReconciler.
var imageUpdateAutomationOwnedConditions = []string{
	meta.ReadyCondition,
	meta.ReconcilingCondition,
	meta.StalledCondition,
}

// imageUpdateAutomationNegativeConditions is a list of negative polarity
// conditions owned by ImageUpdateAutomationReconciler. It is used in tests for
// compliance with kstatus.
var imageUpdateAutomationNegativeConditions = []string{
	meta.StalledCondition,
	meta.ReconcilingCondition,
}

var errParsePolicySelector = errors.New("failed to parse policy selector")

// getPatchOptions composes patch options based on the given parameters.
// It is used as the options used when patching an object.
func getPatchOptions(ownedConditions []string, controllerName string) []patch.Option {
	return []patch.Option{
		patch.WithOwnedConditions{Conditions: ownedConditions},
		patch.WithFieldOwner(controllerName),
	}
}

// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imageupdateautomations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imageupdateautomations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imagepolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imagepolicies/status,verbs=get
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// ImageUpdateAutomationReconciler reconciles a ImageUpdateAutomation object
type ImageUpdateAutomationReconciler struct {
	client.Client
	kuberecorder.EventRecorder
	helper.Metrics

	ControllerName      string
	NoCrossNamespaceRef bool

	features map[string]bool

	patchOptions []patch.Option

	tokenCache *cache.TokenCache
}

type ImageUpdateAutomationReconcilerOptions struct {
	MaxConcurrentReconciles int
	RateLimiter             workqueue.TypedRateLimiter[reconcile.Request]
	RecoverPanic            bool
	TokenCache              *cache.TokenCache
}

func (r *ImageUpdateAutomationReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, opts ImageUpdateAutomationReconcilerOptions) error {
	r.patchOptions = getPatchOptions(imageUpdateAutomationOwnedConditions, r.ControllerName)

	if r.features == nil {
		r.features = features.FeatureGates()
	}

	r.tokenCache = opts.TokenCache

	// Index the git repository object that each I-U-A refers to
	if err := mgr.GetFieldIndexer().IndexField(ctx, &imagev1.ImageUpdateAutomation{}, repoRefKey, func(obj client.Object) []string {
		updater := obj.(*imagev1.ImageUpdateAutomation)
		ref := updater.Spec.SourceRef
		ns := ref.Namespace
		if ns == "" {
			ns = obj.GetNamespace()
		}
		return []string{fmt.Sprintf("%s/%s", ns, ref.Name)}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&imagev1.ImageUpdateAutomation{}, builder.WithPredicates(
			predicate.Or(predicate.GenerationChangedPredicate{}, predicates.ReconcileRequestedPredicate{}))).
		Watches(
			&sourcev1.GitRepository{},
			handler.EnqueueRequestsFromMapFunc(r.automationsForGitRepo),
			builder.WithPredicates(sourceConfigChangePredicate{}),
		).
		Watches(
			&reflectorv1.ImagePolicy{},
			handler.EnqueueRequestsFromMapFunc(r.automationsForImagePolicy),
			builder.WithPredicates(latestImageChangePredicate{}),
		).
		WithOptions(controller.Options{
			RateLimiter: opts.RateLimiter,
		}).
		Complete(r)
}

// automationsForGitRepo fetches all the automations that refer to a
// particular source.GitRepository object.
func (r *ImageUpdateAutomationReconciler) automationsForGitRepo(ctx context.Context, obj client.Object) []reconcile.Request {
	var autoList imagev1.ImageUpdateAutomationList
	objKey := fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName())
	if err := r.List(ctx, &autoList, client.MatchingFields{repoRefKey: objKey}); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "failed to list ImageUpdateAutomations for GitRepository change")
		return nil
	}
	reqs := make([]reconcile.Request, len(autoList.Items))
	for i := range autoList.Items {
		reqs[i].NamespacedName.Name = autoList.Items[i].GetName()
		reqs[i].NamespacedName.Namespace = autoList.Items[i].GetNamespace()
	}
	return reqs
}

// automationsForImagePolicy fetches all the automation objects that
// might depend on a image policy object. Since the link is via
// markers in the git repo, _any_ automation object in the same
// namespace could be affected.
func (r *ImageUpdateAutomationReconciler) automationsForImagePolicy(ctx context.Context, obj client.Object) []reconcile.Request {
	var autoList imagev1.ImageUpdateAutomationList
	if err := r.List(ctx, &autoList, client.InNamespace(obj.GetNamespace())); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "failed to list ImageUpdateAutomations for ImagePolicy change")
		return nil
	}
	reqs := make([]reconcile.Request, len(autoList.Items))
	for i := range autoList.Items {
		reqs[i].NamespacedName.Name = autoList.Items[i].GetName()
		reqs[i].NamespacedName.Namespace = autoList.Items[i].GetNamespace()
	}
	return reqs
}

func (r *ImageUpdateAutomationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	start := time.Now()
	log := ctrl.LoggerFrom(ctx)

	// Fetch the ImageUpdateAutomation.
	obj := &imagev1.ImageUpdateAutomation{}
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Initialize the patch helper with the current version of the object.
	serialPatcher := patch.NewSerialPatcher(obj, r.Client)

	// Always attempt to patch the object after each reconciliation.
	defer func() {
		// Create patch options for the final patch of the object.
		patchOpts := runtimereconcile.AddPatchOptions(obj, r.patchOptions, imageUpdateAutomationOwnedConditions, r.ControllerName)
		if err := serialPatcher.Patch(ctx, obj, patchOpts...); err != nil {
			// Ignore patch error "not found" when the object is being deleted.
			if !obj.GetDeletionTimestamp().IsZero() {
				err = kerrors.FilterOut(err, func(e error) bool { return apierrors.IsNotFound(e) })
			}
			retErr = kerrors.NewAggregate([]error{retErr, err})
		}

		// When the reconciliation ends with an error, ensure that the Result is
		// empty. This is to suppress the runtime warning when returning a
		// non-zero Result and an error.
		if retErr != nil {
			result = ctrl.Result{}
		}

		// Always record suspend, readiness and duration metrics.
		r.Metrics.RecordDuration(ctx, obj, start)
	}()

	// Examine if the object is under deletion.
	if !obj.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(obj)
	}

	// Add finalizer first if it doesn't exist to avoid the race condition
	// between init and delete.
	// Note: Finalizers in general can only be added when the deletionTimestamp
	// is not set.
	if !controllerutil.ContainsFinalizer(obj, imagev1.ImageUpdateAutomationFinalizer) {
		controllerutil.AddFinalizer(obj, imagev1.ImageUpdateAutomationFinalizer)
		return ctrl.Result{Requeue: true}, nil
	}

	// Return if the object is suspended.
	if obj.Spec.Suspend {
		log.Info("reconciliation is suspended for this object")
		return ctrl.Result{}, nil
	}

	result, retErr = r.reconcile(ctx, serialPatcher, obj, start)
	return
}

func (r *ImageUpdateAutomationReconciler) reconcile(ctx context.Context, sp *patch.SerialPatcher,
	obj *imagev1.ImageUpdateAutomation, startTime time.Time) (result ctrl.Result, retErr error) {
	oldObj := obj.DeepCopy()

	var pushResult *source.PushResult

	// syncNeeded decides if full reconciliation with image update is needed.
	syncNeeded := false

	defer func() {
		// Define the meaning of success based on the requeue interval.
		isSuccess := func(res ctrl.Result, err error) bool {
			if err != nil || res.RequeueAfter != obj.GetRequeueAfter() || res.Requeue {
				return false
			}
			return true
		}

		rs := runtimereconcile.NewResultFinalizer(isSuccess, readyMessage)
		retErr = rs.Finalize(obj, result, retErr)

		// Presence of reconciling means that the reconciliation didn't succeed.
		// Set the Reconciling reason to ProgressingWithRetry to indicate a
		// failure retry.
		if conditions.IsReconciling(obj) {
			reconciling := conditions.Get(obj, meta.ReconcilingCondition)
			reconciling.Reason = meta.ProgressingWithRetryReason
			conditions.Set(obj, reconciling)
		}

		r.notify(ctx, oldObj, obj, pushResult, syncNeeded)
	}()

	// TODO: Maybe move this to Reconcile()'s defer and avoid passing startTime
	// to reconcile()?
	obj.Status.LastAutomationRunTime = &metav1.Time{Time: startTime}

	// Set reconciling condition.
	runtimereconcile.ProgressiveStatus(false, obj, meta.ProgressingReason, "reconciliation in progress")

	var reconcileAtVal string
	if v, ok := meta.ReconcileAnnotationValue(obj.GetAnnotations()); ok {
		reconcileAtVal = v
	}

	// Persist reconciling if generation differs or reconciliation is requested.
	switch {
	case obj.Generation != obj.Status.ObservedGeneration:
		runtimereconcile.ProgressiveStatus(false, obj, meta.ProgressingReason,
			"processing object: new generation %d -> %d", obj.Status.ObservedGeneration, obj.Generation)
		if err := sp.Patch(ctx, obj, r.patchOptions...); err != nil {
			result, retErr = ctrl.Result{}, err
			return
		}
	case reconcileAtVal != obj.Status.GetLastHandledReconcileRequest():
		if err := sp.Patch(ctx, obj, r.patchOptions...); err != nil {
			result, retErr = ctrl.Result{}, err
			return
		}
	}

	// List the policies and construct observed policies.
	policies, err := getPolicies(ctx, r.Client, obj.Namespace, obj.Spec.PolicySelector)
	if err != nil {
		if errors.Is(err, errParsePolicySelector) {
			conditions.MarkStalled(obj, imagev1.InvalidPolicySelectorReason, "%s", err)
			result, retErr = ctrl.Result{}, nil
			return
		}
		result, retErr = ctrl.Result{}, err
		return
	}
	// Update any stale Ready=False condition from policies config failure.
	if conditions.HasAnyReason(obj, meta.ReadyCondition, imagev1.InvalidPolicySelectorReason) {
		conditions.MarkUnknown(obj, meta.ReadyCondition, meta.ProgressingReason, "reconciliation in progress")
	}

	// Index the policies by their name.
	observedPolicies := imagev1.ObservedPolicies{}
	for _, policy := range policies {
		observedPolicies[policy.Name] = imagev1.ImageRef{
			Name:   policy.Status.LatestRef.Name,
			Tag:    policy.Status.LatestRef.Tag,
			Digest: policy.Status.LatestRef.Digest,
		}
	}

	// If the policies have changed, require a full sync.
	if observedPoliciesChanged(obj.Status.ObservedPolicies, observedPolicies) {
		syncNeeded = true
	}

	// Create source manager with options.
	smOpts := []source.SourceOption{
		source.WithSourceOptionInvolvedObject(obj.GetName(), obj.GetNamespace()),
		source.WithSourceOptionTokenCache(r.tokenCache),
	}
	if r.NoCrossNamespaceRef {
		smOpts = append(smOpts, source.WithSourceOptionNoCrossNamespaceRef())
	}
	if r.features[features.GitAllBranchReferences] {
		smOpts = append(smOpts, source.WithSourceOptionGitAllBranchReferences())
	}
	sm, err := source.NewSourceManager(ctx, r.Client, obj, smOpts...)
	if err != nil {
		if acl.IsAccessDenied(err) {
			conditions.MarkStalled(obj, aclapi.AccessDeniedReason, "%s", err)
			result, retErr = ctrl.Result{}, nil
			return
		}
		if errors.Is(err, source.ErrInvalidSourceConfiguration) {
			conditions.MarkStalled(obj, imagev1.InvalidSourceConfigReason, "%s", err)
			result, retErr = ctrl.Result{}, nil
			return
		}
		if errors.Is(err, source.ErrFeatureGateNotEnabled) {
			const gate = auth.FeatureGateObjectLevelWorkloadIdentity
			const msgFmt = "to use spec.serviceAccountName for provider authentication please enable the %s feature gate in the controller"
			conditions.MarkStalled(obj, meta.FeatureGateDisabledReason, msgFmt, gate)
			result, retErr = ctrl.Result{}, nil
			return
		}
		e := fmt.Errorf("failed configuring source manager: %w", err)
		conditions.MarkFalse(obj, meta.ReadyCondition, imagev1.SourceManagerFailedReason, "%s", e)
		result, retErr = ctrl.Result{}, e
		return
	}
	defer func() {
		if err := sm.Cleanup(); err != nil {
			retErr = err
		}
	}()
	// Update any stale Ready=False condition from SourceManager failure.
	if conditions.HasAnyReason(obj, meta.ReadyCondition, aclapi.AccessDeniedCondition, imagev1.InvalidSourceConfigReason, imagev1.SourceManagerFailedReason, meta.FeatureGateDisabledReason) {
		conditions.MarkUnknown(obj, meta.ReadyCondition, meta.ProgressingReason, "reconciliation in progress")
	}

	// When the checkout and push branches are different or a refspec is
	// defined, always perform a full sync.
	// This can be worked around in the future by also querying the HEAD of push
	// branch to detech if it has drifted.
	if sm.SwitchBranch() || obj.Spec.GitSpec.HasRefspec() {
		syncNeeded = true
	}

	// Build checkout options.
	checkoutOpts := []source.CheckoutOption{}
	if r.features[features.GitShallowClone] {
		checkoutOpts = append(checkoutOpts, source.WithCheckoutOptionShallowClone())
	}
	if r.features[features.GitSparseCheckout] && obj.Spec.Update.Path != "" {
		checkoutOpts = append(checkoutOpts, source.WithCheckoutOptionSparseCheckoutDirectories(obj.Spec.Update.Path))
	}

	// If full sync is still not needed, configure last observed commit to
	// perform optimized clone and obtain a non-concrete commit if the remote
	// has not changed.
	if !syncNeeded && obj.Status.ObservedSourceRevision != "" {
		checkoutOpts = append(checkoutOpts, source.WithCheckoutOptionLastObserved(obj.Status.ObservedSourceRevision))
	}

	commit, err := sm.CheckoutSource(ctx, checkoutOpts...)
	if err != nil {
		e := fmt.Errorf("failed to checkout source: %w", err)
		conditions.MarkFalse(obj, meta.ReadyCondition, imagev1.GitOperationFailedReason, "%s", e)
		result, retErr = ctrl.Result{}, e
		return
	}
	// Update any stale Ready=False condition from checkout failure.
	if conditions.HasAnyReason(obj, meta.ReadyCondition, imagev1.GitOperationFailedReason) {
		conditions.MarkUnknown(obj, meta.ReadyCondition, meta.ProgressingReason, "reconciliation in progress")
	}

	// If it's a partial commit, the reconciliation can be skipped. The last
	// observed commit is only configured above when full sync is not needed.
	// No change in the policies and remote git repository. Skip reconciliation.
	if !git.IsConcreteCommit(*commit) {
		// Remove any stale Ready condition, most likely False, set above. Its value
		// is derived from the overall result of the reconciliation in the deferred
		// block at the very end.
		conditions.Delete(obj, meta.ReadyCondition)
		result, retErr = ctrl.Result{RequeueAfter: obj.GetRequeueAfter()}, nil
		return
	} else {
		// Concrete commit indicates full sync is needed due to new remote
		// revision.
		syncNeeded = true
	}
	// Continue with full sync with a concrete commit.

	// Apply the policies and check if there's anything to update.
	policyResult, err := policy.ApplyPolicies(ctx, sm.WorkDirectory(), obj, policies)
	if err != nil {
		if errors.Is(err, policy.ErrNoUpdateStrategy) || errors.Is(err, policy.ErrUnsupportedUpdateStrategy) {
			conditions.MarkStalled(obj, imagev1.InvalidUpdateStrategyReason, "%s", err)
			result, retErr = ctrl.Result{}, nil
			return
		}
		e := fmt.Errorf("failed to apply policies: %w", err)
		conditions.MarkFalse(obj, meta.ReadyCondition, imagev1.UpdateFailedReason, "%s", e)
		result, retErr = ctrl.Result{}, e
		return
	}
	// Update any stale Ready=False condition from apply policies failure.
	if conditions.HasAnyReason(obj, meta.ReadyCondition, imagev1.InvalidUpdateStrategyReason, imagev1.UpdateFailedReason) {
		conditions.MarkUnknown(obj, meta.ReadyCondition, meta.ProgressingReason, "reconciliation in progress")
	}

	if len(policyResult.FileChanges) == 0 {
		// Remove any stale Ready condition, most likely False, set above. Its
		// value is derived from the overall result of the reconciliation in the
		// deferred block at the very end.
		conditions.Delete(obj, meta.ReadyCondition)

		// Persist observations.
		obj.Status.ObservedSourceRevision = commit.String()
		obj.Status.ObservedPolicies = observedPolicies

		result, retErr = ctrl.Result{RequeueAfter: obj.GetRequeueAfter()}, nil
		return
	}

	// Build push config.
	pushCfg := []source.PushConfig{}
	// Enable force only when branch is changed for push.
	if r.features[features.GitForcePushBranch] && sm.SwitchBranch() {
		pushCfg = append(pushCfg, source.WithPushConfigForce())
	}
	// Include any push options.
	if obj.Spec.GitSpec.Push != nil && obj.Spec.GitSpec.Push.Options != nil {
		pushCfg = append(pushCfg, source.WithPushConfigOptions(obj.Spec.GitSpec.Push.Options))
	}

	pushResult, err = sm.CommitAndPush(ctx, obj, policyResult, pushCfg...)
	if err != nil {
		// Check if error is due to removed template field usage.
		// Set Stalled condition and return nil error to prevent requeue, allowing user to fix template.
		if errors.Is(err, source.ErrRemovedTemplateField) {
			conditions.MarkStalled(obj, imagev1.RemovedTemplateFieldReason, "%s", err)
			result, retErr = ctrl.Result{}, nil
			return
		}

		e := fmt.Errorf("failed to update source: %w", err)
		conditions.MarkFalse(obj, meta.ReadyCondition, imagev1.GitOperationFailedReason, "%s", e)
		result, retErr = ctrl.Result{}, e
		return
	}
	// Update any stale Ready=False condition from commit and push failure.
	if conditions.HasAnyReason(obj, meta.ReadyCondition, imagev1.GitOperationFailedReason) {
		conditions.MarkUnknown(obj, meta.ReadyCondition, meta.ProgressingReason, "reconciliation in progress")
	}

	if pushResult == nil {
		// NOTE: This should not happen. This exists as a legacy behavior from
		// the old implementation where no commit is made due to no stagged
		// files. If nothing is pushed, the repository is up-to-date. Persist
		// observations and return with successful result.
		conditions.Delete(obj, meta.ReadyCondition)
		obj.Status.ObservedSourceRevision = commit.String()
		obj.Status.ObservedPolicies = observedPolicies
		result, retErr = ctrl.Result{RequeueAfter: obj.GetRequeueAfter()}, nil
		return
	}

	// Persist observations.
	obj.Status.ObservedSourceRevision = pushResult.Commit().String()
	// If the push branch is different, store the checkout branch commit as the
	// observed source revision.
	if pushResult.SwitchBranch() {
		obj.Status.ObservedSourceRevision = commit.String()
	}
	obj.Status.ObservedPolicies = observedPolicies
	obj.Status.LastPushCommit = pushResult.Commit().Hash.String()
	obj.Status.LastPushTime = pushResult.Time()

	// Remove any stale Ready condition, most likely False, set above. Its value
	// is derived from the overall result of the reconciliation in the deferred
	// block at the very end.
	conditions.Delete(obj, meta.ReadyCondition)
	result, retErr = ctrl.Result{RequeueAfter: obj.GetRequeueAfter()}, nil
	return
}

// reconcileDelete handles the deletion of the object.
func (r *ImageUpdateAutomationReconciler) reconcileDelete(obj *imagev1.ImageUpdateAutomation) (ctrl.Result, error) {
	// Remove our finalizer from the list.
	controllerutil.RemoveFinalizer(obj, imagev1.ImageUpdateAutomationFinalizer)

	// Cleanup caches.
	r.tokenCache.DeleteEventsForObject(imagev1.ImageUpdateAutomationKind,
		obj.GetName(), obj.GetNamespace(), cache.OperationReconcile)

	// Stop reconciliation as the object is being deleted.
	return ctrl.Result{}, nil
}

// getPolicies returns list of policies in the given namespace that have latest
// image.
func getPolicies(ctx context.Context, kclient client.Client, namespace string, selector *metav1.LabelSelector) ([]reflectorv1.ImagePolicy, error) {
	policySelector := labels.Everything()
	var err error
	if selector != nil {
		if policySelector, err = metav1.LabelSelectorAsSelector(selector); err != nil {
			return nil, fmt.Errorf("%w: %w", errParsePolicySelector, err)
		}
	}

	var policies reflectorv1.ImagePolicyList
	if err := kclient.List(ctx, &policies, &client.ListOptions{Namespace: namespace, LabelSelector: policySelector}); err != nil {
		return nil, fmt.Errorf("failed to list policies: %w", err)
	}

	readyPolicies := []reflectorv1.ImagePolicy{}
	for _, policy := range policies.Items {
		// Ignore the policies that don't have a latest image.
		if policy.Status.LatestRef == nil {
			continue
		}
		readyPolicies = append(readyPolicies, policy)
	}

	return readyPolicies, nil
}

// observedPoliciesChanged returns if the previous and current observedPolicies
// have changed.
func observedPoliciesChanged(previous, current imagev1.ObservedPolicies) bool {
	if len(previous) != len(current) {
		return true
	}
	for name, imageRef := range current {
		oldImageRef, ok := previous[name]
		if !ok {
			// Changed if an entry is not found.
			return true
		}
		if oldImageRef != imageRef {
			return true
		}
	}
	return false
}

// notify emits notifications and events based on the state of the object and
// the given PushResult. It tries to always send the PushResult commit message
// if there has been any update. Otherwise, a generic up-to-date message. In
// case of any failure, the failure message is read from the Ready condition and
// included in the event.
func (r *ImageUpdateAutomationReconciler) notify(ctx context.Context, oldObj, newObj conditions.Setter, result *source.PushResult, syncNeeded bool) {
	// Use the Ready message as the notification message by default.
	ready := conditions.Get(newObj, meta.ReadyCondition)
	msg := ready.Message

	// If there's a PushResult, use the summary as the notification message.
	if result != nil {
		msg = result.Summary()
	}

	// Was ready before and is ready now, with new push result,
	if conditions.IsReady(oldObj) && conditions.IsReady(newObj) && result != nil {
		eventLogf(ctx, r.EventRecorder, newObj, corev1.EventTypeNormal, ready.Reason, "%s", msg)
		return
	}

	// Emit events when reconciliation fails or recovers from failure.

	// Became ready from not ready.
	if !conditions.IsReady(oldObj) && conditions.IsReady(newObj) {
		eventLogf(ctx, r.EventRecorder, newObj, corev1.EventTypeNormal, ready.Reason, "%s", msg)
		return
	}
	// Not ready, failed. Use the failure message from ready condition.
	if !conditions.IsReady(newObj) {
		eventLogf(ctx, r.EventRecorder, newObj, corev1.EventTypeWarning, ready.Reason, "%s", ready.Message)
		return
	}

	// No change.

	if !syncNeeded {
		// Full reconciliation skipped.
		msg = "no change since last reconciliation"
	}
	eventLogf(ctx, r.EventRecorder, newObj, eventv1.EventTypeTrace, meta.SucceededReason, "%s", msg)
}

// eventLogf records events, and logs at the same time.
//
// This log is different from the debug log in the EventRecorder, in the sense
// that this is a simple log. While the debug log contains complete details
// about the event.
func eventLogf(ctx context.Context, r kuberecorder.EventRecorder, obj runtime.Object, eventType string, reason string, messageFmt string, args ...interface{}) {
	msg := fmt.Sprintf(messageFmt, args...)
	// Log and emit event.
	if eventType == corev1.EventTypeWarning {
		ctrl.LoggerFrom(ctx).Error(errors.New(reason), msg)
	} else {
		ctrl.LoggerFrom(ctx).Info(msg)
	}
	r.Eventf(obj, eventType, reason, msg)
}
