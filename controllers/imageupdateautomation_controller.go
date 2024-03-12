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
	"math"
	"net/url"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/ProtonMail/go-crypto/openpgp"
	securejoin "github.com/cyphar/filepath-securejoin"
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
	"sigs.k8s.io/controller-runtime/pkg/source"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta1"
	apiacl "github.com/fluxcd/pkg/apis/acl"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/gogit"
	"github.com/fluxcd/pkg/git/libgit2"
	"github.com/fluxcd/pkg/runtime/acl"
	helper "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/events"
	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/fluxcd/pkg/runtime/predicates"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta1"
	"github.com/fluxcd/image-automation-controller/pkg/update"
)

const originRemote = "origin"

const defaultMessageTemplate = `Update from image update automation`

const repoRefKey = ".spec.gitRepository"

const signingSecretKey = "git.asc"

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
}

type ImageUpdateAutomationReconcilerOptions struct {
	MaxConcurrentReconciles int
	RateLimiter             ratelimiter.RateLimiter
	RecoverPanic            bool
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

	// Add our finalizer if it does not exist.
	if !controllerutil.ContainsFinalizer(&auto, imagev1.ImageUpdateAutomationFinalizer) {
		patch := client.MergeFrom(auto.DeepCopy())
		controllerutil.AddFinalizer(&auto, imagev1.ImageUpdateAutomationFinalizer)
		if err := r.Patch(ctx, &auto, patch); err != nil {
			log.Error(err, "unable to register finalizer")
			return ctrl.Result{}, err
		}
	}

	// If the object is under deletion, record the readiness, and remove our finalizer.
	if !auto.ObjectMeta.DeletionTimestamp.IsZero() {
		controllerutil.RemoveFinalizer(&auto, imagev1.ImageUpdateAutomationFinalizer)
		if err := r.Update(ctx, &auto); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// record suspension metrics
	r.RecordSuspend(ctx, &auto, auto.Spec.Suspend)

	if auto.Spec.Suspend {
		log.Info("ImageUpdateAutomation is suspended, skipping automation run")
		return ctrl.Result{}, nil
	}

	templateValues.AutomationObject = req.NamespacedName

	defer func() {
		// Always record readiness and duration metrics
		r.Metrics.RecordReadiness(ctx, &auto)
		r.Metrics.RecordDuration(ctx, &auto, start)
	}()

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
		r.event(ctx, auto, events.EventSeverityError, err.Error())
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
		r.event(ctx, auto, events.EventSeverityError, err.Error())
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
	var ref *sourcev1.GitRepositoryRef
	if gitSpec.Checkout != nil {
		ref = &gitSpec.Checkout.Reference
		tracelog.Info("using git repository ref from .spec.git.checkout", "ref", ref)
	} else if r := origin.Spec.Reference; r != nil {
		ref = r
		tracelog.Info("using git repository ref from GitRepository spec", "ref", ref)
	} // else remain as `nil` and git.DefaultBranch will be used.

	var pushBranch string
	if gitSpec.Push != nil {
		pushBranch = gitSpec.Push.Branch
		tracelog.Info("using push branch from .spec.push.branch", "branch", pushBranch)
	} else {
		// Here's where it gets constrained. If there's no push branch
		// given, then the checkout ref must include a branch, and
		// that can be used.
		if ref == nil || ref.Branch == "" {
			return failWithError(fmt.Errorf("Push branch not given explicitly, and cannot be inferred from .spec.git.checkout.ref or GitRepository .spec.ref"))
		}
		pushBranch = ref.Branch
		tracelog.Info("using push branch from $ref.branch", "branch", pushBranch)
	}

	tmp, err := os.MkdirTemp("", fmt.Sprintf("%s-%s", originName.Namespace, originName.Name))
	if err != nil {
		return failWithError(err)
	}
	defer func() {
		if err := os.RemoveAll(tmp); err != nil {
			log.Error(err, "failed to remove working directory", "path", tmp)
		}
	}()

	// FIXME use context with deadline for at least the following ops

	debuglog.Info("attempting to clone git repository", "gitrepository", originName, "ref", ref, "working", tmp)

	authOpts, err := r.getAuthOpts(ctx, &origin)
	if err != nil {
		return failWithError(err)
	}

	var gitClient git.RepositoryClient
	switch origin.Spec.GitImplementation {
	case sourcev1.LibGit2Implementation:
		gitClient, err = libgit2.NewClient(tmp, authOpts)
	case sourcev1.GoGitImplementation, "":
		gitClient, err = gogit.NewClient(tmp, authOpts)
	default:
		err = fmt.Errorf("failed to create git client; referred GitRepository has invalid implementation: %s", origin.Spec.GitImplementation)
	}
	if err != nil {
		return failWithError(err)
	}
	defer gitClient.Close()

	opts := git.CloneOptions{}
	if ref != nil {
		opts.Tag = ref.Tag
		opts.SemVer = ref.SemVer
		opts.Commit = ref.Commit
		opts.Branch = ref.Branch
	}

	// Use the git operations timeout for the repo.
	cloneCtx, cancel := context.WithTimeout(ctx, origin.Spec.Timeout.Duration)
	defer cancel()
	if _, err := gitClient.Clone(cloneCtx, origin.Spec.URL, opts); err != nil {
		return failWithError(err)
	}

	// When there's a push spec, the pushed-to branch is where commits
	// shall be made
	if gitSpec.Push != nil && !(ref != nil && ref.Branch == pushBranch) {
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

		if result, err := updateAccordingToSetters(ctx, tracelog, manifestsPath, policies.Items); err != nil {
			return failWithError(err)
		} else {
			templateValues.Updated = result
		}
	default:
		log.Info("no update strategy given in the spec")
		// no sense rescheduling until this resource changes
		r.event(ctx, auto, events.EventSeverityInfo, "no known update strategy in spec, failing trivially")
		imagev1.SetImageUpdateAutomationReadiness(&auto, metav1.ConditionFalse, imagev1.NoStrategyReason, "no known update strategy is given for object")
		return ctrl.Result{}, r.patchStatus(ctx, req, auto.Status)
	}

	debuglog.Info("ran updates to working dir", "working", tmp)

	var statusMessage string

	var signingEntity *openpgp.Entity
	if gitSpec.Commit.SigningKey != nil {
		if signingEntity, err = r.getSigningEntity(ctx, auto); err != nil {
			failWithError(err)
		}
	}

	// construct the commit message from template and values
	message, err := templateMsg(gitSpec.Commit.MessageTemplate, &templateValues)
	if err != nil {
		return failWithError(err)
	}

	// The status message depends on what happens next. Since there's
	// more than one way to succeed, there's some if..else below, and
	// early returns only on failure.
	if rev, err := gitClient.Commit(
		git.Commit{
			Author: git.Signature{
				Name:  gitSpec.Commit.Author.Name,
				Email: gitSpec.Commit.Author.Email,
				When:  time.Now(),
			},
			Message: message,
		},
		git.WithSigner(signingEntity),
	); err != nil {
		if err != git.ErrNoStagedFiles {
			return failWithError(err)
		}

		log.Info("no changes made in working directory; no commit")
		statusMessage = "no updates made"

		if auto.Status.LastPushTime != nil && len(auto.Status.LastPushCommit) >= 7 {
			statusMessage = fmt.Sprintf("%s; last commit %s at %s", statusMessage, auto.Status.LastPushCommit[:7], auto.Status.LastPushTime.Format(time.RFC3339))
		}
	} else {
		// Use the git operations timeout for the repo.
		pushCtx, cancel := context.WithTimeout(ctx, origin.Spec.Timeout.Duration)
		defer cancel()
		if err := gitClient.Push(pushCtx); err != nil {
			return failWithError(err)
		}

		r.event(ctx, auto, events.EventSeverityInfo, fmt.Sprintf("Committed and pushed change %s to %s\n%s", rev, pushBranch, message))
		log.Info("pushed commit to origin", "revision", rev, "branch", pushBranch)
		auto.Status.LastPushCommit = rev
		auto.Status.LastPushTime = &metav1.Time{Time: start}
		statusMessage = "committed and pushed " + rev + " to " + pushBranch
	}

	// Getting to here is a successful run.
	auto.Status.LastAutomationRunTime = &metav1.Time{Time: start}
	imagev1.SetImageUpdateAutomationReadiness(&auto, metav1.ConditionTrue, imagev1.ReconciliationSucceededReason, statusMessage)
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

func (r *ImageUpdateAutomationReconciler) SetupWithManager(mgr ctrl.Manager, opts ImageUpdateAutomationReconcilerOptions) error {
	ctx := context.Background()
	// Index the git repository object that each I-U-A refers to
	if err := mgr.GetFieldIndexer().IndexField(ctx, &imagev1.ImageUpdateAutomation{}, repoRefKey, func(obj client.Object) []string {
		updater := obj.(*imagev1.ImageUpdateAutomation)
		ref := updater.Spec.SourceRef
		return []string{ref.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&imagev1.ImageUpdateAutomation{}, builder.WithPredicates(
			predicate.Or(predicate.GenerationChangedPredicate{}, predicates.ReconcileRequestedPredicate{}))).
		Watches(&source.Kind{Type: &sourcev1.GitRepository{}}, handler.EnqueueRequestsFromMapFunc(r.automationsForGitRepo)).
		Watches(&source.Kind{Type: &imagev1_reflect.ImagePolicy{}}, handler.EnqueueRequestsFromMapFunc(r.automationsForImagePolicy)).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: opts.MaxConcurrentReconciles,
			RateLimiter:             opts.RateLimiter,
			RecoverPanic:            opts.RecoverPanic,
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

// durationSinceLastRun calculates how long it's been since the last
// time the automation ran (which you can then use to find how long to
// wait until the next run).
func durationSinceLastRun(auto *imagev1.ImageUpdateAutomation, now time.Time) time.Duration {
	last := auto.Status.LastAutomationRunTime
	if last == nil {
		return time.Duration(math.MaxInt64) // a fairly long time
	}
	return now.Sub(last.Time)
}

// automationsForGitRepo fetches all the automations that refer to a
// particular source.GitRepository object.
func (r *ImageUpdateAutomationReconciler) automationsForGitRepo(obj client.Object) []reconcile.Request {
	ctx := context.Background()
	var autoList imagev1.ImageUpdateAutomationList
	if err := r.List(ctx, &autoList, client.InNamespace(obj.GetNamespace()),
		client.MatchingFields{repoRefKey: obj.GetName()}); err != nil {
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
func (r *ImageUpdateAutomationReconciler) automationsForImagePolicy(obj client.Object) []reconcile.Request {
	ctx := context.Background()
	var autoList imagev1.ImageUpdateAutomationList
	if err := r.List(ctx, &autoList, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, len(autoList.Items), len(autoList.Items))
	for i := range autoList.Items {
		reqs[i].NamespacedName.Name = autoList.Items[i].GetName()
		reqs[i].NamespacedName.Namespace = autoList.Items[i].GetNamespace()
	}
	return reqs
}

type repoAccess struct {
	auth *git.AuthOptions
	url  string
}

func (r *ImageUpdateAutomationReconciler) getAuthOpts(ctx context.Context, repository *sourcev1.GitRepository) (*git.AuthOptions, error) {
	var data map[string][]byte
	if repository.Spec.SecretRef != nil {
		name := types.NamespacedName{
			Namespace: repository.GetNamespace(),
			Name:      repository.Spec.SecretRef.Name,
		}

		secret := &corev1.Secret{}
		err := r.Client.Get(ctx, name, secret)
		if err != nil {
			return nil, fmt.Errorf("failed to get secret '%s': %w", name.String(), err)
		}
		data = secret.Data
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
	return entities[0], nil
}

// --- events, metrics

func (r *ImageUpdateAutomationReconciler) event(ctx context.Context, auto imagev1.ImageUpdateAutomation, severity, msg string) {
	eventtype := "Normal"
	if severity == events.EventSeverityError {
		eventtype = "Warning"
	}
	r.EventRecorder.Eventf(&auto, eventtype, severity, msg)
}

// --- updates

// updateAccordingToSetters updates files under the root by treating
// the given image policies as kyaml setters.
func updateAccordingToSetters(ctx context.Context, tracelog logr.Logger, path string, policies []imagev1_reflect.ImagePolicy) (update.Result, error) {
	return update.UpdateWithSetters(tracelog, path, path, policies)
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
