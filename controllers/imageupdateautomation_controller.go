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
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/go-logr/logr"
	libgit2 "github.com/libgit2/git2go/v33"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kuberecorder "k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/reference"
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
	"github.com/fluxcd/pkg/runtime/acl"
	"github.com/fluxcd/pkg/runtime/events"
	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/fluxcd/pkg/runtime/metrics"
	"github.com/fluxcd/pkg/runtime/predicates"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/fluxcd/source-controller/pkg/git"
	"github.com/fluxcd/source-controller/pkg/git/libgit2/managed"
	gitstrat "github.com/fluxcd/source-controller/pkg/git/strategy"

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
	Scheme              *runtime.Scheme
	EventRecorder       kuberecorder.EventRecorder
	MetricsRecorder     *metrics.Recorder
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
	now := time.Now()
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
		r.recordReadinessMetric(ctx, &auto)
		controllerutil.RemoveFinalizer(&auto, imagev1.ImageUpdateAutomationFinalizer)
		if err := r.Update(ctx, &auto); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// record suspension metrics
	defer r.recordSuspension(ctx, auto)

	if auto.Spec.Suspend {
		log.Info("ImageUpdateAutomation is suspended, skipping automation run")
		return ctrl.Result{}, nil
	}

	templateValues.AutomationObject = req.NamespacedName

	// Record readiness metric when exiting; if there's any points at
	// which the readiness is updated _without also exiting_, they
	// should also record the readiness.
	defer r.recordReadinessMetric(ctx, &auto)
	// Record reconciliation duration when exiting
	if r.MetricsRecorder != nil {
		objRef, err := reference.GetReference(r.Scheme, &auto)
		if err != nil {
			return ctrl.Result{}, err
		}
		defer r.MetricsRecorder.RecordDuration(*objRef, now)
	}

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
	} // else remain as `nil`, which is an acceptable value for cloneInto, later.

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

	access, err := r.getRepoAccess(ctx, &origin)
	if err != nil {
		return failWithError(err)
	}

	// We set the TransportOptionsURL of this set of authentication options here by constructing
	// a unique URL that won't clash in a multi tenant environment. This unique URL is used by
	// libgit2 managed transports. This enables us to bypass the inbuilt credentials callback in
	// libgit2, which is inflexible and unstable.
	// NB: The Transport Options URL must be unique, therefore it must use the object under
	// reconciliation details, instead of the repository it depends on.
	if strings.HasPrefix(origin.Spec.URL, "http") {
		access.auth.TransportOptionsURL = fmt.Sprintf("http://%s/%s/%d", auto.Name, auto.UID, auto.Generation)
	} else if strings.HasPrefix(origin.Spec.URL, "ssh") {
		access.auth.TransportOptionsURL = fmt.Sprintf("ssh://%s/%s/%d", auto.Name, auto.UID, auto.Generation)
	} else {
		return failWithError(fmt.Errorf("git repository URL '%s' has invalid transport type, supported types are: http, https, ssh", origin.Spec.URL))
	}

	// Use the git operations timeout for the repo.
	cloneCtx, cancel := context.WithTimeout(ctx, origin.Spec.Timeout.Duration)
	defer cancel()
	var repo *libgit2.Repository
	if repo, err = cloneInto(cloneCtx, access, ref, tmp); err != nil {
		return failWithError(err)
	}
	defer repo.Free()

	// Checkout removes TransportOptions before returning, therefore this
	// must happen after cloneInto.
	// TODO(pjbgf): Git consolidation should improve the API workflow.
	managed.AddTransportOptions(access.auth.TransportOptionsURL, managed.TransportOptions{
		TargetURL:    origin.Spec.URL,
		AuthOpts:     access.auth,
		ProxyOptions: &libgit2.ProxyOptions{Type: libgit2.ProxyTypeAuto},
		Context:      cloneCtx,
	})

	defer managed.RemoveTransportOptions(access.auth.TransportOptionsURL)

	// When there's a push spec, the pushed-to branch is where commits
	// shall be made

	if gitSpec.Push != nil && !(ref != nil && ref.Branch == pushBranch) {
		// Use the git operations timeout for the repo.
		fetchCtx, cancel := context.WithTimeout(ctx, origin.Spec.Timeout.Duration)
		defer cancel()
		if err := switchToBranch(repo, fetchCtx, pushBranch, access); err != nil && err != errRemoteBranchMissing {
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
	signature := &libgit2.Signature{
		Name:  gitSpec.Commit.Author.Name,
		Email: gitSpec.Commit.Author.Email,
		When:  time.Now(),
	}

	if rev, err := commitChangedManifests(tracelog, repo, tmp, signingEntity, signature, message); err != nil {
		if err != errNoChanges {
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
		if err := push(pushCtx, tmp, pushBranch, access); err != nil {
			return failWithError(err)
		}

		r.event(ctx, auto, events.EventSeverityInfo, fmt.Sprintf("Committed and pushed change %s to %s\n%s", rev, pushBranch, message))
		log.Info("pushed commit to origin", "revision", rev, "branch", pushBranch)
		auto.Status.LastPushCommit = rev
		auto.Status.LastPushTime = &metav1.Time{Time: now}
		statusMessage = "committed and pushed " + rev + " to " + pushBranch
	}

	// Getting to here is a successful run.
	auto.Status.LastAutomationRunTime = &metav1.Time{Time: now}
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

func (r *ImageUpdateAutomationReconciler) getRepoAccess(ctx context.Context, repository *sourcev1.GitRepository) (repoAccess, error) {
	var access repoAccess
	access.url = repository.Spec.URL
	access.auth = &git.AuthOptions{}

	if repository.Spec.SecretRef != nil {
		name := types.NamespacedName{
			Namespace: repository.GetNamespace(),
			Name:      repository.Spec.SecretRef.Name,
		}

		secret := &corev1.Secret{}
		err := r.Client.Get(ctx, name, secret)
		if err != nil {
			err = fmt.Errorf("auth secret error: %w", err)
			return access, err
		}

		access.auth, err = git.AuthOptionsFromSecret(access.url, secret)
		if err != nil {
			err = fmt.Errorf("auth error: %w", err)
			return access, err
		}
	}
	return access, nil
}

// cloneInto clones the upstream repository at the `ref` given (which
// can be `nil`). It returns a `*libgit2.Repository` since that is used
// for committing changes.
func cloneInto(ctx context.Context, access repoAccess, ref *sourcev1.GitRepositoryRef,
	path string) (*libgit2.Repository, error) {
	opts := git.CheckoutOptions{}
	if ref != nil {
		opts.Tag = ref.Tag
		opts.SemVer = ref.SemVer
		opts.Commit = ref.Commit
		opts.Branch = ref.Branch
	}
	checkoutStrat, err := gitstrat.CheckoutStrategyForImplementation(ctx, sourcev1.LibGit2Implementation, opts)
	if err == nil {
		_, err = checkoutStrat.Checkout(ctx, path, access.url, access.auth)
	}
	if err != nil {
		return nil, err
	}

	return libgit2.OpenRepository(path)
}

func headCommit(repo *libgit2.Repository) (*libgit2.Commit, error) {
	head, err := repo.Head()
	if err != nil {
		return nil, err
	}
	defer head.Free()
	c, err := repo.LookupCommit(head.Target())
	if err != nil {
		return nil, err
	}
	return c, nil
}

var errNoChanges error = errors.New("no changes made to working directory")

func commitChangedManifests(tracelog logr.Logger, repo *libgit2.Repository, absRepoPath string, ent *openpgp.Entity, sig *libgit2.Signature, message string) (string, error) {
	sl, err := repo.StatusList(&libgit2.StatusOptions{
		Show: libgit2.StatusShowIndexAndWorkdir,
	})
	if err != nil {
		return "", err
	}
	defer sl.Free()

	count, err := sl.EntryCount()
	if err != nil {
		return "", err
	}

	if count == 0 {
		return "", errNoChanges
	}

	var parentC []*libgit2.Commit
	head, err := headCommit(repo)
	if err == nil {
		defer head.Free()
		parentC = append(parentC, head)
	}

	index, err := repo.Index()
	if err != nil {
		return "", err
	}
	defer index.Free()

	// add to index any files that are not within .git/
	if err = filepath.Walk(repo.Workdir(),
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(repo.Workdir(), path)
			if err != nil {
				return err
			}
			f, err := os.Stat(path)
			if err != nil {
				return err
			}
			if f.IsDir() || strings.HasPrefix(rel, ".git") || rel == "." {
				return nil
			}
			if err := index.AddByPath(rel); err != nil {
				tracelog.Info("adding file", "file", rel)
				return err
			}
			return nil
		}); err != nil {
		return "", err
	}

	if err := index.Write(); err != nil {
		return "", err
	}

	treeID, err := index.WriteTree()
	if err != nil {
		return "", err
	}

	tree, err := repo.LookupTree(treeID)
	if err != nil {
		return "", err
	}
	defer tree.Free()

	commitID, err := repo.CreateCommit("HEAD", sig, sig, message, tree, parentC...)
	if err != nil {
		return "", err
	}

	// return unsigned commit if pgp entity is not provided
	if ent == nil {
		return commitID.String(), nil
	}

	commit, err := repo.LookupCommit(commitID)
	if err != nil {
		return "", err
	}
	defer commit.Free()

	signedCommitID, err := commit.WithSignatureUsing(func(commitContent string) (string, string, error) {
		cipherText := new(bytes.Buffer)
		err := openpgp.ArmoredDetachSignText(cipherText, ent, strings.NewReader(commitContent), &packet.Config{})
		if err != nil {
			return "", "", errors.New("error signing payload")
		}

		return cipherText.String(), "", nil
	})
	if err != nil {
		return "", err
	}
	signedCommit, err := repo.LookupCommit(signedCommitID)
	if err != nil {
		return "", err
	}
	defer signedCommit.Free()

	newHead, err := repo.Head()
	if err != nil {
		return "", err
	}
	defer newHead.Free()

	ref, err := repo.References.Create(
		newHead.Name(),
		signedCommit.Id(),
		true,
		"repoint to signed commit",
	)
	if err != nil {
		return "", err
	}
	defer ref.Free()

	return signedCommitID.String(), nil
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

var errRemoteBranchMissing = errors.New("remote branch missing")

// switchToBranch switches to a branch after fetching latest from upstream.
// If the branch does not exist, it is created using the head as the starting point.
func switchToBranch(repo *libgit2.Repository, ctx context.Context, branch string, access repoAccess) error {
	origin, err := repo.Remotes.Lookup(originRemote)
	if err != nil {
		return fmt.Errorf("cannot lookup remote: %w", err)
	}
	defer origin.Free()

	// Override callbacks with dummy ones as they are not needed within Managed Transport.
	// However, not setting them may lead to git2go panicing.
	callbacks := managed.RemoteCallbacks()

	// Force the fetching of the remote branch.
	err = origin.Fetch([]string{branch}, &libgit2.FetchOptions{
		RemoteCallbacks: callbacks,
	}, "")
	if err != nil {
		return fmt.Errorf("cannot fetch remote branch: %w", err)
	}

	remoteBranch, err := repo.References.Lookup(fmt.Sprintf("refs/remotes/origin/%s", branch))
	if err != nil && !libgit2.IsErrorCode(err, libgit2.ErrorCodeNotFound) {
		return err
	}
	if remoteBranch != nil {
		defer remoteBranch.Free()
	}
	err = nil

	var commit *libgit2.Commit
	// tries to get tip commit from remote branch, if it exists.
	// otherwise gets the commit that local head is pointing to.
	if remoteBranch != nil {
		commit, err = repo.LookupCommit(remoteBranch.Target())
	} else {
		head, err := repo.Head()
		if err != nil {
			return fmt.Errorf("cannot get repo head: %w", err)
		}
		defer head.Free()
		commit, err = repo.LookupCommit(head.Target())
	}
	if err != nil {
		return fmt.Errorf("cannot find the head commit: %w", err)
	}
	defer commit.Free()

	localBranch, err := repo.References.Lookup(fmt.Sprintf("refs/heads/%s", branch))
	if err != nil && !libgit2.IsErrorCode(err, libgit2.ErrorCodeNotFound) {
		return fmt.Errorf("cannot lookup branch '%s': %w", branch, err)
	}
	if localBranch == nil {
		lb, err := repo.CreateBranch(branch, commit, false)
		if err != nil {
			return fmt.Errorf("cannot create branch '%s': %w", branch, err)
		}
		defer lb.Free()
		// We could've done something like:
		// localBranch = lb.Reference
		// But for some reason, calling `lb.Free()` AND using it, causes a really
		// nasty crash. Since, we can't avoid calling `lb.Free()`, in order to prevent
		// memory leaks, we don't use `lb` and instead manually lookup the ref.
		localBranch, err = repo.References.Lookup(fmt.Sprintf("refs/heads/%s", branch))
		if err != nil {
			return fmt.Errorf("cannot lookup branch '%s': %w", branch, err)
		}
	}
	defer localBranch.Free()

	tree, err := repo.LookupTree(commit.TreeId())
	if err != nil {
		return fmt.Errorf("cannot lookup tree for branch '%s': %w", branch, err)
	}
	defer tree.Free()

	err = repo.CheckoutTree(tree, &libgit2.CheckoutOpts{
		// the remote branch should take precedence if it exists at this point in time.
		Strategy: libgit2.CheckoutForce,
	})
	if err != nil {
		return fmt.Errorf("cannot checkout tree for branch '%s': %w", branch, err)
	}

	ref, err := localBranch.SetTarget(commit.Id(), "")
	if err != nil {
		return fmt.Errorf("cannot update branch '%s' to be at target commit: %w", branch, err)
	}
	ref.Free()

	return repo.SetHead("refs/heads/" + branch)
}

// push pushes the branch given to the origin using the git library
// indicated by `impl`. It's passed both the path to the repo and a
// libgit2.Repository value, since the latter may as well be used if the
// implementation is libgit2.
func push(ctx context.Context, path, branch string, access repoAccess) error {
	repo, err := libgit2.OpenRepository(path)
	if err != nil {
		return err
	}
	defer repo.Free()
	origin, err := repo.Remotes.Lookup(originRemote)
	if err != nil {
		return err
	}
	defer origin.Free()

	// Override callbacks with dummy ones as they are not needed within Managed Transport.
	// However, not setting them may lead to git2go panicing.
	callbacks := managed.RemoteCallbacks()

	// calling repo.Push will succeed even if a reference update is
	// rejected; to detect this case, this callback is supplied.
	var callbackErr error
	callbacks.PushUpdateReferenceCallback = func(refname, status string) error {
		if status != "" {
			callbackErr = fmt.Errorf("ref %s rejected: %s", refname, status)
		}
		return nil
	}
	err = origin.Push([]string{fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)}, &libgit2.PushOptions{
		RemoteCallbacks: callbacks,
		ProxyOptions:    libgit2.ProxyOptions{Type: libgit2.ProxyTypeAuto},
	})
	if err != nil {
		if strings.Contains(err.Error(), "early EOF") {
			return fmt.Errorf("%w (the SSH key may not have write access to the repository)", err)
		}
		return libgit2PushError(err)
	}
	return callbackErr
}

func libgit2PushError(err error) error {
	if err == nil {
		return err
	}
	// libgit2 returns the whole output from stderr, and we only need
	// the message. GitLab likes to return a banner, so as an
	// heuristic, strip any lines that are just "remote:" and spaces
	// or fencing.
	msg := err.Error()
	lines := strings.Split(msg, "\n")
	if len(lines) == 1 {
		return err
	}
	var b strings.Builder
	// the following removes the prefix "remote:" from each line; to
	// retain a bit of fidelity to the original error, start with it.
	b.WriteString("remote: ")

	var appending bool
	for _, line := range lines {
		m := strings.TrimPrefix(line, "remote:")
		if m = strings.Trim(m, " \t="); m != "" {
			if appending {
				b.WriteString(" ")
			}
			b.WriteString(m)
			appending = true
		}
	}
	return errors.New(b.String())
}

// --- events, metrics

func (r *ImageUpdateAutomationReconciler) event(ctx context.Context, auto imagev1.ImageUpdateAutomation, severity, msg string) {
	eventtype := "Normal"
	if severity == events.EventSeverityError {
		eventtype = "Warning"
	}
	r.EventRecorder.Eventf(&auto, eventtype, severity, msg)
}

func (r *ImageUpdateAutomationReconciler) recordReadinessMetric(ctx context.Context, auto *imagev1.ImageUpdateAutomation) {
	if r.MetricsRecorder == nil {
		return
	}

	objRef, err := reference.GetReference(r.Scheme, auto)
	if err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "unable to record readiness metric")
		return
	}
	if rc := apimeta.FindStatusCondition(auto.Status.Conditions, meta.ReadyCondition); rc != nil {
		r.MetricsRecorder.RecordCondition(*objRef, *rc, !auto.DeletionTimestamp.IsZero())
	} else {
		r.MetricsRecorder.RecordCondition(*objRef, metav1.Condition{
			Type:   meta.ReadyCondition,
			Status: metav1.ConditionUnknown,
		}, !auto.DeletionTimestamp.IsZero())
	}
}

// --- updates

// updateAccordingToSetters updates files under the root by treating
// the given image policies as kyaml setters.
func updateAccordingToSetters(ctx context.Context, tracelog logr.Logger, path string, policies []imagev1_reflect.ImagePolicy) (update.Result, error) {
	return update.UpdateWithSetters(tracelog, path, path, policies)
}

func (r *ImageUpdateAutomationReconciler) recordSuspension(ctx context.Context, auto imagev1.ImageUpdateAutomation) {
	if r.MetricsRecorder == nil {
		return
	}
	log := ctrl.LoggerFrom(ctx)

	objRef, err := reference.GetReference(r.Scheme, &auto)
	if err != nil {
		log.Error(err, "unable to record suspended metric")
		return
	}

	if !auto.DeletionTimestamp.IsZero() {
		r.MetricsRecorder.RecordSuspend(*objRef, false)
	} else {
		r.MetricsRecorder.RecordSuspend(*objRef, auto.Spec.Suspend)
	}
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
