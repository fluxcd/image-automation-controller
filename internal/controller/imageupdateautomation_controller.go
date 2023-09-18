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
	"net/url"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/ProtonMail/go-crypto/openpgp"
	securejoin "github.com/cyphar/filepath-securejoin"
	extgogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kuberecorder "k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/ratelimiter"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	apiacl "github.com/fluxcd/pkg/apis/acl"
	eventv1 "github.com/fluxcd/pkg/apis/event/v1beta1"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/gogit"
	"github.com/fluxcd/pkg/git/repository"
	"github.com/fluxcd/pkg/runtime/acl"
	helper "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/fluxcd/pkg/runtime/predicates"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta1"
	"github.com/fluxcd/image-automation-controller/internal/features"
	"github.com/fluxcd/image-automation-controller/pkg/update"
)

const (
	originRemote           = "origin"
	defaultMessageTemplate = `Update from image update automation`
	repoRefKey             = ".spec.gitRepository"
	signingSecretKey       = "git.asc"
	signingPassphraseKey   = "passphrase"
)

// TemplateData is the type of the value given to the commit message
// template.
type TemplateData struct {
	AutomationObject types.NamespacedName
	Updated          update.Result
}

// ImageUpdateAutomationReconciler reconciles a ImageUpdateAutomation object
type ImageUpdateAutomationReconciler struct {
	client.Client
	EventRecorder kuberecorder.EventRecorder
	helper.Metrics

	NoCrossNamespaceRef bool

	features map[string]bool
}

type ImageUpdateAutomationReconcilerOptions struct {
	RateLimiter ratelimiter.RateLimiter
}

// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imageupdateautomations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imageupdateautomations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch

