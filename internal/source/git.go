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

package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/gogit"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta2"
)

const (
	signingSecretKey     = "git.asc"
	signingPassphraseKey = "passphrase"
)

type gitSrcCfg struct {
	srcKey       types.NamespacedName
	url          string
	pushBranch   string
	switchBranch bool

	checkoutRef *sourcev1.GitRepositoryRef

	authOpts      *git.AuthOptions
	clientOpts    []gogit.ClientOption
	signingEntity *openpgp.Entity
}

func buildGitConfig(ctx context.Context, c client.Client, originKey, srcKey types.NamespacedName, gitSpec *imagev1.GitSpec, opts SourceOptions) (*gitSrcCfg, error) {
	cfg := &gitSrcCfg{
		srcKey: srcKey,
	}

	// Get the repo.
	repo := &sourcev1.GitRepository{}
	if err := c.Get(ctx, srcKey, repo); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil, fmt.Errorf("referenced git repository does not exist: %w", err)
		}
	}
	cfg.url = repo.Spec.URL

	// Get the checkout ref for the source, prioritizing the image automation
	// object gitSpec checkout reference and falling back to the GitRepository
	// reference if not provided.
	// var checkoutRef *sourcev1.GitRepositoryRef
	if gitSpec.Checkout != nil {
		cfg.checkoutRef = &gitSpec.Checkout.Reference
	} else if repo.Spec.Reference != nil {
		cfg.checkoutRef = repo.Spec.Reference
	} // else remain as `nil` and git.DefaultBranch will be used.

	// Configure push first as the client options below depend on the push
	// configuration.
	if err := configurePush(cfg, gitSpec, cfg.checkoutRef); err != nil {
		return nil, err
	}

	var err error
	cfg.authOpts, err = getAuthOpts(ctx, c, repo)
	if err != nil {
		return nil, err
	}
	proxyOpts, err := getProxyOpts(ctx, c, repo)
	if err != nil {
		return nil, err
	}
	cfg.clientOpts = []gogit.ClientOption{gogit.WithDiskStorage()}
	if cfg.authOpts.Transport == git.HTTP {
		cfg.clientOpts = append(cfg.clientOpts, gogit.WithInsecureCredentialsOverHTTP())
	}
	if proxyOpts != nil {
		cfg.clientOpts = append(cfg.clientOpts, gogit.WithProxy(*proxyOpts))
	}
	// If the push branch is different from the checkout ref, we need to
	// have all the references downloaded at clone time, to ensure that
	// SwitchBranch will have access to the target branch state. fluxcd/flux2#3384
	//
	// To always overwrite the push branch, the feature gate
	// GitAllBranchReferences can be set to false, which will cause
	// the SwitchBranch operation to ignore the remote branch state.
	if cfg.switchBranch {
		cfg.clientOpts = append(cfg.clientOpts, gogit.WithSingleBranch(!opts.gitAllBranchReferences))
	}

	if gitSpec.Commit.SigningKey != nil {
		if cfg.signingEntity, err = getSigningEntity(ctx, c, originKey.Namespace, gitSpec); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func configurePush(cfg *gitSrcCfg, gitSpec *imagev1.GitSpec, checkoutRef *sourcev1.GitRepositoryRef) error {
	if gitSpec.Push != nil && gitSpec.Push.Branch != "" {
		cfg.pushBranch = gitSpec.Push.Branch

		if checkoutRef != nil {
			if cfg.pushBranch != checkoutRef.Branch {
				cfg.switchBranch = true
			}
		} else {
			// Compare with the git default branch when no checkout ref is
			// explicitly defined.
			if cfg.pushBranch != git.DefaultBranch {
				cfg.switchBranch = true
			}
		}
		return nil
	}

	// If no push branch is configured above, use the branch from checkoutRef.

	// Here's where it gets constrained. If there's no push branch
	// given, then the checkout ref must include a branch, and
	// that can be used.
	if checkoutRef == nil || checkoutRef.Branch == "" {
		return errors.New("push spec not provided, and cannot be inferred from .spec.git.checkout.ref or GitRepository .spec.ref")
	}
	cfg.pushBranch = checkoutRef.Branch
	return nil
}

func getAuthOpts(ctx context.Context, c client.Client, repo *sourcev1.GitRepository) (*git.AuthOptions, error) {
	var data map[string][]byte
	var err error
	if repo.Spec.SecretRef != nil {
		data, err = getSecretData(ctx, c, repo.Spec.SecretRef.Name, repo.GetNamespace())
		if err != nil {
			return nil, fmt.Errorf("failed to get auth secret '%s/%s': %w", repo.GetNamespace(), repo.Spec.SecretRef.Name, err)
		}
	}

	u, err := url.Parse(repo.Spec.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL '%s': %w", repo.Spec.URL, err)
	}

	opts, err := git.NewAuthOptions(*u, data)
	if err != nil {
		return nil, fmt.Errorf("failed to configure authentication options: %w", err)
	}

	return opts, nil
}

func getProxyOpts(ctx context.Context, c client.Client, repo *sourcev1.GitRepository) (*transport.ProxyOptions, error) {
	if repo.Spec.ProxySecretRef == nil {
		return nil, nil
	}
	name := repo.Spec.ProxySecretRef.Name
	namespace := repo.GetNamespace()
	proxyData, err := getSecretData(ctx, c, name, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get proxy secret '%s/%s': %w", namespace, name, err)
	}
	address, ok := proxyData["address"]
	if !ok {
		return nil, fmt.Errorf("invalid proxy secret '%s/%s': key 'address' is missing", namespace, name)
	}

	proxyOpts := &transport.ProxyOptions{
		URL:      string(address),
		Username: string(proxyData["username"]),
		Password: string(proxyData["password"]),
	}
	return proxyOpts, nil
}

func getSigningEntity(ctx context.Context, c client.Client, namespace string, gitSpec *imagev1.GitSpec) (*openpgp.Entity, error) {
	secretName := gitSpec.Commit.SigningKey.SecretRef.Name
	secretData, err := getSecretData(ctx, c, secretName, namespace)
	if err != nil {
		return nil, fmt.Errorf("could not find signing key secret '%s': %w", secretName, err)
	}

	data, ok := secretData[signingSecretKey]
	if !ok {
		return nil, fmt.Errorf("signing key secret '%s' does not contain a 'git.asc' key", secretName)
	}

	// Read entity from secret value
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("could not read signing key from secret '%s': %w", secretName, err)
	}
	if len(entities) > 1 {
		return nil, fmt.Errorf("multiple entities read from secret '%s', could not determine which signing key to use", secretName)
	}

	entity := entities[0]
	if entity.PrivateKey != nil && entity.PrivateKey.Encrypted {
		passphrase, ok := secretData[signingPassphraseKey]
		if !ok {
			return nil, fmt.Errorf("can not use passphrase protected signing key without '%s' field present in secret %s",
				"passphrase", secretName)
		}
		if err = entity.PrivateKey.Decrypt([]byte(passphrase)); err != nil {
			return nil, fmt.Errorf("could not decrypt private key of the signing key present in secret %s: %w", secretName, err)
		}
	}
	return entity, nil
}

func getSecretData(ctx context.Context, c client.Client, name, namespace string) (map[string][]byte, error) {
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	var secret corev1.Secret
	if err := c.Get(ctx, key, &secret); err != nil {
		return nil, err
	}
	return secret.Data, nil
}
