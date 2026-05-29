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
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1"
	"github.com/fluxcd/image-automation-controller/internal/testutil"
)

func Test_detectSigningType(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string][]byte
		typ     imagev1.SigningKeyType
		wantErr string
	}{
		{
			name: "gpg with git.asc passes",
			data: map[string][]byte{"git.asc": []byte("dummy")},
			typ:  imagev1.SigningKeyTypeGPG,
		},
		{
			name:    "gpg without git.asc errors",
			data:    map[string][]byte{"identity": []byte("dummy")},
			typ:     imagev1.SigningKeyTypeGPG,
			wantErr: "does not contain a 'git.asc' key",
		},
		{
			name: "ssh with identity passes",
			data: map[string][]byte{"identity": []byte("dummy")},
			typ:  imagev1.SigningKeyTypeSSH,
		},
		{
			name:    "ssh without identity errors",
			data:    map[string][]byte{"git.asc": []byte("dummy")},
			typ:     imagev1.SigningKeyTypeSSH,
			wantErr: "does not contain an 'identity' key",
		},
		{
			name: "empty type defaults to gpg",
			data: map[string][]byte{"git.asc": []byte("dummy")},
			typ:  "",
		},
		{
			name:    "empty type without git.asc errors",
			data:    map[string][]byte{"identity": []byte("dummy")},
			typ:     "",
			wantErr: "does not contain a 'git.asc' key",
		},
		{
			name:    "unknown type errors",
			data:    map[string][]byte{"git.asc": []byte("dummy")},
			typ:     imagev1.SigningKeyType("rot13"),
			wantErr: "unknown signing key type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			err := detectSigningType(tt.data, tt.typ, "secret-name")
			if tt.wantErr == "" {
				g.Expect(err).ToNot(HaveOccurred())
				return
			}
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring(tt.wantErr))
		})
	}
}

func Test_loadGPGSigner(t *testing.T) {
	t.Run("unencrypted key returns signer", func(t *testing.T) {
		g := NewWithT(t)

		_, keyBytes := testutil.GetSigningKeyPair(g, "")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "k"},
			Data: map[string][]byte{
				signingSecretKeyGPG: keyBytes,
			},
		}

		s, err := loadGPGSigner(secret)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(s).ToNot(BeNil())
	})

	t.Run("encrypted key with passphrase returns signer", func(t *testing.T) {
		g := NewWithT(t)

		passphrase := "abcde12345"
		_, keyBytes := testutil.GetSigningKeyPair(g, passphrase)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "k"},
			Data: map[string][]byte{
				signingSecretKeyGPG:        keyBytes,
				signingSecretPassphraseGPG: []byte(passphrase),
			},
		}

		s, err := loadGPGSigner(secret)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(s).ToNot(BeNil())
	})

	t.Run("encrypted key without passphrase errors", func(t *testing.T) {
		g := NewWithT(t)

		_, keyBytes := testutil.GetSigningKeyPair(g, "abcde12345")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "k"},
			Data: map[string][]byte{
				signingSecretKeyGPG: keyBytes,
			},
		}

		_, err := loadGPGSigner(secret)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("'passphrase' field present in secret"))
	})
}

func Test_loadSSHSigner(t *testing.T) {
	t.Run("unencrypted ed25519 key returns signer", func(t *testing.T) {
		g := NewWithT(t)

		pemBytes, _ := testutil.GetSSHSigningKey(g, "")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "k"},
			Data: map[string][]byte{
				signingSecretKeySSH: pemBytes,
			},
		}

		s, err := loadSSHSigner(secret)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(s).ToNot(BeNil())
	})

	t.Run("encrypted key with password returns signer", func(t *testing.T) {
		g := NewWithT(t)

		passphrase := "abcde12345"
		pemBytes, _ := testutil.GetSSHSigningKey(g, passphrase)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "k"},
			Data: map[string][]byte{
				signingSecretKeySSH:      pemBytes,
				signingSecretPasswordSSH: []byte(passphrase),
			},
		}

		s, err := loadSSHSigner(secret)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(s).ToNot(BeNil())
	})

	t.Run("encrypted key without password errors with expected message", func(t *testing.T) {
		g := NewWithT(t)

		pemBytes, _ := testutil.GetSSHSigningKey(g, "abcde12345")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "k"},
			Data: map[string][]byte{
				signingSecretKeySSH: pemBytes,
			},
		}

		_, err := loadSSHSigner(secret)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("'password' field present in secret"))
	})

	t.Run("malformed identity errors", func(t *testing.T) {
		g := NewWithT(t)

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "k"},
			Data: map[string][]byte{
				signingSecretKeySSH: []byte("not a real ssh key"),
			},
		}

		_, err := loadSSHSigner(secret)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("could not parse SSH signing key"))
	})
}