func (r *ImageUpdateAutomationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	debuglog := log.V(logger.DebugLevel)
	tracelog := log.V(logger.TraceLevel)
	start := time.Now()
	var templateValues TemplateData

	var auto imagev1.ImageUpdateAutomation
	if err := r.Get(ctx, req.NamespacedName, &auto); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	defer func() {
		// Always record suspend, readiness and duration metrics.
		r.Metrics.RecordSuspend(ctx, &auto, auto.Spec.Suspend)
		r.Metrics.RecordReadiness(ctx, &auto)
		r.Metrics.RecordDuration(ctx, &auto, start)
	}()

	// If the object is under deletion, record the readiness, and remove our finalizer.
	if !auto.ObjectMeta.DeletionTimestamp.IsZero() {
		controllerutil.RemoveFinalizer(&auto, imagev1.ImageUpdateAutomationFinalizer)
		if err := r.Update(ctx, &auto); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Add our finalizer if it does not exist.
	// Note: Finalizers in general can only be added when the deletionTimestamp
	// is not set.
	if !controllerutil.ContainsFinalizer(&auto, imagev1.ImageUpdateAutomationFinalizer) {
		patch := client.MergeFrom(auto.DeepCopy())
		controllerutil.AddFinalizer(&auto, imagev1.ImageUpdateAutomationFinalizer)
		if err := r.Patch(ctx, &auto, patch); err != nil {
			log.Error(err, "unable to register finalizer")
			return ctrl.Result{}, err
		}
	}

	if auto.Spec.Suspend {
		log.Info("ImageUpdateAutomation is suspended, skipping automation run")
		return ctrl.Result{}, nil
	}

	templateValues.AutomationObject = req.NamespacedName

	// whatever else happens, we've now "seen" the reconcile
	// annotation if it's there
	if token, ok := meta.ReconcileAnnotationValue(auto.GetAnnotations()); ok {
		auto.Status.SetLastHandledReconcileRequest(token)

		if err := r.patchStatus(ctx, req, auto.Status); err != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	// failWithError is a helper for bailing on the reconciliation.
	failWithError := func(err error) (ctrl.Result, error) {
		r.event(ctx, auto, eventv1.EventSeverityError, err.Error())
		imagev1.SetImageUpdateAutomationReadiness(&auto, metav1.ConditionFalse, imagev1.ReconciliationFailedReason, err.Error())
		if err := r.patchStatus(ctx, req, auto.Status); err != nil {
			log.Error(err, "failed to reconcile")
		}
		return ctrl.Result{Requeue: true}, err
	}

	// get the git repository object so it can be checked out

	// only GitRepository objects are supported for now
	if kind := auto.Spec.SourceRef.Kind; kind != sourcev1.GitRepositoryKind {
		return failWithError(fmt.Errorf("source kind '%s' not supported", kind))
	}

	gitSpec := auto.Spec.GitSpec
	if gitSpec == nil {
		return failWithError(fmt.Errorf("source kind %s neccessitates field .spec.git", sourcev1.GitRepositoryKind))
	}

	var origin sourcev1.GitRepository
	gitRepoNamespace := req.Namespace
	if auto.Spec.SourceRef.Namespace != "" {
		gitRepoNamespace = auto.Spec.SourceRef.Namespace
	}
	originName := types.NamespacedName{
		Name:      auto.Spec.SourceRef.Name,
		Namespace: gitRepoNamespace,
	}
	debuglog.Info("fetching git repository", "gitrepository", originName)

	if r.NoCrossNamespaceRef && gitRepoNamespace != auto.GetNamespace() {
		err := acl.AccessDeniedError(fmt.Sprintf("can't access '%s/%s', cross-namespace references have been blocked",
			auto.Spec.SourceRef.Kind, originName))
		log.Error(err, "access denied to cross-namespaced resource")
		imagev1.SetImageUpdateAutomationReadiness(&auto, metav1.ConditionFalse, apiacl.AccessDeniedReason,
			err.Error())
		if err := r.patchStatus(ctx, req, auto.Status); err != nil {
			return ctrl.Result{Requeue: true}, err
		}
		r.event(ctx, auto, eventv1.EventSeverityError, err.Error())
		return ctrl.Result{}, nil
	}

	if err := r.Get(ctx, originName, &origin); err != nil {
		if client.IgnoreNotFound(err) == nil {
			imagev1.SetImageUpdateAutomationReadiness(&auto, metav1.ConditionFalse, imagev1.GitNotAvailableReason, "referenced git repository is missing")
			log.Error(err, fmt.Sprintf("referenced git repository %s does not exist.", originName.String()))
			if err := r.patchStatus(ctx, req, auto.Status); err != nil {
				return ctrl.Result{Requeue: true}, err
			}
			return ctrl.Result{}, nil // and assume we'll hear about it when it arrives
		}
		return ctrl.Result{}, err
	}

	// validate the git spec and default any values needed later, before proceeding
	var checkoutRef *sourcev1.GitRepositoryRef
	if gitSpec.Checkout != nil {
		checkoutRef = &gitSpec.Checkout.Reference
		tracelog.Info("using git repository ref from .spec.git.checkout", "ref", checkoutRef)
	} else if r := origin.Spec.Reference; r != nil {
		checkoutRef = r
		tracelog.Info("using git repository ref from GitRepository spec", "ref", checkoutRef)
	} // else remain as `nil` and git.DefaultBranch will be used.

	tmp, err := os.MkdirTemp("", fmt.Sprintf("%s-%s", originName.Namespace, originName.Name))
	if err != nil {
		return failWithError(err)
	}
	defer func() {
		if err := os.RemoveAll(tmp); err != nil {
			log.Error(err, "failed to remove working directory", "path", tmp)
		}
	}()

	// pushBranch contains the branch name the commit needs to be pushed to.
	// It takes the value of the push branch if one is specified or, if the push
	// config is nil, then it takes the value of the checkout branch if possible.
	var pushBranch string
	var switchBranch bool
	if gitSpec.Push != nil && gitSpec.Push.Branch != "" {
		pushBranch = gitSpec.Push.Branch
		tracelog.Info("using push branch from .spec.push.branch", "branch", pushBranch)
		// We only need to switch branches when a branch has been specified in
		// the push spec and it is different than the one in the checkout ref.
		if gitSpec.Push.Branch != checkoutRef.Branch {
			switchBranch = true
		}
	} else {
		// Here's where it gets constrained. If there's no push branch
		// given, then the checkout ref must include a branch, and
		// that can be used.
		if checkoutRef == nil || checkoutRef.Branch == "" {
			return failWithError(
				fmt.Errorf("Push spec not provided, and cannot be inferred from .spec.git.checkout.ref or GitRepository .spec.ref"),
			)
		}
		pushBranch = checkoutRef.Branch
		tracelog.Info("using push branch from $ref.branch", "branch", pushBranch)
	}

	authOpts, err := r.getAuthOpts(ctx, &origin)
	if err != nil {
		return failWithError(err)
	}
	var proxyOpts *transport.ProxyOptions
	if origin.Spec.ProxySecretRef != nil {
		proxyOpts, err = r.getProxyOpts(ctx, origin.Spec.ProxySecretRef.Name, origin.GetNamespace())
		if err != nil {
			return failWithError(err)
		}
	}

	clientOpts := r.getGitClientOpts(authOpts.Transport, proxyOpts, switchBranch)
	gitClient, err := gogit.NewClient(tmp, authOpts, clientOpts...)
	if err != nil {
		return failWithError(err)
	}
	defer gitClient.Close()

	opts := repository.CloneConfig{}
	if checkoutRef != nil {
		opts.Tag = checkoutRef.Tag
		opts.SemVer = checkoutRef.SemVer
		opts.Commit = checkoutRef.Commit
		opts.Branch = checkoutRef.Branch
	}

	if enabled, _ := r.features[features.GitShallowClone]; enabled {
		opts.ShallowClone = true
	}

	// Use the git operations timeout for the repo.
	cloneCtx, cancel := context.WithTimeout(ctx, origin.Spec.Timeout.Duration)
	defer cancel()
	debuglog.Info("attempting to clone git repository", "gitrepository", originName, "ref", checkoutRef, "working", tmp)
	if _, err := gitClient.Clone(cloneCtx, origin.Spec.URL, opts); err != nil {
		return failWithError(err)
	}

	// When there's a push branch specified, the pushed-to branch is where commits
	// shall be made
	if switchBranch {
		// Use the git operations timeout for the repo.
		fetchCtx, cancel := context.WithTimeout(ctx, origin.Spec.Timeout.Duration)
		defer cancel()
		if err := gitClient.SwitchBranch(fetchCtx, pushBranch); err != nil {
			return failWithError(err)
		}
	}

	switch {
	case auto.Spec.Update != nil && auto.Spec.Update.Strategy == imagev1.UpdateStrategySetters:
		// For setters we first want to compile a list of _all_ the
		// policies in the same namespace (maybe in the future this
		// could be filtered by the automation object).
		var policies imagev1_reflect.ImagePolicyList
		if err := r.List(ctx, &policies, &client.ListOptions{Namespace: req.NamespacedName.Namespace}); err != nil {
			return failWithError(err)
		}

		manifestsPath := tmp
		if auto.Spec.Update.Path != "" {
			tracelog.Info("adjusting update path according to .spec.update.path", "base", tmp, "spec-path", auto.Spec.Update.Path)
			p, err := securejoin.SecureJoin(tmp, auto.Spec.Update.Path)
			if err != nil {
				return failWithError(err)
			}
			manifestsPath = p
		}

		debuglog.Info("updating with setters according to image policies", "count", len(policies.Items), "manifests-path", manifestsPath)
		if tracelog.Enabled() {
			for _, item := range policies.Items {
				tracelog.Info("found policy", "namespace", item.Namespace, "name", item.Name, "latest-image", item.Status.LatestImage)
			}
		}

		result, err := updateAccordingToSetters(ctx, tracelog, manifestsPath, manifestsPath, policies.Items)
		if err != nil {
			return failWithError(err)
		}

		templateValues.Updated = result

	default:
		log.Info("no update strategy given in the spec")
		// no sense rescheduling until this resource changes
		r.event(ctx, auto, eventv1.EventSeverityInfo, "no known update strategy in spec, failing trivially")
		imagev1.SetImageUpdateAutomationReadiness(&auto, metav1.ConditionFalse, imagev1.NoStrategyReason, "no known update strategy is given for object")
		return ctrl.Result{}, r.patchStatus(ctx, req, auto.Status)
	}

	debuglog.Info("ran updates to working dir", "working", tmp)

	var signingEntity *openpgp.Entity
	if gitSpec.Commit.SigningKey != nil {
		if signingEntity, err = r.getSigningEntity(ctx, auto); err != nil {
			return failWithError(err)
		}
	}

	// construct the commit message from template and values
	message, err := templateMsg(gitSpec.Commit.MessageTemplate, &templateValues)
	if err != nil {
		return failWithError(err)
	}

	var rev string
	if len(templateValues.Updated.Files) > 0 {
		// The status message depends on what happens next. Since there's
		// more than one way to succeed, there's some if..else below, and
		// early returns only on failure.
		rev, err = gitClient.Commit(
			git.Commit{
				Author: git.Signature{
					Name:  gitSpec.Commit.Author.Name,
					Email: gitSpec.Commit.Author.Email,
					When:  time.Now(),
				},
				Message: message,
			},
			repository.WithSigner(signingEntity),
		)
	} else {
		err = extgogit.ErrEmptyCommit
	}

	var statusMessage strings.Builder
	if err != nil {
		if !errors.Is(err, git.ErrNoStagedFiles) && !errors.Is(err, extgogit.ErrEmptyCommit) {
			return failWithError(err)
		}

		log.Info("no changes made in working directory; no commit")
		statusMessage.WriteString("no updates made")

		if auto.Status.LastPushTime != nil && len(auto.Status.LastPushCommit) >= 7 {
			statusMessage.WriteString(fmt.Sprintf("; last commit %s at %s",
				auto.Status.LastPushCommit[:7], auto.Status.LastPushTime.Format(time.RFC3339)))
		}
	} else {
		// Use the git operations timeout for the repo.
		pushCtx, cancel := context.WithTimeout(ctx, origin.Spec.Timeout.Duration)
		defer cancel()

		var pushConfig repository.PushConfig
		if gitSpec.Push != nil {
			pushConfig.Options = gitSpec.Push.Options
		}
		if pushBranch != "" {
			// If the force push feature flag is true and we are pushing to a
			// different branch than the one we checked out to, then force push
			// these changes.
			forcePush := r.features[features.GitForcePushBranch]
			if forcePush && switchBranch {
				pushConfig.Force = true
			}

			if err := gitClient.Push(pushCtx, pushConfig); err != nil {
				return failWithError(err)
			}
			log.Info("pushed commit to origin", "revision", rev, "branch", pushBranch)
			statusMessage.WriteString(fmt.Sprintf("committed and pushed commit '%s' to branch '%s'", rev, pushBranch))
		}

		if gitSpec.Push != nil && gitSpec.Push.Refspec != "" {
			pushConfig.Refspecs = []string{gitSpec.Push.Refspec}
			if err := gitClient.Push(pushCtx, pushConfig); err != nil {
				return failWithError(err)
			}
			log.Info("pushed commit to origin", "revision", rev, "refspec", gitSpec.Push.Refspec)

			if statusMessage.Len() > 0 {
				statusMessage.WriteString(fmt.Sprintf(" and using refspec '%s'", gitSpec.Push.Refspec))
			} else {
				statusMessage.WriteString(fmt.Sprintf("committed and pushed commit '%s' using refspec '%s'", rev, gitSpec.Push.Refspec))
			}
		}

		r.event(ctx, auto, eventv1.EventSeverityInfo, fmt.Sprintf("%s\n%s", statusMessage.String(), message))

		auto.Status.LastPushCommit = rev
		auto.Status.LastPushTime = &metav1.Time{Time: start}
	}

	// Getting to here is a successful run.
	auto.Status.LastAutomationRunTime = &metav1.Time{Time: start}
	imagev1.SetImageUpdateAutomationReadiness(&auto, metav1.ConditionTrue, imagev1.ReconciliationSucceededReason, statusMessage.String())
	if err := r.patchStatus(ctx, req, auto.Status); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	// We're either in this method because something changed, or this
	// object got requeued. Either way, once successful, we don't need
	// to see the object again until Interval has passed, or something
	// changes again.

	interval := intervalOrDefault(&auto)
	return ctrl.Result{RequeueAfter: interval}, nil
}

func (r *ImageUpdateAutomationReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, opts ImageUpdateAutomationReconcilerOptions) error {
	// Index the git repository object that each I-U-A refers to
	if err := mgr.GetFieldIndexer().IndexField(ctx, &imagev1.ImageUpdateAutomation{}, repoRefKey, func(obj client.Object) []string {
		updater := obj.(*imagev1.ImageUpdateAutomation)
		ref := updater.Spec.SourceRef
		return []string{ref.Name}
	}); err != nil {
		return err
	}

	if r.features == nil {
		r.features = features.FeatureGates()
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&imagev1.ImageUpdateAutomation{}, builder.WithPredicates(
			predicate.Or(predicate.GenerationChangedPredicate{}, predicates.ReconcileRequestedPredicate{}))).
		Watches(&sourcev1.GitRepository{}, handler.EnqueueRequestsFromMapFunc(r.automationsForGitRepo)).
		Watches(&imagev1_reflect.ImagePolicy{}, handler.EnqueueRequestsFromMapFunc(r.automationsForImagePolicy)).
		WithOptions(controller.Options{
			RateLimiter: opts.RateLimiter,
		}).
		Complete(r)
}

func (r *ImageUpdateAutomationReconciler) patchStatus(ctx context.Context,
	req ctrl.Request,
	newStatus imagev1.ImageUpdateAutomationStatus) error {

	var auto imagev1.ImageUpdateAutomation
	if err := r.Get(ctx, req.NamespacedName, &auto); err != nil {
		return err
	}

	patch := client.MergeFrom(auto.DeepCopy())
	auto.Status = newStatus

	return r.Status().Patch(ctx, &auto, patch)
}

// intervalOrDefault gives the interval specified, or if missing, the default
func intervalOrDefault(auto *imagev1.ImageUpdateAutomation) time.Duration {
	if auto.Spec.Interval.Duration < time.Second {
		return time.Second
	}
	return auto.Spec.Interval.Duration
}

func (r *ImageUpdateAutomationReconciler) getGitClientOpts(gitTransport git.TransportType, proxyOpts *transport.ProxyOptions,
	diffPushBranch bool) []gogit.ClientOption {
	clientOpts := []gogit.ClientOption{gogit.WithDiskStorage()}
	if gitTransport == git.HTTP {
		clientOpts = append(clientOpts, gogit.WithInsecureCredentialsOverHTTP())
	}

	if proxyOpts != nil {
		clientOpts = append(clientOpts, gogit.WithProxy(*proxyOpts))
	}

	// If the push branch is different from the checkout ref, we need to
	// have all the references downloaded at clone time, to ensure that
	// SwitchBranch will have access to the target branch state. fluxcd/flux2#3384
	//
	// To always overwrite the push branch, the feature gate
	// GitAllBranchReferences can be set to false, which will cause
	// the SwitchBranch operation to ignore the remote branch state.
	allReferences := r.features[features.GitAllBranchReferences]
	if diffPushBranch {
		clientOpts = append(clientOpts, gogit.WithSingleBranch(!allReferences))
	}
	return clientOpts
}

// automationsForGitRepo fetches all the automations that refer to a
// particular source.GitRepository object.
func (r *ImageUpdateAutomationReconciler) automationsForGitRepo(ctx context.Context, obj client.Object) []reconcile.Request {
	var autoList imagev1.ImageUpdateAutomationList
	if err := r.List(ctx, &autoList, client.InNamespace(obj.GetNamespace()),
		client.MatchingFields{repoRefKey: obj.GetName()}); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "failed to list ImageUpdateAutomations for GitRepository change")
		return nil
	}
	reqs := make([]reconcile.Request, len(autoList.Items), len(autoList.Items))
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
	reqs := make([]reconcile.Request, len(autoList.Items), len(autoList.Items))
	for i := range autoList.Items {
		reqs[i].NamespacedName.Name = autoList.Items[i].GetName()
		reqs[i].NamespacedName.Namespace = autoList.Items[i].GetNamespace()
	}
	return reqs
}

