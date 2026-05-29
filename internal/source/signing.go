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

package source

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ProtonMail/go-crypto/openpgp"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluxcd/pkg/git/signature"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1"
)

// Secret-data keys read by the signing pipeline.
const (
	signingSecretKeyGPG        = "git.asc"
	signingSecretPassphraseGPG = "passphrase"
	signingSecretKeySSH        = "identity"
	signingSecretPasswordSSH   = "password"
)

// detectSigningType validates that the Secret data contains the key
// expected for the given signing-key type. An empty type defaults to
// GPG so callers can omit the API field. It is a pure function so the
// matrix can be table-tested without a fake client.
func detectSigningType(data map[string][]byte, typ imagev1.SigningKeyType, secretName string) error {
	switch typ {
	case imagev1.SigningKeyTypeGPG, "":
		if _, ok := data[signingSecretKeyGPG]; !ok {
			return fmt.Errorf("signing key secret '%s' does not contain a '%s' key", secretName, signingSecretKeyGPG)
		}
		return nil
	case imagev1.SigningKeyTypeSSH:
		if _, ok := data[signingSecretKeySSH]; !ok {
			return fmt.Errorf("signing key secret '%s' does not contain an '%s' key", secretName, signingSecretKeySSH)
		}
		return nil
	default:
		return fmt.Errorf("unknown signing key type %q", typ)
	}
}

// resolveSigner loads the signing-key Secret named by gitSpec from the
// ImageUpdateAutomation's namespace, validates the contents against the
// declared type, and returns the matching signature.Signer. Returns
// (nil, nil) when gitSpec carries no SigningKey configuration.
func resolveSigner(ctx context.Context, c client.Client, namespace string, gitSpec *imagev1.GitSpec) (signature.Signer, error) {
	if gitSpec.Commit.SigningKey == nil {
		return nil, nil
	}

	name := gitSpec.Commit.SigningKey.SecretRef.Name
	secret, err := getSecret(ctx, c, name, namespace)
	if err != nil {
		return nil, fmt.Errorf("could not find signing key secret '%s': %w", name, err)
	}
	if err := detectSigningType(secret.Data, gitSpec.Commit.SigningKey.Type, name); err != nil {
		return nil, err
	}
	switch gitSpec.Commit.SigningKey.Type {
	case imagev1.SigningKeyTypeGPG, "":
		return loadGPGSigner(secret)
	case imagev1.SigningKeyTypeSSH:
		return loadSSHSigner(secret)
	default:
		// Unreachable: detectSigningType already errors on unknown types.
		return nil, fmt.Errorf("unknown signing key type %q", gitSpec.Commit.SigningKey.Type)
	}
}

// loadGPGSigner returns a signature.Signer that signs commits with the
// OpenPGP key in the referenced Secret.
func loadGPGSigner(secret *corev1.Secret) (signature.Signer, error) {
	data, ok := secret.Data[signingSecretKeyGPG]
	if !ok {
		// detectSigningType already guards this case, but a defensive
		// check keeps the leaf usable in isolation.
		return nil, fmt.Errorf("signing key secret '%s' does not contain a '%s' key", secret.Name, signingSecretKeyGPG)
	}

	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("could not read signing key from secret '%s': %w", secret.Name, err)
	}
	if len(entities) > 1 {
		return nil, fmt.Errorf("multiple entities read from secret '%s', could not determine which signing key to use", secret.Name)
	}

	entity := entities[0]
	if entity.PrivateKey != nil && entity.PrivateKey.Encrypted {
		passphrase, ok := secret.Data[signingSecretPassphraseGPG]
		if !ok {
			return nil, fmt.Errorf("can not use passphrase protected signing key without '%s' field present in secret %s", signingSecretPassphraseGPG, secret.Name)
		}
		if err := entity.PrivateKey.Decrypt(passphrase); err != nil {
			return nil, fmt.Errorf("could not decrypt private key of the signing key present in secret %s: %w", secret.Name, err)
		}
	}
	return signature.NewOpenPGPSigner(entity)
}

// loadSSHSigner returns a signature.Signer that signs commits with the SSH
// key in the referenced Secret. Implementation lands in Task 7.
func loadSSHSigner(secret *corev1.Secret) (signature.Signer, error) {
	return nil, fmt.Errorf("loadSSHSigner: not implemented")
}
