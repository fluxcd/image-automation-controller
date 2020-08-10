/*
Copyright 2020 The Flux CD contributors.

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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	imagev1alpha1 "github.com/fluxcd/image-automation-controller/api/v1alpha1"
	"github.com/fluxcd/image-automation-controller/pkg/update"
	imagev1alpha1_reflect "github.com/fluxcd/image-reflector-controller/api/v1alpha1"
	sourcev1alpha1 "github.com/fluxcd/source-controller/api/v1alpha1"
	"github.com/fluxcd/source-controller/pkg/git"
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
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imageupdateautomations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imageupdateautomations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=gitrepositories,verbs=get;list;watch

func (r *ImageUpdateAutomationReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("imageupdateautomation", req.NamespacedName)

	var auto imagev1alpha1.ImageUpdateAutomation
	if err := r.Get(ctx, req.NamespacedName, &auto); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// get the git repository object so it can be checked out
	var origin sourcev1alpha1.GitRepository
	originName := types.NamespacedName{
		Name:      auto.Spec.Checkout.GitRepositoryRef.Name,
		Namespace: auto.GetNamespace(),
	}
	if err := r.Get(ctx, originName, &origin); err != nil {
		// TODO status
		if client.IgnoreNotFound(err) == nil {
			log.Error(err, "referenced git repository does not exist")
			return ctrl.Result{}, nil // and assume we'll hear about it when it arrives
		}
		return ctrl.Result{}, err
	}

	log.V(debug).Info("found git repository", "gitrepository", originName)

	tmp, err := ioutil.TempDir("", fmt.Sprintf("%s-%s", originName.Namespace, originName.Name))
	if err != nil {
		// TODO status
		return ctrl.Result{}, err
	}
	defer os.RemoveAll(tmp)

	// FIXME use context with deadline for at least the following ops

	access, err := r.getRepoAccess(ctx, &origin)
	if err != nil {
		return ctrl.Result{}, err
	}

	var repo *gogit.Repository
	if repo, err = cloneInto(ctx, access, auto.Spec.Checkout.Branch, tmp); err != nil {
		// TODO status
		return ctrl.Result{}, err
	}

	log.V(debug).Info("cloned git repository", "gitrepository", originName, "branch", auto.Spec.Checkout.Branch, "working", tmp)

	updateStrat := auto.Spec.Update
	switch {
	case updateStrat.ImagePolicyRef != nil:
		var policy imagev1alpha1_reflect.ImagePolicy
		policyName := types.NamespacedName{
			Namespace: auto.GetNamespace(),
			Name:      updateStrat.ImagePolicyRef.Name,
		}
		if err := r.Get(ctx, policyName, &policy); err != nil {
			if client.IgnoreNotFound(err) == nil {
				log.Info("referenced ImagePolicy not found")
				// assume we'll be told if the image policy turns up, or if this resource changes
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
		if err := updateAccordingToImagePolicy(ctx, tmp, &policy); err != nil {
			if err == errImagePolicyNotReady {
				log.Info("image policy does not have latest image ref", "imagepolicy", policyName)
				// assume we'll be told if the image policy or this resource changes
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
	default:
		log.Info("no update strategy given in the spec")
		// no sense rescheduling until this resource changes
		return ctrl.Result{}, nil
	}

	log.V(debug).Info("ran updates to working dir", "working", tmp)

	var commitMade bool
	if rev, err := commitAllAndPush(ctx, repo, access, &auto.Spec.Commit); err != nil {
		if err == errNoChanges {
			log.Info("no changes made in working directory; no commit")
		} else {
			return ctrl.Result{}, err
		}
	} else {
		commitMade = true
		log.V(debug).Info("pushed commit to origin", "revision", rev)
	}

	// The status is not updated unless a commit was made, OR it's
	// been at least interval since the last run (in which case,
	// assume this is a periodic run). This is so there's a fixed
	// point -- otherwise, the fact of the status change would mean it
	// gets queued again.

	now := time.Now()
	interval := intervalOrDefault(&auto)
	sinceLast := durationSinceLastRun(&auto, now)

	when := interval

	if commitMade || sinceLast >= interval {
		auto.Status.LastAutomationRunTime = &metav1.Time{Time: now}
		if err = r.Status().Update(ctx, &auto); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// requeue for the remainder of the interval
		when = interval - sinceLast
	}

	return ctrl.Result{RequeueAfter: when}, nil
}

func (r *ImageUpdateAutomationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()
	// Index the git repository object that each I-U-A refers to
	if err := mgr.GetFieldIndexer().IndexField(ctx, &imagev1alpha1.ImageUpdateAutomation{}, repoRefKey, func(obj runtime.Object) []string {
		updater := obj.(*imagev1alpha1.ImageUpdateAutomation)
		ref := updater.Spec.Checkout.GitRepositoryRef
		return []string{ref.Name}
	}); err != nil {
		return err
	}

	// Index the image policy (if any) that each I-U-A refers to
	if err := mgr.GetFieldIndexer().IndexField(ctx, &imagev1alpha1.ImageUpdateAutomation{}, imagePolicyKey, func(obj runtime.Object) []string {
		updater := obj.(*imagev1alpha1.ImageUpdateAutomation)
		if ref := updater.Spec.Update.ImagePolicyRef; ref != nil {
			return []string{ref.Name}
		}
		return nil
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&imagev1alpha1.ImageUpdateAutomation{}).
		Watches(&source.Kind{Type: &sourcev1alpha1.GitRepository{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.automationsForGitRepo),
			}).
		Watches(&source.Kind{Type: &imagev1alpha1_reflect.ImagePolicy{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.automationsForImagePolicy),
			}).
		Complete(r)
}

// intervalOrDefault gives the interval specified, or if missing, the default
func intervalOrDefault(auto *imagev1alpha1.ImageUpdateAutomation) time.Duration {
	if auto.Spec.RunInterval != nil {
		return auto.Spec.RunInterval.Duration
	}
	return defaultInterval
}

// durationUntilNextRun gives the length of time to wait before
// running the automation again after a successful run, unless
// something (a dependency) changes to trigger a run.
func durationSinceLastRun(auto *imagev1alpha1.ImageUpdateAutomation, now time.Time) time.Duration {
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
	var autoList imagev1alpha1.ImageUpdateAutomationList
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

// automationsForImagePolicy fetches all the automations that refer to
// a particular source.ImagePolicy object.
func (r *ImageUpdateAutomationReconciler) automationsForImagePolicy(obj handler.MapObject) []reconcile.Request {
	ctx := context.Background()
	var autoList imagev1alpha1.ImageUpdateAutomationList
	if err := r.List(ctx, &autoList, client.InNamespace(obj.Meta.GetNamespace()), client.MatchingFields{imagePolicyKey: obj.Meta.GetName()}); err != nil {
		r.Log.Error(err, "failed to list ImageUpdateAutomations for ImagePolicy", "name", types.NamespacedName{
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

func (r *ImageUpdateAutomationReconciler) getRepoAccess(ctx context.Context, repository *sourcev1alpha1.GitRepository) (repoAccess, error) {
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
	checkoutStrat := git.CheckoutStrategyForRef(&sourcev1alpha1.GitRepositoryRef{
		Branch: branch,
	})
	_, _, err := checkoutStrat.Checkout(ctx, path, access.url, access.auth)
	if err != nil {
		return nil, err
	}

	return gogit.PlainOpen(path)
}

var errNoChanges error = errors.New("no changes made to working directory")

func commitAllAndPush(ctx context.Context, repo *gogit.Repository, access repoAccess, commit *imagev1alpha1.CommitSpec) (string, error) {
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

// --- updates

var errImagePolicyNotReady = errors.New("ImagePolicy resource is not ready")

// update the manifest files under path according to policy, by
// replacing any mention of the policy's image repository with the
// latest ref.
func updateAccordingToImagePolicy(ctx context.Context, path string, policy *imagev1alpha1_reflect.ImagePolicy) error {
	// the function that does the update expects an original and a
	// replacement; but it only uses the repository part of the
	// original, and it compares canonical forms (with the defaults
	// filled in). Since the latest image will have the same
	// repository, I can just pass that as the original.
	latestRef := policy.Status.LatestImage
	if latestRef == "" {
		return errImagePolicyNotReady
	}
	return update.UpdateImageEverywhere(path, path, latestRef, latestRef)
}
