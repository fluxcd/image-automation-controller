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
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"strings"
	"text/template"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kuberecorder "k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1alpha1"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/events"
	"github.com/fluxcd/pkg/runtime/predicates"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta1"
	"github.com/fluxcd/source-controller/pkg/git"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1alpha1"
	"github.com/fluxcd/image-automation-controller/pkg/update"
)

const defaultInterval = 2 * time.Minute

// log level for debug info
const debug = 1
const originRemote = "origin"

const defaultMessageTemplate = `Update from image update automation`

const repoRefKey = ".spec.gitRepository"
const imagePolicyKey = ".spec.update.imagePolicy"

// ImageUpdateAutomationReconciler reconciles a ImageUpdateAutomation object
type ImageUpdateAutomationReconciler struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	EventRecorder kuberecorder.EventRecorder
}

// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imageupdateautomations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imageupdateautomations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch

func (r *ImageUpdateAutomationReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("imageupdateautomation", req.NamespacedName)
	now := time.Now()

	var auto imagev1.ImageUpdateAutomation
	if err := r.Get(ctx, req.NamespacedName, &auto); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if auto.Spec.Suspend {
		log.Info("ImageUpdateAutomation is suspended, skipping automation run")
		return ctrl.Result{}, nil
	}

	// whatever else happens, we've now "seen" the reconcile
	// annotation if it's there
	if token, ok := meta.ReconcileAnnotationValue(auto.GetAnnotations()); ok {
		auto.Status.SetLastHandledReconcileRequest(token)
		if err := r.Status().Update(ctx, &auto); err != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	// failWithError is a helper for bailing on the reconciliation.
	failWithError := func(err error) (ctrl.Result, error) {
		r.event(auto, events.EventSeverityError, err.Error())
		imagev1.SetImageUpdateAutomationReadiness(&auto, metav1.ConditionFalse, meta.ReconciliationFailedReason, err.Error())
		if err := r.Status().Update(ctx, &auto); err != nil {
			log.Error(err, "failed to reconcile")
		}
		return ctrl.Result{Requeue: true}, err
	}

	// get the git repository object so it can be checked out
	var origin sourcev1.GitRepository
	originName := types.NamespacedName{
		Name:      auto.Spec.Checkout.GitRepositoryRef.Name,
		Namespace: auto.GetNamespace(),
	}
	if err := r.Get(ctx, originName, &origin); err != nil {
		if client.IgnoreNotFound(err) == nil {
			imagev1.SetImageUpdateAutomationReadiness(&auto, metav1.ConditionFalse, imagev1.GitNotAvailableReason, "referenced git repository is missing")
			log.Error(err, "referenced git repository does not exist")
			if err := r.Status().Update(ctx, &auto); err != nil {
				return ctrl.Result{Requeue: true}, err
			}
			return ctrl.Result{}, nil // and assume we'll hear about it when it arrives
		}
		return ctrl.Result{}, err
	}

	log.V(debug).Info("found git repository", "gitrepository", originName)

	tmp, err := ioutil.TempDir("", fmt.Sprintf("%s-%s", originName.Namespace, originName.Name))
	if err != nil {
		return failWithError(err)
	}
	defer os.RemoveAll(tmp)

	// FIXME use context with deadline for at least the following ops

	access, err := r.getRepoAccess(ctx, &origin)
	if err != nil {
		return failWithError(err)
	}

	var repo *gogit.Repository
	if repo, err = cloneInto(ctx, access, auto.Spec.Checkout.Branch, tmp); err != nil {
		return failWithError(err)
	}

	log.V(debug).Info("cloned git repository", "gitrepository", originName, "branch", auto.Spec.Checkout.Branch, "working", tmp)

	updateStrat := auto.Spec.Update
	switch {
	case updateStrat.Setters != nil:
		// For setters we first want to compile a list of _all_ the
		// policies in the same namespace (maybe in the future this
		// could be filtered by the automation object).
		var policies imagev1_reflect.ImagePolicyList
		if err := r.List(ctx, &policies, &client.ListOptions{Namespace: req.NamespacedName.Namespace}); err != nil {
			return failWithError(err)
		}

		if err := updateAccordingToSetters(ctx, tmp, policies.Items); err != nil {
			return failWithError(err)
		}
	default:
		log.Info("no update strategy given in the spec")
		// no sense rescheduling until this resource changes
		r.event(auto, events.EventSeverityInfo, "no update strategy in spec, failing trivially")
		imagev1.SetImageUpdateAutomationReadiness(&auto, metav1.ConditionFalse, imagev1.NoStrategyReason, "no update strategy is given for object")
		err := r.Status().Update(ctx, &auto)
		return ctrl.Result{}, err
	}

	log.V(debug).Info("ran updates to working dir", "working", tmp)

	var statusMessage string

	if rev, err := commitAllAndPush(ctx, repo, access, &auto.Spec.Commit); err != nil {
		if err == errNoChanges {
			r.event(auto, events.EventSeverityInfo, "no updates made")
			log.V(debug).Info("no changes made in working directory; no commit")
			statusMessage = "no updates made"
		} else {
			return failWithError(err)
		}
	} else {
		r.event(auto, events.EventSeverityInfo, "committed and pushed change "+rev)
		log.Info("pushed commit to origin", "revision", rev)
		statusMessage = "committed and pushed " + rev
	}

	// Getting to here is a successful run.
	auto.Status.LastAutomationRunTime = &metav1.Time{Time: now}
	imagev1.SetImageUpdateAutomationReadiness(&auto, metav1.ConditionTrue, meta.ReconciliationSucceededReason, statusMessage)
	if err = r.Status().Update(ctx, &auto); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	// We're either in this method because something changed, or this
	// object got requeued. Either way, once successful, we don't need
	// to see the object again until Interval has passed, or something
	// changes again.

	interval := intervalOrDefault(&auto)
	return ctrl.Result{RequeueAfter: interval}, nil
}

func (r *ImageUpdateAutomationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()
	// Index the git repository object that each I-U-A refers to
	if err := mgr.GetFieldIndexer().IndexField(ctx, &imagev1.ImageUpdateAutomation{}, repoRefKey, func(obj runtime.Object) []string {
		updater := obj.(*imagev1.ImageUpdateAutomation)
		ref := updater.Spec.Checkout.GitRepositoryRef
		return []string{ref.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&imagev1.ImageUpdateAutomation{}).
		WithEventFilter(predicates.ChangePredicate{}).
		Watches(&source.Kind{Type: &sourcev1.GitRepository{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.automationsForGitRepo),
			}).
		Complete(r)
}

// intervalOrDefault gives the interval specified, or if missing, the default
func intervalOrDefault(auto *imagev1.ImageUpdateAutomation) time.Duration {
	if auto.Spec.RunInterval != nil {
		return auto.Spec.RunInterval.Duration
	}
	return defaultInterval
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
func (r *ImageUpdateAutomationReconciler) automationsForGitRepo(obj handler.MapObject) []reconcile.Request {
	ctx := context.Background()
	var autoList imagev1.ImageUpdateAutomationList
	if err := r.List(ctx, &autoList, client.InNamespace(obj.Meta.GetNamespace()), client.MatchingFields{repoRefKey: obj.Meta.GetName()}); err != nil {
		r.Log.Error(err, "failed to list ImageUpdateAutomations for GitRepository", "name", types.NamespacedName{
			Name:      obj.Meta.GetName(),
			Namespace: obj.Meta.GetNamespace(),
		})
		return nil
	}
	reqs := make([]reconcile.Request, len(autoList.Items), len(autoList.Items))
	for i := range autoList.Items {
		reqs[i].NamespacedName.Name = autoList.Items[i].GetName()
		reqs[i].NamespacedName.Namespace = autoList.Items[i].GetNamespace()
	}
	return reqs
}

// --- git ops

type repoAccess struct {
	auth transport.AuthMethod
	url  string
}

func (r *ImageUpdateAutomationReconciler) getRepoAccess(ctx context.Context, repository *sourcev1.GitRepository) (repoAccess, error) {
	var access repoAccess
	access.url = repository.Spec.URL
	authStrat := git.AuthSecretStrategyForURL(access.url)

	if repository.Spec.SecretRef != nil && authStrat != nil {
		name := types.NamespacedName{
			Namespace: repository.GetNamespace(),
			Name:      repository.Spec.SecretRef.Name,
		}

		var secret corev1.Secret
		err := r.Client.Get(ctx, name, &secret)
		if err != nil {
			err = fmt.Errorf("auth secret error: %w", err)
			return access, err
		}

		access.auth, err = authStrat.Method(secret)
		if err != nil {
			err = fmt.Errorf("auth error: %w", err)
			return access, err
		}
	}
	return access, nil
}

func cloneInto(ctx context.Context, access repoAccess, branch, path string) (*gogit.Repository, error) {
	checkoutStrat := git.CheckoutStrategyForRef(&sourcev1.GitRepositoryRef{
		Branch: branch,
	})
	_, _, err := checkoutStrat.Checkout(ctx, path, access.url, access.auth)
	if err != nil {
		return nil, err
	}

	return gogit.PlainOpen(path)
}

var errNoChanges error = errors.New("no changes made to working directory")

func commitAllAndPush(ctx context.Context, repo *gogit.Repository, access repoAccess, commit *imagev1.CommitSpec) (string, error) {
	working, err := repo.Worktree()
	if err != nil {
		return "", err
	}

	status, err := working.Status()
	if err != nil {
		return "", err
	} else if status.IsClean() {
		return "", errNoChanges
	}

	msgTmpl := commit.MessageTemplate
	if msgTmpl == "" {
		msgTmpl = defaultMessageTemplate
	}
	tmpl, err := template.New("commit message").Parse(msgTmpl)
	if err != nil {
		return "", err
	}
	buf := &strings.Builder{}
	if err := tmpl.Execute(buf, "no data! yet"); err != nil {
		return "", err
	}

	var rev plumbing.Hash
	if rev, err = working.Commit(buf.String(), &gogit.CommitOptions{
		All: true,
		Author: &object.Signature{
			Name:  commit.AuthorName,
			Email: commit.AuthorEmail,
			When:  time.Now(),
		},
	}); err != nil {
		return "", err
	}

	return rev.String(), repo.PushContext(ctx, &gogit.PushOptions{
		Auth: access.auth,
	})
}

// --- events

func (r *ImageUpdateAutomationReconciler) event(auto imagev1.ImageUpdateAutomation, severity, msg string) {
	if r.EventRecorder != nil {
		r.EventRecorder.Event(&auto, "Normal", severity, msg)
	}
}

// --- updates

// updateAccordingToSetters updates files under the root by treating
// the given image policies as kyaml setters.
func updateAccordingToSetters(ctx context.Context, path string, policies []imagev1_reflect.ImagePolicy) error {
	return update.UpdateWithSetters(path, path, policies)
}
