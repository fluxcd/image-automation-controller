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
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta2"
	"github.com/fluxcd/image-automation-controller/internal/testutil"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/github"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
)

func Test_getAuthOpts(t *testing.T) {
	namespace := "default"

	invalidAuthSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-auth",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"password": []byte("pass"),
		},
	}

	validAuthSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valid-auth",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"username": []byte("user"),
			"password": []byte("pass"),
		},
	}

	tests := []struct {
		name       string
		url        string
		secretName string
		want       *git.AuthOptions
		wantErr    bool
	}{
		{
			name:       "non-existing secret",
			secretName: "non-existing",
			want:       nil,
			wantErr:    true,
		},
		{
			name:       "invalid secret",
			url:        "https://example.com",
			secretName: "invalid-auth",
			want:       nil,
			wantErr:    true,
		},
		{
			name:       "valid secret",
			url:        "https://example.com",
			secretName: "valid-auth",
			want: &git.AuthOptions{
				Transport: git.HTTPS,
				Host:      "example.com",
				Username:  "user",
				Password:  "pass",
			},
			wantErr: false,
		},
		{
			name: "no secret",
			url:  "https://example.com",
			want: &git.AuthOptions{
				Transport: git.HTTPS,
				Host:      "example.com",
			},
			wantErr: false,
		},
		{
			name:    "invalid URL",
			url:     "://example.com",
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			clientBuilder := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(invalidAuthSecret, validAuthSecret)
			c := clientBuilder.Build()

			gitRepo := &sourcev1.GitRepository{}
			gitRepo.Namespace = namespace
			gitRepo.Spec = sourcev1.GitRepositorySpec{
				URL: tt.url,
			}
			if tt.secretName != "" {
				gitRepo.Spec.SecretRef = &meta.LocalObjectReference{Name: tt.secretName}
			}

			got, err := getAuthOpts(context.TODO(), c, gitRepo, SourceOptions{}, nil)
			if (err != nil) != tt.wantErr {
				g.Fail(fmt.Sprintf("unexpected error: %v", err))
				return
			}
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func Test_getAuthOpts_providerAuth(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		secret     *corev1.Secret
		beforeFunc func(obj *sourcev1.GitRepository)
		wantErr    string
	}{
		{
			name: "azure provider",
			url:  "https://dev.azure.com/foo/bar/_git/baz",
			beforeFunc: func(obj *sourcev1.GitRepository) {
				obj.Spec.Provider = sourcev1.GitProviderAzure
			},
			wantErr: "ManagedIdentityCredential",
		},
		{
			name: "github provider with no secret ref",
			url:  "https://github.com/org/repo.git",
			beforeFunc: func(obj *sourcev1.GitRepository) {
				obj.Spec.Provider = sourcev1.GitProviderGitHub
			},
			wantErr: "secretRef with github app data must be specified when provider is set to github: invalid source configuration",
		},
		{
			name: "github provider with secret ref that does not exist",
			url:  "https://github.com/org/repo.git",
			beforeFunc: func(obj *sourcev1.GitRepository) {
				obj.Spec.Provider = sourcev1.GitProviderGitHub
				obj.Spec.SecretRef = &meta.LocalObjectReference{
					Name: "githubAppSecret",
				}
			},
			wantErr: "failed to get auth secret '/githubAppSecret': secrets \"githubAppSecret\" not found",
		},
		{
			name: "github provider with github app data in secret",
			url:  "https://example.com/org/repo",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "githubAppSecret",
				},
				Data: map[string][]byte{
					github.KeyAppID:             []byte("123"),
					github.KeyAppInstallationID: []byte("456"),
					github.KeyAppPrivateKey:     []byte("abc"),
				},
			},
			beforeFunc: func(obj *sourcev1.GitRepository) {
				obj.Spec.Provider = sourcev1.GitProviderGitHub
				obj.Spec.SecretRef = &meta.LocalObjectReference{
					Name: "githubAppSecret",
				}
			},
			wantErr: "Key must be a PEM encoded PKCS1 or PKCS8 key",
		},
		{
			name: "generic provider with github app data in secret",
			url:  "https://example.com/org/repo",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "githubAppSecret",
				},
				Data: map[string][]byte{
					github.KeyAppID: []byte("123"),
				},
			},
			beforeFunc: func(obj *sourcev1.GitRepository) {
				obj.Spec.Provider = sourcev1.GitProviderGeneric
				obj.Spec.SecretRef = &meta.LocalObjectReference{
					Name: "githubAppSecret",
				}
			},
			wantErr: "secretRef '/githubAppSecret' has github app data but provider is not set to github: invalid source configuration",
		},
		{
			name: "generic provider",
			url:  "https://example.com/org/repo",
			beforeFunc: func(obj *sourcev1.GitRepository) {
				obj.Spec.Provider = sourcev1.GitProviderGeneric
			},
		},
		{
			name: "no provider",
			url:  "https://example.com/org/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			clientBuilder := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithStatusSubresource(&sourcev1.GitRepository{})

			if tt.secret != nil {
				clientBuilder.WithObjects(tt.secret)
			}
			c := clientBuilder.Build()
			obj := &sourcev1.GitRepository{
				Spec: sourcev1.GitRepositorySpec{
					URL: tt.url,
				},
			}

			if tt.beforeFunc != nil {
				tt.beforeFunc(obj)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			opts, err := getAuthOpts(ctx, c, obj, SourceOptions{}, nil)

			if tt.wantErr != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tt.wantErr))
			} else {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(opts).ToNot(BeNil())
				g.Expect(opts.BearerToken).To(BeEmpty())
				g.Expect(opts.Username).To(BeEmpty())
				g.Expect(opts.Password).To(BeEmpty())
			}
		})
	}
}