// getAuthOpts fetches the secret containing the auth options (if specified),
// constructs a git.AuthOptions object using those options along with the provided
// repository's URL and returns it.
func (r *ImageUpdateAutomationReconciler) getAuthOpts(ctx context.Context, repository *sourcev1.GitRepository) (*git.AuthOptions, error) {
	var data map[string][]byte
	var err error
	if repository.Spec.SecretRef != nil {
		data, err = r.getSecretData(ctx, repository.Spec.SecretRef.Name, repository.GetNamespace())
		if err != nil {
			return nil, fmt.Errorf("failed to get auth secret '%s/%s': %w", repository.GetNamespace(), repository.Spec.SecretRef.Name, err)
		}
	}

	u, err := url.Parse(repository.Spec.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL '%s': %w", repository.Spec.URL, err)
	}

	opts, err := git.NewAuthOptions(*u, data)
	if err != nil {
		return nil, fmt.Errorf("failed to configure authentication options: %w", err)
	}

	return opts, nil
}

// getProxyOpts fetches the secret containing the proxy settings, constructs a
// transport.ProxyOptions object using those settings and then returns it.
func (r *ImageUpdateAutomationReconciler) getProxyOpts(ctx context.Context, proxySecretName,
	proxySecretNamespace string) (*transport.ProxyOptions, error) {
	proxyData, err := r.getSecretData(ctx, proxySecretName, proxySecretNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy secret '%s/%s': %w", proxySecretNamespace, proxySecretName, err)
	}
	address, ok := proxyData["address"]
	if !ok {
		return nil, fmt.Errorf("invalid proxy secret '%s/%s': key 'address' is missing", proxySecretNamespace, proxySecretName)
	}

	proxyOpts := &transport.ProxyOptions{
		URL:      string(address),
		Username: string(proxyData["username"]),
		Password: string(proxyData["password"]),
	}
	return proxyOpts, nil
}

