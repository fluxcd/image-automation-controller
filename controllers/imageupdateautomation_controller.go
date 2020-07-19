/*
Copyright 2020 Michael Bridgen <mikeb@squaremobius.net>

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
	"strings"

	"github.com/fluxcd/pkg/ssh/knownhosts"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	//	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sourcev1alpha1 "github.com/fluxcd/source-controller/api/v1alpha1"
	imagev1alpha1 "github.com/squaremo/image-automation-controller/api/v1alpha1"
	"github.com/squaremo/image-automation-controller/pkg/update"
	imagev1alpha1_reflect "github.com/squaremo/image-reflector-controller/api/v1alpha1"
)

// log level for debug info
const debug = 1
const originRemote = "origin"

// ImageUpdateAutomationReconciler reconciles a ImageUpdateAutomation object
type ImageUpdateAutomationReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=image.fluxcd.io,resources=imageupdateautomations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=image.fluxcd.io,resources=imageupdateautomations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=source.fluxcd.io,resources=gitrepositories,verbs=get;list;watch
// +kubebuilder:rbac:groups=source.fluxcd.io,resources=gitrepositories,verbs=get;list;watch

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
		Name:      auto.Spec.GitRepository.Name,
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
	//defer os.RemoveAll(tmp)

	// FIXME context with deadline
	if err := r.cloneInto(ctx, &origin, tmp); err != nil {
		// TODO status
		return ctrl.Result{}, err
	}

	log.V(debug).Info("cloned git repository", "gitrepository", originName, "tmp", tmp)

	updateStrat := auto.Spec.Update
	switch {
	case updateStrat.ImagePolicy != nil:
		var policy imagev1alpha1_reflect.ImagePolicy
		policyName := types.NamespacedName{
			Namespace: auto.GetNamespace(),
			Name:      updateStrat.ImagePolicy.Name,
		}
		if err := r.Get(ctx, policyName, &policy); err != nil {
			if client.IgnoreNotFound(err) == nil {
				log.Info("referenced ImagePolicy not found")
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
		if err := updateAccordingToImagePolicy(ctx, tmp, &policy); err != nil {
			if err == errImagePolicyNotReady {
				log.Info("image policy does not have latest image ref", "imagepolicy", policyName)
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
	default:
		log.Info("no update strategy given in the spec")
		return ctrl.Result{}, nil
	}

	log.V(debug).Info("made updates to working dir", "tmp", tmp)

	return ctrl.Result{}, nil
}

func (r *ImageUpdateAutomationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&imagev1alpha1.ImageUpdateAutomation{}).
		Complete(r)
}

// --- git ops

func (r *ImageUpdateAutomationReconciler) cloneInto(ctx context.Context, repository *sourcev1alpha1.GitRepository, path string) error {
	// this is largely cribbed from
	// https://github.com/fluxcd/source-controller/blob/master/controllers/gitrepository_controller.go#L166
	// and
	// https://github.com/fluxcd/source-controller/blob/master/internal/git/checkout.go
	// which is internal to that controller, and doesn't do quite what
	// I want anyway. It'd be nice for source controller to have a
	// convenience method for cloning a GitRepository.

	url := repository.Spec.URL

	// this inlines some of https://github.com/fluxcd/source-controller/blob/master/internal/git/transport.go
	var authFromSecret func(*corev1.Secret) (transport.AuthMethod, error)
	switch {
	case strings.HasPrefix(url, "http"):
		authFromSecret = basicAuth
	case strings.HasPrefix(url, "ssh"):
		authFromSecret = publicKeyAuth
	}

	var auth transport.AuthMethod
	if repository.Spec.SecretRef != nil && authFromSecret != nil {
		name := types.NamespacedName{
			Namespace: repository.GetNamespace(),
			Name:      repository.Spec.SecretRef.Name,
		}

		var secret corev1.Secret
		err := r.Client.Get(ctx, name, &secret)
		if err != nil {
			err = fmt.Errorf("auth secret error: %w", err)
			return err
		}

		auth, err = authFromSecret(&secret)
		if err != nil {
			err = fmt.Errorf("auth error: %w", err)
			return err
		}
	}

	// For now, check out master:
	// https://github.com/fluxcd/source-controller/blob/master/internal/git/checkout.go#L66
	_, err := git.PlainCloneContext(ctx, path, false, &git.CloneOptions{
		URL:               repository.Spec.URL,
		Auth:              auth,
		RemoteName:        originRemote,
		ReferenceName:     plumbing.NewBranchReferenceName("master"),
		SingleBranch:      true,
		NoCheckout:        false,
		Depth:             1,
		RecurseSubmodules: 0,
		Progress:          nil,
		Tags:              git.NoTags,
	})
	if err != nil {
		return fmt.Errorf("git clone error: %w", err)
	}

	return nil
}

func basicAuth(secret *corev1.Secret) (transport.AuthMethod, error) {
	auth := &http.BasicAuth{}
	if username, ok := secret.Data["username"]; ok {
		auth.Username = string(username)
	}
	if password, ok := secret.Data["password"]; ok {
		auth.Password = string(password)
	}
	if auth.Username == "" || auth.Password == "" {
		return nil, fmt.Errorf("invalid '%s' secret data: required fields 'username' and 'password'", secret.Name)
	}
	return auth, nil
}

func publicKeyAuth(secret *corev1.Secret) (transport.AuthMethod, error) {
	identity := secret.Data["identity"]
	knownHosts := secret.Data["known_hosts"]
	if len(identity) == 0 || len(knownHosts) == 0 {
		return nil, fmt.Errorf("invalid '%s' secret data: required fields 'identity' and 'known_hosts'", secret.Name)
	}

	pk, err := ssh.NewPublicKeys("git", identity, "")
	if err != nil {
		return nil, err
	}

	callback, err := knownhosts.New(knownHosts)
	if err != nil {
		return nil, err
	}
	pk.HostKeyCallback = callback
	return pk, nil
}

// --- updates

var errImagePolicyNotReady = errors.New("ImagePolocy resource is not ready")

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