func Test_getProxyOpts(t *testing.T) {
	namespace := "default"
	invalidProxy := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-proxy",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"url": []byte("https://example.com"),
		},
	}
	validProxy := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "valid-proxy",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"address":  []byte("https://example.com"),
			"username": []byte("user"),
			"password": []byte("pass"),
		},
	}

	tests := []struct {
		name         string
		secretName   string
		want         *transport.ProxyOptions
		wantProxyURL *url.URL
		wantErr      bool
	}{
		{
			name:         "non-existing secret",
			secretName:   "non-existing",
			want:         nil,
			wantProxyURL: nil,
			wantErr:      true,
		},
		{
			name:         "invalid proxy secret",
			secretName:   "invalid-proxy",
			want:         nil,
			wantProxyURL: nil,
			wantErr:      true,
		},
		{
			name:       "valid proxy secret",
			secretName: "valid-proxy",
			want: &transport.ProxyOptions{
				URL:      "https://example.com",
				Username: "user",
				Password: "pass",
			},
			wantProxyURL: &url.URL{
				Scheme: "https",
				Host:   "example.com",
				User:   url.UserPassword("user", "pass"),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			clientBuilder := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(invalidProxy, validProxy)
			c := clientBuilder.Build()

			gitRepo := &sourcev1.GitRepository{}
			gitRepo.Namespace = namespace
			if tt.secretName != "" {
				gitRepo.Spec = sourcev1.GitRepositorySpec{
					ProxySecretRef: &meta.LocalObjectReference{Name: tt.secretName},
				}
			}

			got, gotProxyURL, err := getProxyOpts(context.TODO(), c, gitRepo)
			if (err != nil) != tt.wantErr {
				g.Fail(fmt.Sprintf("unexpected error: %v", err))
				return
			}
			g.Expect(got).To(Equal(tt.want))
			g.Expect(gotProxyURL).To(Equal(tt.wantProxyURL))
		})
	}
}

func Test_getSigningEntity(t *testing.T) {
	g := NewWithT(t)

	namespace := "default"

	passphrase := "abcde12345"
	_, keyEncrypted := testutil.GetSigningKeyPair(g, passphrase)
	encryptedKeySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "encrypted-key",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			signingSecretKey:     keyEncrypted,
			signingPassphraseKey: []byte(passphrase),
		},
	}

	_, keyUnencrypted := testutil.GetSigningKeyPair(g, "")
	unencryptedKeySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unencrypted-key",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			signingSecretKey: keyUnencrypted,
		},
	}

	tests := []struct {
		name       string
		secretName string
		wantErr    bool
	}{
		{
			name:       "non-existing secret",
			secretName: "non-existing",
			wantErr:    true,
		},
		{
			name:       "unencrypted key",
			secretName: "unencrypted-key",
			wantErr:    false,
		},
		{
			name:       "encrypted key",
			secretName: "encrypted-key",
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			clientBuilder := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(encryptedKeySecret, unencryptedKeySecret)
			c := clientBuilder.Build()

			gitSpec := &imagev1.GitSpec{}
			if tt.secretName != "" {
				gitSpec.Commit = imagev1.CommitSpec{
					SigningKey: &imagev1.SigningKey{
						SecretRef: meta.LocalObjectReference{Name: tt.secretName},
					},
				}
			}

			_, err := getSigningEntity(context.TODO(), c, namespace, gitSpec)
			if (err != nil) != tt.wantErr {
				g.Fail(fmt.Sprintf("unexpected error: %v", err))
				return
			}
		})
	}
}