func (r *ImageUpdateAutomationReconciler) getSecretData(ctx context.Context, name, namespace string) (map[string][]byte, error) {
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	var secret corev1.Secret
	if err := r.Client.Get(ctx, key, &secret); err != nil {
		return nil, err
	}
	return secret.Data, nil
}

// getSigningEntity retrieves an OpenPGP entity referenced by the
// provided imagev1.ImageUpdateAutomation for git commit signing
func (r *ImageUpdateAutomationReconciler) getSigningEntity(ctx context.Context, auto imagev1.ImageUpdateAutomation) (*openpgp.Entity, error) {
	// get kubernetes secret
	secretName := types.NamespacedName{
		Namespace: auto.GetNamespace(),
		Name:      auto.Spec.GitSpec.Commit.SigningKey.SecretRef.Name,
	}
	var secret corev1.Secret
	if err := r.Get(ctx, secretName, &secret); err != nil {
		return nil, fmt.Errorf("could not find signing key secret '%s': %w", secretName, err)
	}

	// get data from secret
	data, ok := secret.Data[signingSecretKey]
	if !ok {
		return nil, fmt.Errorf("signing key secret '%s' does not contain a 'git.asc' key", secretName)
	}

	// read entity from secret value
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("could not read signing key from secret '%s': %w", secretName, err)
	}
	if len(entities) > 1 {
		return nil, fmt.Errorf("multiple entities read from secret '%s', could not determine which signing key to use", secretName)
	}

	entity := entities[0]
	if entity.PrivateKey != nil && entity.PrivateKey.Encrypted {
		passphrase, ok := secret.Data[signingPassphraseKey]
		if !ok {
			return nil, fmt.Errorf("can not use passphrase protected signing key without '%s' field present in secret %s",
				signingPassphraseKey, secretName)
		}
		if err = entity.PrivateKey.Decrypt([]byte(passphrase)); err != nil {
			return nil, fmt.Errorf("could not decrypt private key of the signing key present in secret %s: %w", secretName, err)
		}
	}
	return entity, nil
}

// --- events, metrics

func (r *ImageUpdateAutomationReconciler) event(ctx context.Context, auto imagev1.ImageUpdateAutomation, severity, msg string) {
	eventtype := "Normal"
	if severity == eventv1.EventSeverityError {
		eventtype = "Warning"
	}
	r.EventRecorder.Eventf(&auto, eventtype, severity, msg)
}

// --- updates

// updateAccordingToSetters updates files under the root by treating
// the given image policies as kyaml setters.
func updateAccordingToSetters(ctx context.Context, tracelog logr.Logger, inpath, outpath string, policies []imagev1_reflect.ImagePolicy) (update.Result, error) {
	return update.UpdateWithSetters(tracelog, inpath, outpath, policies)
}

// templateMsg renders a msg template, returning the message or an error.
func templateMsg(messageTemplate string, templateValues *TemplateData) (string, error) {
	if messageTemplate == "" {
		messageTemplate = defaultMessageTemplate
	}

	// Includes only functions that are guaranteed to always evaluate to the same result for given input.
	// This removes the possibility of accidentally relying on where or when the template runs.
	// https://github.com/Masterminds/sprig/blob/3ac42c7bc5e4be6aa534e036fb19dde4a996da2e/functions.go#L70
	t, err := template.New("commit message").Funcs(sprig.HermeticTxtFuncMap()).Parse(messageTemplate)
	if err != nil {
		return "", fmt.Errorf("unable to create commit message template from spec: %w", err)
	}

	b := &strings.Builder{}
	if err := t.Execute(b, *templateValues); err != nil {
		return "", fmt.Errorf("failed to run template from spec: %w", err)
	}
	return b.String(), nil
}