func Test_buildGitConfig(t *testing.T) {
	testGitRepoName := "test-gitrepo"
	namespace := "foo-ns"
	testTimeout := &metav1.Duration{Duration: time.Minute}
	testGitURL := "https://example.com"

	tests := []struct {
		name             string
		gitSpec          *imagev1.GitSpec
		gitRepoName      string
		gitRepoRef       *sourcev1.GitRepositoryRef
		gitRepoTimeout   *metav1.Duration
		gitRepoURL       string
		gitRepoProxyData map[string][]byte
		srcOpts          SourceOptions
		wantErr          bool
		wantCheckoutRef  *sourcev1.GitRepositoryRef
		wantPushBranch   string
		wantSwitchBranch bool
		wantTimeout      *metav1.Duration
	}{
		{
			name: "same branch, gitSpec checkoutRef",
			gitSpec: &imagev1.GitSpec{
				Checkout: &imagev1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{Branch: "aaa"},
				},
			},
			gitRepoName: testGitRepoName,
			gitRepoURL:  testGitURL,
			wantErr:     false,
			wantCheckoutRef: &sourcev1.GitRepositoryRef{
				Branch: "aaa",
			},
			wantPushBranch:   "aaa",
			wantSwitchBranch: false,
			wantTimeout:      testTimeout,
		},
		{
			name: "different branch, gitSpec checkoutRef",
			gitSpec: &imagev1.GitSpec{
				Checkout: &imagev1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{Branch: "aaa"},
				},
				Push: &imagev1.PushSpec{
					Branch: "bbb",
				},
			},
			gitRepoName: testGitRepoName,
			gitRepoURL:  testGitURL,
			wantErr:     false,
			wantCheckoutRef: &sourcev1.GitRepositoryRef{
				Branch: "aaa",
			},
			wantPushBranch:   "bbb",
			wantSwitchBranch: true,
			wantTimeout:      testTimeout,
		},
		{
			name:        "same branch, gitrepo checkoutRef",
			gitSpec:     &imagev1.GitSpec{},
			gitRepoName: testGitRepoName,
			gitRepoURL:  testGitURL,
			gitRepoRef: &sourcev1.GitRepositoryRef{
				Branch: "ccc",
			},
			wantErr: false,
			wantCheckoutRef: &sourcev1.GitRepositoryRef{
				Branch: "ccc",
			},
			wantPushBranch:   "ccc",
			wantSwitchBranch: false,
			wantTimeout:      testTimeout,
		},
		{
			name: "different branch, gitrepo checkoutRef",
			gitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{
					Branch: "ddd",
				},
			},
			gitRepoName: testGitRepoName,
			gitRepoURL:  testGitURL,
			gitRepoRef: &sourcev1.GitRepositoryRef{
				Branch: "ccc",
			},
			wantErr: false,
			wantCheckoutRef: &sourcev1.GitRepositoryRef{
				Branch: "ccc",
			},
			wantPushBranch:   "ddd",
			wantSwitchBranch: true,
			wantTimeout:      testTimeout,
		},
		{
			name: "no checkoutRef defined",
			gitSpec: &imagev1.GitSpec{
				Push: &imagev1.PushSpec{
					Branch: "aaa",
				},
			},
			gitRepoName:      testGitRepoName,
			gitRepoURL:       testGitURL,
			wantErr:          false,
			wantCheckoutRef:  nil, // Use the git default checkout branch.
			wantPushBranch:   "aaa",
			wantSwitchBranch: true,
			wantTimeout:      testTimeout,
		},
		{
			name: "gitSpec override gitRepo checkout config",
			gitSpec: &imagev1.GitSpec{
				Checkout: &imagev1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{Branch: "aaa"},
				},
				Push: &imagev1.PushSpec{
					Branch: "bbb",
				},
			},
			gitRepoName: testGitRepoName,
			gitRepoURL:  testGitURL,
			gitRepoRef: &sourcev1.GitRepositoryRef{
				Branch: "ccc",
			},
			wantErr: false,
			wantCheckoutRef: &sourcev1.GitRepositoryRef{
				Branch: "aaa",
			},
			wantPushBranch:   "bbb",
			wantSwitchBranch: true,
			wantTimeout:      testTimeout,
		},
		{
			name:    "non-existing gitRepo",
			gitSpec: &imagev1.GitSpec{},
			wantErr: true,
		},
		{
			name:        "use gitrepo timeout",
			gitSpec:     &imagev1.GitSpec{},
			gitRepoName: testGitRepoName,
			gitRepoURL:  testGitURL,
			gitRepoRef: &sourcev1.GitRepositoryRef{
				Branch: "ccc",
			},
			gitRepoTimeout: &metav1.Duration{Duration: 30 * time.Second},
			wantErr:        false,
			wantCheckoutRef: &sourcev1.GitRepositoryRef{
				Branch: "ccc",
			},
			wantPushBranch:   "ccc",
			wantSwitchBranch: false,
			wantTimeout:      &metav1.Duration{Duration: 30 * time.Second},
		},
		{
			name:        "bad git URL",
			gitSpec:     &imagev1.GitSpec{},
			gitRepoName: testGitRepoName,
			gitRepoURL:  "://example.com",
			gitRepoRef: &sourcev1.GitRepositoryRef{
				Branch: "ccc",
			},
			wantErr: true,
		},
		{
			name:        "proxy config",
			gitSpec:     &imagev1.GitSpec{},
			gitRepoName: testGitRepoName,
			gitRepoURL:  testGitURL,
			gitRepoRef: &sourcev1.GitRepositoryRef{
				Branch: "ccc",
			},
			gitRepoProxyData: map[string][]byte{
				"address": []byte("http://example.com"),
			},
			wantErr: false,
			wantCheckoutRef: &sourcev1.GitRepositoryRef{
				Branch: "ccc",
			},
			wantPushBranch:   "ccc",
			wantSwitchBranch: false,
			wantTimeout:      testTimeout,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			testObjects := []client.Object{}

			var proxySecret *corev1.Secret
			if tt.gitRepoProxyData != nil {
				proxySecret = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "valid-proxy",
						Namespace: namespace,
					},
					Data: tt.gitRepoProxyData,
				}
				testObjects = append(testObjects, proxySecret)
			}

			var gitRepo *sourcev1.GitRepository
			if tt.gitRepoName != "" {
				gitRepo = &sourcev1.GitRepository{}
				gitRepo.Name = testGitRepoName
				gitRepo.Namespace = namespace
				gitRepo.Spec = sourcev1.GitRepositorySpec{}
				if tt.gitRepoURL != "" {
					gitRepo.Spec.URL = tt.gitRepoURL
				}
				if tt.gitRepoRef != nil {
					gitRepo.Spec.Reference = tt.gitRepoRef
				}
				if tt.gitRepoTimeout != nil {
					gitRepo.Spec.Timeout = tt.gitRepoTimeout
				}
				if proxySecret != nil {
					gitRepo.Spec.ProxySecretRef = &meta.LocalObjectReference{Name: proxySecret.Name}
				}
				testObjects = append(testObjects, gitRepo)
			}

			clientBuilder := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(testObjects...)
			c := clientBuilder.Build()

			gitRepoKey := types.NamespacedName{
				Namespace: namespace,
				Name:      tt.gitRepoName,
			}

			updateAutoKey := types.NamespacedName{
				Namespace: namespace,
				Name:      "test-update",
			}

			gitSrcCfg, err := buildGitConfig(context.TODO(), c, updateAutoKey, gitRepoKey, tt.gitSpec, tt.srcOpts)
			if (err != nil) != tt.wantErr {
				g.Fail(fmt.Sprintf("unexpected error: %v", err))
				return
			}
			if err == nil {
				g.Expect(gitSrcCfg.checkoutRef).To(Equal(tt.wantCheckoutRef), "unexpected checkoutRef")
				g.Expect(gitSrcCfg.pushBranch).To(Equal(tt.wantPushBranch), "unexpected push branch")
				g.Expect(gitSrcCfg.switchBranch).To(Equal(tt.wantSwitchBranch), "unexpected switch branch")
				g.Expect(gitSrcCfg.timeout).To(Equal(tt.wantTimeout), "unexpected git operation timeout")
			}
		})
	}
}
