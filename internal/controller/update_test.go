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
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	extgogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	. "github.com/onsi/gomega"
	"github.com/otiai10/copy"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	aclapi "github.com/fluxcd/pkg/apis/acl"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/gittestserver"
	"github.com/fluxcd/pkg/runtime/conditions"
	conditionscheck "github.com/fluxcd/pkg/runtime/conditions/check"
	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/fluxcd/pkg/ssh"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta2"
	"github.com/fluxcd/image-automation-controller/internal/source"
	"github.com/fluxcd/image-automation-controller/internal/testutil"
	"github.com/fluxcd/image-automation-controller/pkg/test"
)

const (
	originRemote       = "origin"
	timeout            = 10 * time.Second
	testAuthorName     = "Flux B Ot"
	testAuthorEmail    = "fluxbot@example.com"
	testCommitTemplate = `Commit summary

Automation: {{ .AutomationObject }}

Files:
{{ range $filename, $_ := .Updated.Files -}}
- {{ $filename }}
{{ end -}}

Objects:
{{ range $resource, $_ := .Updated.Objects -}}
{{ if eq $resource.Kind "Deployment" -}}
- {{ $resource.Kind | lower }} {{ $resource.Name | lower }}
{{ else -}}
- {{ $resource.Kind }} {{ $resource.Name }}
{{ end -}}
{{ end -}}

Images:
{{ range .Updated.Images -}}
- {{.}} ({{.Policy.Name}})
{{ end -}}
`
	testCommitMessageFmt = `Commit summary

Automation: %s/update-test

Files:
- deploy.yaml
Objects:
- deployment test
Images:
- helloworld:v1.0.0 (%s)
`
)

func TestImageUpdateAutomationReconciler_deleteBeforeFinalizer(t *testing.T) {
	g := NewWithT(t)

	namespace, err := testEnv.CreateNamespace(ctx, "imageupdate")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

	imageUpdate := &imagev1.ImageUpdateAutomation{}
	imageUpdate.Name = "test-imageupdate"
	imageUpdate.Namespace = namespace.Name
	imageUpdate.Spec = imagev1.ImageUpdateAutomationSpec{
		Interval: metav1.Duration{Duration: time.Hour},
		SourceRef: imagev1.CrossNamespaceSourceReference{
			Kind: "GitRepository",
			Name: "foo",
		},
	}
	// Add a test finalizer to prevent the object from getting deleted.
	imageUpdate.SetFinalizers([]string{"test-finalizer"})
	g.Expect(k8sClient.Create(ctx, imageUpdate)).NotTo(HaveOccurred())
	// Add deletion timestamp by deleting the object.
	g.Expect(k8sClient.Delete(ctx, imageUpdate)).NotTo(HaveOccurred())

	r := &ImageUpdateAutomationReconciler{
		Client:        k8sClient,
		EventRecorder: record.NewFakeRecorder(32),
	}
	// NOTE: Only a real API server responds with an error in this scenario.
	g.Eventually(func() error {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(imageUpdate)})
		return err
	}, timeout).Should(Succeed())
}

func TestImageUpdateAutomationReconciler_watchSourceAndLatestImage(t *testing.T) {
	g := NewWithT(t)

	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: "not-expected-to-exist",
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: "1.x",
			},
		},
	}
	fixture := "testdata/appconfig"
	latest := "helloworld:v1.0.0"

	namespace, err := testEnv.CreateNamespace(ctx, "image-auto-test")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

	testWithRepoAndImagePolicy(ctx, g, testEnv, namespace.Name, fixture, policySpec, latest, func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
		// Update the setter marker in the repo.
		policyKey := types.NamespacedName{
			Name:      s.imagePolicyName,
			Namespace: s.namespace,
		}
		_ = testutil.CommitInRepo(ctx, g, repoURL, s.branch, originRemote, "Install setter marker", func(tmp string) {
			g.Expect(testutil.ReplaceMarker(filepath.Join(tmp, "deploy.yaml"), policyKey)).To(Succeed())
		})

		// Create the automation object.
		updateStrategy := &imagev1.UpdateStrategy{
			Strategy: imagev1.UpdateStrategySetters,
		}
		err := createImageUpdateAutomation(ctx, testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, s.branch, "", testCommitTemplate, "", updateStrategy)
		g.Expect(err).ToNot(HaveOccurred())
		defer func() {
			g.Expect(deleteImageUpdateAutomation(ctx, testEnv, "update-test", s.namespace)).To(Succeed())
		}()

		var imageUpdate imagev1.ImageUpdateAutomation
		imageUpdateKey := types.NamespacedName{
			Namespace: s.namespace,
			Name:      "update-test",
		}

		// Let the image update be ready.
		g.Eventually(func() bool {
			if err := testEnv.Get(ctx, imageUpdateKey, &imageUpdate); err != nil {
				return false
			}
			return conditions.IsReady(&imageUpdate)
		}, timeout).Should(BeTrue())
		lastPushedCommit := imageUpdate.Status.LastPushCommit

		// Update ImagePolicy with new latest and wait for image update to
		// trigger.
		latest = "helloworld:v1.1.0"
		err = updateImagePolicyWithLatestImage(ctx, testEnv, s.imagePolicyName, s.namespace, latest)
		g.Expect(err).ToNot(HaveOccurred())

		g.Eventually(func() bool {
			if err := testEnv.Get(ctx, imageUpdateKey, &imageUpdate); err != nil {
				return false
			}
			ready := conditions.Get(&imageUpdate, meta.ReadyCondition)
			return ready.Status == metav1.ConditionTrue && imageUpdate.Status.LastPushCommit != lastPushedCommit
		}, timeout).Should(BeTrue())

		// Update GitRepo with bad config and wait for image update to fail.
		var gitRepo sourcev1.GitRepository
		gitRepoKey := types.NamespacedName{
			Name:      s.gitRepoName,
			Namespace: s.gitRepoNamespace,
		}
		g.Expect(testEnv.Get(ctx, gitRepoKey, &gitRepo)).To(Succeed())
		patch := client.MergeFrom(gitRepo.DeepCopy())
		gitRepo.Spec.SecretRef = &meta.LocalObjectReference{Name: "non-existing-secret"}
		g.Expect(testEnv.Patch(ctx, &gitRepo, patch)).To(Succeed())

		g.Eventually(func() bool {
			if err := testEnv.Get(ctx, imageUpdateKey, &imageUpdate); err != nil {
				return false
			}
			return conditions.IsFalse(&imageUpdate, meta.ReadyCondition)
		}, timeout).Should(BeTrue())
	})
}

func TestImageUpdateAutomationReconciler_suspended(t *testing.T) {
	g := NewWithT(t)

	updateKey := types.NamespacedName{
		Name:      "test-update",
		Namespace: "default",
	}
	update := &imagev1.ImageUpdateAutomation{
		Spec: imagev1.ImageUpdateAutomationSpec{
			Interval: metav1.Duration{Duration: time.Hour},
			Suspend:  true,
		},
	}
	update.Name = updateKey.Name
	update.Namespace = updateKey.Namespace

	// Add finalizer so that reconciliation reaches suspend check.
	controllerutil.AddFinalizer(update, imagev1.ImageUpdateAutomationFinalizer)

	builder := fakeclient.NewClientBuilder().WithScheme(testEnv.GetScheme())
	builder.WithObjects(update)

	r := ImageUpdateAutomationReconciler{
		Client: builder.Build(),
	}

	res, err := r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: updateKey})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(res.Requeue).ToNot(BeTrue())

	// Make sure no status was written.
	g.Expect(r.Get(context.TODO(), updateKey, update)).To(Succeed())
	g.Expect(update.Status.Conditions).To(HaveLen(0))
	g.Expect(update.Status.LastAutomationRunTime).To(BeNil())

	// Cleanup.
	g.Expect(r.Delete(ctx, update)).To(Succeed())
}

func TestImageUpdateAutomationReconciler_Reconcile(t *testing.T) {
	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: "not-expected-to-exist",
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: "1.x",
			},
		},
	}
	fixture := "testdata/appconfig"
	latest := "helloworld:v1.0.0"
	updateName := "test-update"

	t.Run("no gitspec results in stalled", func(t *testing.T) {
		g := NewWithT(t)

		namespace, err := testEnv.CreateNamespace(ctx, "test-update")
		g.Expect(err).ToNot(HaveOccurred())
		defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

		obj := &imagev1.ImageUpdateAutomation{}
		obj.Name = updateName
		obj.Namespace = namespace.Name
		obj.Spec = imagev1.ImageUpdateAutomationSpec{
			SourceRef: imagev1.CrossNamespaceSourceReference{
				Name: "non-existing",
				Kind: sourcev1.GitRepositoryKind,
			},
		}
		g.Expect(testEnv.Create(ctx, obj)).To(Succeed())
		defer func() {
			g.Expect(deleteImageUpdateAutomation(ctx, testEnv, obj.Name, obj.Namespace)).To(Succeed())
		}()

		expectedConditions := []metav1.Condition{
			*conditions.TrueCondition(meta.StalledCondition, imagev1.InvalidSourceConfigReason, "invalid source configuration"),
			*conditions.FalseCondition(meta.ReadyCondition, imagev1.InvalidSourceConfigReason, "invalid source configuration"),
		}
		g.Eventually(func(g Gomega) {
			g.Expect(testEnv.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			g.Expect(obj.Status.Conditions).To(conditions.MatchConditions(expectedConditions))
		}).Should(Succeed())

		// Check if the object status is valid.
		condns := &conditionscheck.Conditions{NegativePolarity: imageUpdateAutomationNegativeConditions}
		checker := conditionscheck.NewChecker(testEnv.Client, condns)
		checker.WithT(g).CheckErr(ctx, obj)
	})

	t.Run("non-existing gitrepo results in failure", func(t *testing.T) {
		g := NewWithT(t)

		namespace, err := testEnv.CreateNamespace(ctx, "test-update")
		g.Expect(err).ToNot(HaveOccurred())
		defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

		obj := &imagev1.ImageUpdateAutomation{}
		obj.Name = updateName
		obj.Namespace = namespace.Name
		obj.Spec = imagev1.ImageUpdateAutomationSpec{
			SourceRef: imagev1.CrossNamespaceSourceReference{
				Name: "non-existing",
				Kind: sourcev1.GitRepositoryKind,
			},
			GitSpec: &imagev1.GitSpec{
				Commit: imagev1.CommitSpec{
					Author: imagev1.CommitUser{
						Email: "aaa",
					},
				},
			},
		}
		g.Expect(testEnv.Create(ctx, obj)).To(Succeed())
		defer func() {
			g.Expect(deleteImageUpdateAutomation(ctx, testEnv, obj.Name, obj.Namespace)).To(Succeed())
		}()

		expectedConditions := []metav1.Condition{
			*conditions.TrueCondition(meta.ReconcilingCondition, meta.ProgressingWithRetryReason, "processing"),
			*conditions.FalseCondition(meta.ReadyCondition, imagev1.SourceManagerFailedReason, "not found"),
		}
		g.Eventually(func(g Gomega) {
			g.Expect(testEnv.Get(ctx, client.ObjectKeyFromObject(obj), obj)).To(Succeed())
			g.Expect(obj.Status.Conditions).To(conditions.MatchConditions(expectedConditions))
		}).Should(Succeed())

		// Check if the object status is valid.
		condns := &conditionscheck.Conditions{NegativePolarity: imageUpdateAutomationNegativeConditions}
		checker := conditionscheck.NewChecker(testEnv.Client, condns)
		checker.WithT(g).CheckErr(ctx, obj)
	})

	t.Run("source checkout fails", func(t *testing.T) {
		g := NewWithT(t)

		namespace, err := testEnv.CreateNamespace(ctx, "test-update")
		g.Expect(err).ToNot(HaveOccurred())
		defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

		testWithRepoAndImagePolicy(ctx, g, testEnv, namespace.Name, fixture, policySpec, latest,
			func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
				err := createImageUpdateAutomation(ctx, testEnv, updateName, s.namespace, s.gitRepoName, s.gitRepoNamespace, "bad-branch", s.branch, "", testCommitTemplate, "", nil)
				g.Expect(err).ToNot(HaveOccurred())
				defer func() {
					g.Expect(deleteImageUpdateAutomation(ctx, testEnv, updateName, s.namespace)).To(Succeed())
				}()

				objKey := types.NamespacedName{
					Namespace: s.namespace,
					Name:      updateName,
				}
				var obj imagev1.ImageUpdateAutomation

				expectedConditions := []metav1.Condition{
					*conditions.TrueCondition(meta.ReconcilingCondition, meta.ProgressingWithRetryReason, "processing"),
					*conditions.FalseCondition(meta.ReadyCondition, imagev1.GitOperationFailedReason, "reference not found"),
				}
				g.Eventually(func(g Gomega) {
					g.Expect(testEnv.Get(ctx, objKey, &obj)).To(Succeed())
					g.Expect(obj.Status.Conditions).To(conditions.MatchConditions(expectedConditions))
				}).Should(Succeed())

				// Check if the object status is valid.
				condns := &conditionscheck.Conditions{NegativePolarity: imageUpdateAutomationNegativeConditions}
				checker := conditionscheck.NewChecker(testEnv.Client, condns)
				checker.WithT(g).CheckErr(ctx, &obj)
			})
	})

	t.Run("no marker no update", func(t *testing.T) {
		g := NewWithT(t)

		namespace, err := testEnv.CreateNamespace(ctx, "test-update")
		g.Expect(err).ToNot(HaveOccurred())
		defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

		testWithRepoAndImagePolicy(ctx, g, testEnv, namespace.Name, fixture, policySpec, latest,
			func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
				err := createImageUpdateAutomation(ctx, testEnv, updateName, s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, s.branch, "", testCommitTemplate, "", nil)
				g.Expect(err).ToNot(HaveOccurred())
				defer func() {
					g.Expect(deleteImageUpdateAutomation(ctx, testEnv, updateName, s.namespace)).To(Succeed())
				}()

				objKey := types.NamespacedName{
					Namespace: s.namespace,
					Name:      updateName,
				}
				var obj imagev1.ImageUpdateAutomation

				expectedConditions := []metav1.Condition{
					*conditions.TrueCondition(meta.ReadyCondition, meta.SucceededReason, readyMessage),
				}
				g.Eventually(func(g Gomega) {
					g.Expect(testEnv.Get(ctx, objKey, &obj)).To(Succeed())
					g.Expect(obj.Status.ObservedGeneration).To(Equal(obj.GetGeneration()))
					g.Expect(obj.Status.Conditions).To(conditions.MatchConditions(expectedConditions))
				}).Should(Succeed())

				g.Expect(obj.Status.LastPushCommit).To(BeEmpty())
				g.Expect(obj.Status.LastPushTime).To(BeNil())
				g.Expect(obj.Status.LastAutomationRunTime).ToNot(BeNil())
				g.Expect(obj.Status.ObservedSourceRevision).ToNot(BeEmpty())
				g.Expect(obj.Status.ObservedPolicies).To(HaveLen(1))

				// Check if the object status is valid.
				condns := &conditionscheck.Conditions{NegativePolarity: imageUpdateAutomationNegativeConditions}
				checker := conditionscheck.NewChecker(testEnv.Client, condns)
				checker.WithT(g).CheckErr(ctx, &obj)
			})
	})

	t.Run("push update", func(t *testing.T) {
		g := NewWithT(t)

		namespace, err := testEnv.CreateNamespace(ctx, "test-update")
		g.Expect(err).ToNot(HaveOccurred())
		defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

		testWithRepoAndImagePolicy(ctx, g, testEnv, namespace.Name, fixture, policySpec, latest,
			func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
				policyKey := types.NamespacedName{
					Name:      s.imagePolicyName,
					Namespace: s.namespace,
				}

				_ = testutil.CommitInRepo(ctx, g, repoURL, s.branch, originRemote, "Install setter marker", func(tmp string) {
					g.Expect(testutil.ReplaceMarker(filepath.Join(tmp, "deploy.yaml"), policyKey)).To(Succeed())
				})

				err := createImageUpdateAutomation(ctx, testEnv, updateName, s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, s.branch, "", testCommitTemplate, "", nil)
				g.Expect(err).ToNot(HaveOccurred())
				defer func() {
					g.Expect(deleteImageUpdateAutomation(ctx, testEnv, updateName, s.namespace)).To(Succeed())
				}()

				objKey := types.NamespacedName{
					Namespace: s.namespace,
					Name:      updateName,
				}
				var obj imagev1.ImageUpdateAutomation

				expectedConditions := []metav1.Condition{
					*conditions.TrueCondition(meta.ReadyCondition, meta.SucceededReason, readyMessage),
				}
				g.Eventually(func(g Gomega) {
					g.Expect(testEnv.Get(ctx, objKey, &obj)).To(Succeed())
					g.Expect(obj.Status.ObservedGeneration).To(Equal(obj.GetGeneration()))
					g.Expect(obj.Status.Conditions).To(conditions.MatchConditions(expectedConditions))
				}).Should(Succeed())
				g.Expect(obj.Status.LastPushCommit).ToNot(BeEmpty())
				g.Expect(obj.Status.LastPushTime).ToNot(BeNil())
				g.Expect(obj.Status.LastAutomationRunTime).ToNot(BeNil())
				g.Expect(obj.Status.ObservedSourceRevision).To(ContainSubstring("%s@sha1", s.branch))
				g.Expect(obj.Status.ObservedPolicies).To(HaveLen(1))

				// Check if the object status is valid.
				condns := &conditionscheck.Conditions{NegativePolarity: imageUpdateAutomationNegativeConditions}
				checker := conditionscheck.NewChecker(testEnv.Client, condns)
				checker.WithT(g).CheckErr(ctx, &obj)
			})
	})

	t.Run("source moves forward & policy updates separately, new observations", func(t *testing.T) {
		g := NewWithT(t)

		namespace, err := testEnv.CreateNamespace(ctx, "test-update")
		g.Expect(err).ToNot(HaveOccurred())
		defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

		testWithRepoAndImagePolicy(ctx, g, testEnv, namespace.Name, fixture, policySpec, latest,
			func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
				policyKey1 := types.NamespacedName{
					Name:      s.imagePolicyName,
					Namespace: s.namespace,
				}

				policyKey2 := types.NamespacedName{
					Name:      "non-existing-policy",
					Namespace: s.namespace,
				}

				_ = testutil.CommitInRepo(ctx, g, repoURL, s.branch, originRemote, "Install setter marker", func(tmp string) {
					g.Expect(testutil.ReplaceMarker(filepath.Join(tmp, "deploy.yaml"), policyKey1)).To(Succeed())
				})

				err := createImageUpdateAutomation(ctx, testEnv, updateName, s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, s.branch, "", testCommitTemplate, "", nil)
				g.Expect(err).ToNot(HaveOccurred())
				defer func() {
					g.Expect(deleteImageUpdateAutomation(ctx, testEnv, updateName, s.namespace)).To(Succeed())
				}()

				objKey := types.NamespacedName{
					Namespace: s.namespace,
					Name:      updateName,
				}
				var obj imagev1.ImageUpdateAutomation

				expectedConditions := []metav1.Condition{
					*conditions.TrueCondition(meta.ReadyCondition, meta.SucceededReason, readyMessage),
				}
				g.Eventually(func(g Gomega) {
					g.Expect(testEnv.Get(ctx, objKey, &obj)).To(Succeed())
					g.Expect(obj.Status.ObservedGeneration).To(Equal(obj.GetGeneration()))
					g.Expect(obj.Status.Conditions).To(conditions.MatchConditions(expectedConditions))
				}, timeout).Should(Succeed())
				g.Expect(obj.Status.LastAutomationRunTime).ToNot(BeNil())
				g.Expect(obj.Status.LastPushCommit).ToNot(BeEmpty())
				g.Expect(obj.Status.LastPushTime).ToNot(BeNil())
				g.Expect(obj.Status.LastAutomationRunTime).ToNot(BeNil())
				g.Expect(obj.Status.ObservedSourceRevision).To(ContainSubstring("%s@sha1", s.branch))
				g.Expect(obj.Status.ObservedPolicies).To(HaveLen(1))

				// Record the previous values and check after a reconciliation.
				//
				// NOTE: Ignoring LastAutomationRunTime as the recorded time is
				// only up to seconds. Because the test runs really quick, the
				// run time may be at the same second. Introducing a sleep for a
				// second shows that the time gets updated. Avoiding to
				// introduce a sleep to test this for now.
				srcRevBefore := obj.Status.ObservedSourceRevision
				pushCommitBefore := obj.Status.LastPushCommit
				pushTimeBefore := obj.Status.LastPushTime

				// Annotate the object and trigger a no-op reconciliation.
				patch := client.MergeFrom(obj.DeepCopy())
				obj.SetAnnotations(map[string]string{meta.ReconcileRequestAnnotation: "now"})
				g.Expect(testEnv.Patch(ctx, &obj, patch)).To(Succeed())

				// Look for the LastHandledReconcileAt to update.
				g.Eventually(func(g Gomega) {
					g.Expect(testEnv.Get(ctx, objKey, &obj)).To(Succeed())
					g.Expect(conditions.IsReady(&obj)).To(BeTrue())
					g.Expect(obj.Status.LastHandledReconcileAt).To(Equal("now"))
				}, timeout).Should(Succeed())
				// Nothing else should change.
				g.Expect(obj.Status.ObservedSourceRevision).To(Equal(srcRevBefore))
				g.Expect(obj.Status.LastPushCommit).To(Equal(pushCommitBefore))
				g.Expect(obj.Status.LastPushTime).To(Equal(pushTimeBefore))

				// Push a new commit such that there's no new update and
				// reconcile again.
				_ = testutil.CommitInRepo(ctx, g, repoURL, s.branch, originRemote, "Update setter marker", func(tmp string) {
					marker := fmt.Sprintf(`{"$imagepolicy": "%s:%s"}`, policyKey1.Namespace, policyKey1.Name)
					g.Expect(testutil.ReplaceMarkerWithMarker(filepath.Join(tmp, "deploy.yaml"), policyKey2, marker))
				})
				patch = client.MergeFrom(obj.DeepCopy())
				obj.SetAnnotations(map[string]string{meta.ReconcileRequestAnnotation: "nownow"})
				g.Expect(testEnv.Patch(ctx, &obj, patch)).To(Succeed())

				// Look for the ObservedSourceRevision to update.
				g.Eventually(func(g Gomega) {
					g.Expect(testEnv.Get(ctx, objKey, &obj)).To(Succeed())
					g.Expect(conditions.IsReady(&obj)).To(BeTrue())
					g.Expect(obj.Status.ObservedSourceRevision).ToNot(Equal(srcRevBefore))
				}, timeout).Should(Succeed())
				observedPoliciesBefore := obj.Status.ObservedPolicies
				srcRevBefore = obj.Status.ObservedSourceRevision

				// Update the policy, there will be no new update due to the
				// setter set above, reconcile again.
				latest = "helloworld:v2.0.0"
				g.Expect(updateImagePolicyWithLatestImage(ctx, testEnv, s.imagePolicyName, s.namespace, latest)).To(Succeed())

				// Look for the ObservedPolicies to update.
				g.Eventually(func(g Gomega) {
					g.Expect(testEnv.Get(ctx, objKey, &obj)).To(Succeed())
					g.Expect(conditions.IsReady(&obj)).To(BeTrue())
					g.Expect(obj.Status.ObservedPolicies).ToNot(Equal(observedPoliciesBefore))
				}, timeout).Should(Succeed())
				g.Expect(obj.Status.ObservedSourceRevision).To(Equal(srcRevBefore))
			})
	})

	t.Run("error recovery with early return", func(t *testing.T) {
		g := NewWithT(t)

		namespace, err := testEnv.CreateNamespace(ctx, "test-update")
		g.Expect(err).ToNot(HaveOccurred())
		defer func() {
			g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed())
		}()

		testWithRepoAndImagePolicy(ctx, g, testEnv, namespace.Name, fixture, policySpec, latest,
			func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
				err := createImageUpdateAutomation(ctx, testEnv, updateName, s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, s.branch, "", testCommitTemplate, "", nil)
				g.Expect(err).ToNot(HaveOccurred())
				defer func() {
					g.Expect(deleteImageUpdateAutomation(ctx, testEnv, updateName, s.namespace)).To(Succeed())
				}()

				objKey := types.NamespacedName{
					Namespace: s.namespace,
					Name:      updateName,
				}
				var obj imagev1.ImageUpdateAutomation

				// Ensure the image update is ready.
				g.Eventually(func(g Gomega) {
					g.Expect(testEnv.Get(ctx, objKey, &obj)).To(Succeed())
					g.Expect(obj.Status.ObservedGeneration).To(Equal(obj.GetGeneration()))
					g.Expect(conditions.IsReady(&obj))
				}, timeout).Should(Succeed())

				g.Expect(obj.Status.ObservedSourceRevision).ToNot(BeEmpty())

				// Update the GitRepository to add a non-existing secret ref.
				gitRepoKey := types.NamespacedName{
					Namespace: s.gitRepoNamespace,
					Name:      s.gitRepoName,
				}
				var gitRepo sourcev1.GitRepository
				g.Expect(testEnv.Get(ctx, gitRepoKey, &gitRepo)).To(Succeed())
				patch := client.MergeFrom(gitRepo.DeepCopy())
				gitRepo.Spec.SecretRef = &meta.LocalObjectReference{Name: "non-existing-git-sec"}
				g.Expect(testEnv.Patch(ctx, &gitRepo, patch)).To(Succeed())

				// Wait for image update to fail.
				g.Eventually(func(g Gomega) {
					g.Expect(testEnv.Get(ctx, objKey, &obj)).To(Succeed())
					g.Expect(conditions.IsReady(&obj)).To(BeFalse())
				}, timeout).Should(Succeed())

				// Patch the GitRepository to remove the secret ref.
				patch = client.MergeFrom(gitRepo.DeepCopy())
				gitRepo.Spec.SecretRef = nil
				g.Expect(testEnv.Patch(ctx, &gitRepo, patch)).To(Succeed())

				// Wait for image update to recover from failure.
				g.Eventually(func(g Gomega) {
					g.Expect(testEnv.Get(ctx, objKey, &obj)).To(Succeed())
					g.Expect(conditions.IsReady(&obj)).To(BeTrue())
				}, timeout).Should(Succeed())
			})
	})
}

func TestImageUpdateAutomationReconciler_commitMessage(t *testing.T) {
	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: "not-expected-to-exist",
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: "1.x",
			},
		},
	}
	fixture := "testdata/appconfig"
	latest := "helloworld:v1.0.0"

	tests := []struct {
		name          string
		template      string
		wantCommitMsg func(policyName, policyNS string) string
	}{
		{
			name:     "template with update Result",
			template: testCommitTemplate,
			wantCommitMsg: func(policyName, policyNS string) string {
				return fmt.Sprintf(testCommitMessageFmt, policyNS, policyName)
			},
		},
		{
			name: "template with update ResultV2",
			template: `Commit summary with ResultV2

Automation: {{ .AutomationObject }}

{{ range $filename, $objchange := .Changed.FileChanges -}}
- File: {{ $filename }}
{{- range $obj, $changes := $objchange }}
  - Object: {{ $obj.Kind }}/{{ $obj.Namespace }}/{{ $obj.Name }}
    Changes:
{{- range $_ , $change := $changes }}
    - {{ $change.OldValue }} -> {{ $change.NewValue }} ({{ $change.Setter }})
{{ end -}}
{{ end -}}
{{ end -}}
`,
			wantCommitMsg: func(policyName, policyNS string) string {
				return fmt.Sprintf(`Commit summary with ResultV2

Automation: %s/update-test

- File: deploy.yaml
  - Object: Deployment//test
    Changes:
    - helloworld:1.0.0 -> helloworld:v1.0.0 (%s:%s)
`, policyNS, policyNS, policyName)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create test namespace.
			namespace, err := testEnv.CreateNamespace(ctx, "image-auto-test")
			g.Expect(err).ToNot(HaveOccurred())
			defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

			testWithRepoAndImagePolicy(
				ctx, g, testEnv, namespace.Name, fixture, policySpec, latest,
				func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
					commitMessage := tt.wantCommitMsg(s.imagePolicyName, s.namespace)

					// Update the setter marker in the repo.
					policyKey := types.NamespacedName{
						Name:      s.imagePolicyName,
						Namespace: s.namespace,
					}
					_ = testutil.CommitInRepo(ctx, g, repoURL, s.branch, originRemote, "Install setter marker", func(tmp string) {
						g.Expect(testutil.ReplaceMarker(filepath.Join(tmp, "deploy.yaml"), policyKey)).To(Succeed())
					})

					// Pull the head commit we just pushed, so it's not
					// considered a new commit when checking for a commit
					// made by automation.
					preChangeCommitId := testutil.CommitIdFromBranch(localRepo, s.branch)

					// Pull the head commit that was just pushed, so it's not considered a new
					// commit when checking for a commit made by automation.
					waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

					preChangeCommitId = testutil.CommitIdFromBranch(localRepo, s.branch)

					// Create the automation object and let it make a commit itself.
					updateStrategy := &imagev1.UpdateStrategy{
						Strategy: imagev1.UpdateStrategySetters,
					}
					err := createImageUpdateAutomation(ctx, testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, s.branch, "", tt.template, "", updateStrategy)
					g.Expect(err).ToNot(HaveOccurred())
					defer func() {
						g.Expect(deleteImageUpdateAutomation(ctx, testEnv, "update-test", s.namespace)).To(Succeed())
					}()

					// Wait for a new commit to be made by the controller.
					waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

					head, _ := localRepo.Head()
					commit, err := localRepo.CommitObject(head.Hash())
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(commit.Message).To(Equal(commitMessage))

					signature := commit.Author
					g.Expect(signature).NotTo(BeNil())
					g.Expect(signature.Name).To(Equal(testAuthorName))
					g.Expect(signature.Email).To(Equal(testAuthorEmail))

					// Regression check to ensure the status message contains the branch name
					// if checkout branch is the same as push branch.
					imageUpdateKey := types.NamespacedName{
						Namespace: s.namespace,
						Name:      "update-test",
					}
					var imageUpdate imagev1.ImageUpdateAutomation
					_ = testEnv.Get(context.TODO(), imageUpdateKey, &imageUpdate)
					ready := apimeta.FindStatusCondition(imageUpdate.Status.Conditions, meta.ReadyCondition)
					g.Expect(ready.Message).To(Equal(readyMessage))
					g.Expect(imageUpdate.Status.LastPushCommit).To(Equal(head.Hash().String()))

					// Check if the object status is valid.
					condns := &conditionscheck.Conditions{NegativePolarity: imageUpdateAutomationNegativeConditions}
					checker := conditionscheck.NewChecker(testEnv.Client, condns)
					checker.WithT(g).CheckErr(ctx, &imageUpdate)
				},
			)
		})

	}
}

func TestImageUpdateAutomationReconciler_crossNamespaceRef(t *testing.T) {
	g := NewWithT(t)
	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: "not-expected-to-exist",
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: "1.x",
			},
		},
	}
	fixture := "testdata/appconfig"
	latest := "helloworld:v1.0.0"

	// Test successful cross namespace reference when NoCrossNamespaceRef=false.

	// Create test namespace.
	namespace1, err := testEnv.CreateNamespace(ctx, "image-auto-test")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { g.Expect(testEnv.Delete(ctx, namespace1)).To(Succeed()) }()

	args := newRepoAndPolicyArgs(namespace1.Name)

	// Create another test namespace.
	namespace2, err := testEnv.CreateNamespace(ctx, "image-auto-test")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { g.Expect(testEnv.Delete(ctx, namespace2)).To(Succeed()) }()

	args.gitRepoNamespace = namespace2.Name

	testWithCustomRepoAndImagePolicy(
		ctx, g, testEnv, fixture, policySpec, latest, args,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
			commitMessage := fmt.Sprintf(testCommitMessageFmt, s.namespace, s.imagePolicyName)

			// Update the setter marker in the repo.
			policyKey := types.NamespacedName{
				Name:      s.imagePolicyName,
				Namespace: s.namespace,
			}
			_ = testutil.CommitInRepo(ctx, g, repoURL, s.branch, originRemote, "Install setter marker", func(tmp string) {
				g.Expect(testutil.ReplaceMarker(filepath.Join(tmp, "deploy.yaml"), policyKey)).To(Succeed())
			})

			// Pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId := testutil.CommitIdFromBranch(localRepo, s.branch)

			// Pull the head commit that was just pushed, so it's not considered a new
			// commit when checking for a commit made by automation.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			preChangeCommitId = testutil.CommitIdFromBranch(localRepo, s.branch)

			// Create the automation object and let it make a commit itself.
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			}
			err := createImageUpdateAutomation(ctx, testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", "", testCommitTemplate, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())
			defer func() {
				g.Expect(deleteImageUpdateAutomation(ctx, testEnv, "update-test", s.namespace)).To(Succeed())
			}()

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Message).To(Equal(commitMessage))

			signature := commit.Author
			g.Expect(signature).NotTo(BeNil())
			g.Expect(signature.Name).To(Equal(testAuthorName))
			g.Expect(signature.Email).To(Equal(testAuthorEmail))
		},
	)

	// Test cross namespace reference failure when NoCrossNamespaceRef=true.
	r := &ImageUpdateAutomationReconciler{
		Client: fakeclient.NewClientBuilder().
			WithScheme(testEnv.Scheme()).
			WithStatusSubresource(&imagev1.ImageUpdateAutomation{}, &imagev1_reflect.ImagePolicy{}).
			Build(),
		EventRecorder:       testEnv.GetEventRecorderFor("image-automation-controller"),
		NoCrossNamespaceRef: true,
	}

	// Create test namespace.
	namespace3, err := testEnv.CreateNamespace(ctx, "image-auto-test")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { g.Expect(testEnv.Delete(ctx, namespace3)).To(Succeed()) }()

	// Test successful cross namespace reference when NoCrossNamespaceRef=false.
	args = newRepoAndPolicyArgs(namespace3.Name)

	// Create another test namespace.
	namespace4, err := testEnv.CreateNamespace(ctx, "image-auto-test")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { g.Expect(testEnv.Delete(ctx, namespace4)).To(Succeed()) }()

	args.gitRepoNamespace = namespace4.Name

	testWithCustomRepoAndImagePolicy(
		ctx, g, r.Client, fixture, policySpec, latest, args,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			}
			err := createImageUpdateAutomation(ctx, r.Client, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", "", testCommitTemplate, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())

			imageUpdateKey := types.NamespacedName{
				Name:      "update-test",
				Namespace: s.namespace,
			}
			var imageUpdate imagev1.ImageUpdateAutomation
			_ = r.Client.Get(context.TODO(), imageUpdateKey, &imageUpdate)

			sp := patch.NewSerialPatcher(&imageUpdate, r.Client)

			_, err = r.reconcile(context.TODO(), sp, &imageUpdate, time.Now())
			g.Expect(err).To(BeNil())

			ready := apimeta.FindStatusCondition(imageUpdate.Status.Conditions, meta.ReadyCondition)
			g.Expect(ready.Reason).To(Equal(aclapi.AccessDeniedReason))
		},
	)
}

func TestImageUpdateAutomationReconciler_updatePath(t *testing.T) {
	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: "not-expected-to-exist",
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: "1.x",
			},
		},
	}
	fixture := "testdata/pathconfig"
	latest := "helloworld:v1.0.0"

	g := NewWithT(t)

	// Create test namespace.
	namespace, err := testEnv.CreateNamespace(ctx, "image-auto-test")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

	testWithRepoAndImagePolicy(
		ctx, g, testEnv, namespace.Name, fixture, policySpec, latest,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
			// Update the setter marker in the repo.
			policyKey := types.NamespacedName{
				Name:      s.imagePolicyName,
				Namespace: s.namespace,
			}

			// pull the head commit we just pushed, so it's not
			// considered a new commit when checking for a commit
			// made by automation.
			preChangeCommitId := testutil.CommitIdFromBranch(localRepo, s.branch)

			_ = testutil.CommitInRepo(ctx, g, repoURL, s.branch, originRemote, "Install setter marker", func(tmp string) {
				g.Expect(testutil.ReplaceMarker(filepath.Join(tmp, "yes", "deploy.yaml"), policyKey)).To(Succeed())
			})
			_ = testutil.CommitInRepo(ctx, g, repoURL, s.branch, originRemote, "Install setter marker", func(tmp string) {
				g.Expect(testutil.ReplaceMarker(filepath.Join(tmp, "no", "deploy.yaml"), policyKey)).To(Succeed())
			})

			// Pull the head commit that was just pushed, so it's not considered a new
			// commit when checking for a commit made by automation.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			preChangeCommitId = testutil.CommitIdFromBranch(localRepo, s.branch)

			// Create the automation object and let it make a commit itself.
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
				Path:     "./yes",
			}
			err := createImageUpdateAutomation(ctx, testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", "", testCommitTemplate, "", updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())
			defer func() {
				g.Expect(deleteImageUpdateAutomation(ctx, testEnv, "update-test", s.namespace)).To(Succeed())
			}()

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			head, _ := localRepo.Head()
			commit, err := localRepo.CommitObject(head.Hash())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(commit.Message).ToNot(ContainSubstring("update-no"))
			g.Expect(commit.Message).To(ContainSubstring("update-yes"))

			var update imagev1.ImageUpdateAutomation
			updateKey := types.NamespacedName{
				Namespace: s.namespace,
				Name:      "update-test",
			}
			g.Expect(testEnv.Get(ctx, updateKey, &update)).To(Succeed())
			// Check if the object status is valid.
			condns := &conditionscheck.Conditions{NegativePolarity: imageUpdateAutomationNegativeConditions}
			checker := conditionscheck.NewChecker(testEnv.Client, condns)
			checker.WithT(g).CheckErr(ctx, &update)
		},
	)
}

func TestImageUpdateAutomationReconciler_signedCommit(t *testing.T) {
	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: "not-expected-to-exist",
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: "1.x",
			},
		},
	}
	fixture := "testdata/appconfig"
	latest := "helloworld:v1.0.0"

	g := NewWithT(t)

	// Create test namespace.
	namespace, err := testEnv.CreateNamespace(ctx, "image-auto-test")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

	testWithRepoAndImagePolicy(
		ctx, g, testEnv, namespace.Name, fixture, policySpec, latest,
		func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository) {
			signingKeySecretName := "signing-key-secret-" + rand.String(5)
			// Update the setter marker in the repo.
			policyKey := types.NamespacedName{
				Name:      s.imagePolicyName,
				Namespace: s.namespace,
			}
			_ = testutil.CommitInRepo(ctx, g, repoURL, s.branch, originRemote, "Install setter marker", func(tmp string) {
				g.Expect(testutil.ReplaceMarker(filepath.Join(tmp, "deploy.yaml"), policyKey)).To(Succeed())
			})

			preChangeCommitId := testutil.CommitIdFromBranch(localRepo, s.branch)

			// Pull the head commit that was just pushed, so it's not considered a new
			// commit when checking for a commit made by automation.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			pgpEntity := createSigningKeyPairSecret(ctx, g, testEnv, signingKeySecretName, s.namespace)

			preChangeCommitId = testutil.CommitIdFromBranch(localRepo, s.branch)

			// Create the automation object and let it make a commit itself.
			updateStrategy := &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			}
			err := createImageUpdateAutomation(ctx, testEnv, "update-test", s.namespace, s.gitRepoName, s.gitRepoNamespace, s.branch, "", "", testCommitTemplate, signingKeySecretName, updateStrategy)
			g.Expect(err).ToNot(HaveOccurred())
			defer func() {
				g.Expect(deleteImageUpdateAutomation(ctx, testEnv, "update-test", s.namespace)).To(Succeed())
			}()

			// Wait for a new commit to be made by the controller.
			waitForNewHead(g, localRepo, s.branch, preChangeCommitId)

			head, _ := localRepo.Head()
			g.Expect(err).ToNot(HaveOccurred())
			commit, err := localRepo.CommitObject(head.Hash())
			g.Expect(err).ToNot(HaveOccurred())

			c2 := *commit
			c2.PGPSignature = ""

			encoded := &plumbing.MemoryObject{}
			err = c2.Encode(encoded)
			g.Expect(err).ToNot(HaveOccurred())
			content, err := encoded.Reader()
			g.Expect(err).ToNot(HaveOccurred())

			kr := openpgp.EntityList([]*openpgp.Entity{pgpEntity})
			signature := strings.NewReader(commit.PGPSignature)

			_, err = openpgp.CheckArmoredDetachedSignature(kr, content, signature, nil)
			g.Expect(err).ToNot(HaveOccurred())
		},
	)
}

func TestImageUpdateAutomationReconciler_e2e(t *testing.T) {
	protos := []string{"http", "ssh"}

	testFunc := func(t *testing.T, proto string) {
		g := NewWithT(t)

		const latestImage = "helloworld:1.0.1"

		// Create a test namespace.
		namespace, err := testEnv.CreateNamespace(ctx, "image-auto-test")
		g.Expect(err).ToNot(HaveOccurred())
		defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

		branch := rand.String(8)
		repositoryPath := "/config-" + rand.String(6) + ".git"
		gitRepoName := "image-auto-" + rand.String(5)
		gitSecretName := "git-secret-" + rand.String(5)
		imagePolicyName := "policy-" + rand.String(5)
		updateStrategy := &imagev1.UpdateStrategy{
			Strategy: imagev1.UpdateStrategySetters,
		}

		// Create git server.
		gitServer := testutil.SetUpGitTestServer(g)
		defer os.RemoveAll(gitServer.Root())
		defer gitServer.StopHTTP()

		cloneLocalRepoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
		repoURL, err := getRepoURL(gitServer, repositoryPath, proto)
		g.Expect(err).ToNot(HaveOccurred())

		// Start the ssh server if needed.
		if proto == "ssh" {
			go func() {
				gitServer.StartSSH()
			}()
			defer func() {
				g.Expect(gitServer.StopSSH()).To(Succeed())
			}()
		}

		commitMessage := "Commit a difference " + rand.String(5)

		// Initialize a git repo.
		_ = testutil.InitGitRepo(g, gitServer, "testdata/appconfig", branch, repositoryPath)

		// Create GitRepository resource for the above repo.
		if proto == "ssh" {
			// SSH requires an identity (private key) and known_hosts file
			// in a secret.
			err = createSSHIdentitySecret(testEnv, gitSecretName, namespace.Name, repoURL)
			g.Expect(err).ToNot(HaveOccurred())
			err = createGitRepository(ctx, testEnv, gitRepoName, namespace.Name, repoURL, gitSecretName)
			g.Expect(err).ToNot(HaveOccurred())
		} else {
			err = createGitRepository(ctx, testEnv, gitRepoName, namespace.Name, repoURL, "")
			g.Expect(err).ToNot(HaveOccurred())
		}

		// Create an image policy.
		policyKey := types.NamespacedName{
			Name:      imagePolicyName,
			Namespace: namespace.Name,
		}

		// Clone the repo locally.
		cloneCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		localRepo, cloneDir, err := testutil.Clone(cloneCtx, cloneLocalRepoURL, branch, originRemote)
		g.Expect(err).ToNot(HaveOccurred(), "failed to clone")
		defer func() { os.RemoveAll(cloneDir) }()

		testutil.CheckoutBranch(g, localRepo, branch)
		err = createImagePolicyWithLatestImage(ctx, testEnv, imagePolicyName, namespace.Name, "not-expected-to-exist", "1.x", latestImage)
		g.Expect(err).ToNot(HaveOccurred(), "failed to create ImagePolicy resource")

		defer func() {
			g.Expect(deleteImagePolicy(ctx, testEnv, imagePolicyName, namespace.Name)).ToNot(HaveOccurred())
		}()

		preChangeCommitId := testutil.CommitIdFromBranch(localRepo, branch)
		// Insert a setter reference into the deployment file,
		// before creating the automation object itself.
		_ = testutil.CommitInRepo(ctx, g, cloneLocalRepoURL, branch, originRemote, "Install setter marker", func(tmp string) {
			g.Expect(testutil.ReplaceMarker(filepath.Join(tmp, "deploy.yaml"), policyKey)).To(Succeed())
		})

		// Pull the head commit we just pushed, so it's not
		// considered a new commit when checking for a commit
		// made by automation.
		waitForNewHead(g, localRepo, branch, preChangeCommitId)

		preChangeCommitId = testutil.CommitIdFromBranch(localRepo, branch)

		// Now create the automation object, and let it make a commit itself.
		updateKey := types.NamespacedName{
			Namespace: namespace.Name,
			Name:      "update-" + rand.String(5),
		}
		err = createImageUpdateAutomation(ctx, testEnv, updateKey.Name, namespace.Name, gitRepoName, namespace.Name, branch, "", "", commitMessage, "", updateStrategy)
		g.Expect(err).ToNot(HaveOccurred())
		defer func() {
			g.Expect(deleteImageUpdateAutomation(ctx, testEnv, updateKey.Name, namespace.Name)).To(Succeed())
		}()

		var imageUpdate imagev1.ImageUpdateAutomation
		g.Eventually(func() bool {
			if err := testEnv.Get(ctx, updateKey, &imageUpdate); err != nil {
				return false
			}
			return conditions.IsReady(&imageUpdate) && imageUpdate.Status.LastPushCommit != ""
		}, timeout).Should(BeTrue())

		// Wait for a new commit to be made by the controller.
		waitForNewHead(g, localRepo, branch, preChangeCommitId)

		// Check if the repo head matches with the ImageUpdateAutomation
		// last push commit status.
		head, _ := localRepo.Head()
		commit, err := localRepo.CommitObject(head.Hash())
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(commit.Message).To(Equal(commitMessage))
		g.Expect(commit.Hash.String()).To(Equal(imageUpdate.Status.LastPushCommit))

		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		compareRepoWithExpected(checkCtx, g, cloneLocalRepoURL, branch, "testdata/appconfig-setters-expected", func(tmp string) {
			g.Expect(testutil.ReplaceMarker(filepath.Join(tmp, "deploy.yaml"), policyKey)).To(Succeed())
		})

		// Check if the object status is valid.
		condns := &conditionscheck.Conditions{NegativePolarity: imageUpdateAutomationNegativeConditions}
		checker := conditionscheck.NewChecker(testEnv.Client, condns)
		checker.WithT(g).CheckErr(ctx, &imageUpdate)
	}

	for _, proto := range protos {
		t.Run(proto, func(t *testing.T) {
			testFunc(t, proto)
		})
	}
}

func TestImageUpdateAutomationReconciler_defaulting(t *testing.T) {
	g := NewWithT(t)

	branch := rand.String(8)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create a test namespace.
	namespace, err := testEnv.CreateNamespace(ctx, "image-auto-test")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() { g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed()) }()

	// Create an instance of ImageUpdateAutomation.
	key := types.NamespacedName{
		Name:      "update-" + rand.String(5),
		Namespace: namespace.Name,
	}
	auto := &imagev1.ImageUpdateAutomation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: imagev1.ImageUpdateAutomationSpec{
			SourceRef: imagev1.CrossNamespaceSourceReference{
				Kind: "GitRepository",
				Name: "garbage",
			},
			Interval: metav1.Duration{Duration: 2 * time.Hour}, // this is to ensure any subsequent run should be outside the scope of the testing
			GitSpec: &imagev1.GitSpec{
				Checkout: &imagev1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{
						Branch: branch,
					},
				},
				// leave Update field out
				Commit: imagev1.CommitSpec{
					Author: imagev1.CommitUser{
						Email: testAuthorEmail,
					},
					MessageTemplate: "nothing",
				},
			},
		},
	}
	g.Expect(testEnv.Create(ctx, auto)).To(Succeed())
	defer func() {
		g.Expect(testEnv.Delete(ctx, auto)).To(Succeed())
	}()

	// Should default .spec.update to {strategy: Setters}.
	var fetchedAuto imagev1.ImageUpdateAutomation
	g.Eventually(func() bool {
		err := testEnv.Get(ctx, key, &fetchedAuto)
		return err == nil
	}, timeout, time.Second).Should(BeTrue())
	g.Expect(fetchedAuto.Spec.Update).
		To(Equal(&imagev1.UpdateStrategy{Strategy: imagev1.UpdateStrategySetters}))
}

func TestImageUpdateAutomationReconciler_notify(t *testing.T) {
	g := NewWithT(t)
	testPushResult, err := source.NewPushResult("branch", "rev", "test commit message")
	g.Expect(err).ToNot(HaveOccurred())

	tests := []struct {
		name             string
		pushResult       *source.PushResult
		syncNeeded       bool
		oldObjBeforeFunc func(obj conditions.Setter)
		newObjBeforeFunc func(obj conditions.Setter)
		wantEvent        string
	}{
		{
			name:       "first time reconciliation, no update",
			pushResult: nil,
			syncNeeded: true,
			newObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, readyMessage)
			},
			wantEvent: "Normal Succeeded repository up-to-date",
		},
		{
			name:       "second reconciliation, syncNeeded=false, no update",
			pushResult: nil,
			syncNeeded: false,
			oldObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, readyMessage)
			},
			newObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, readyMessage)
			},
			wantEvent: "Trace Succeeded no change since last reconciliation",
		},
		{
			name:       "second reconciliation, syncNeeded=true, no update",
			pushResult: nil,
			syncNeeded: true,
			oldObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, readyMessage)
			},
			newObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, readyMessage)
			},
			wantEvent: "Trace Succeeded repository up-to-date",
		},
		{
			name:       "was ready, new update, is ready",
			pushResult: testPushResult,
			syncNeeded: true,
			oldObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, readyMessage)
			},
			newObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, readyMessage)
			},
			wantEvent: "Normal Succeeded pushed commit 'rev' to branch 'branch'\ntest commit message",
		},
		{
			name:       "failure recovery, no update",
			pushResult: nil,
			syncNeeded: true,
			oldObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkFalse(obj, meta.ReadyCondition, meta.FailedReason, "failed to checkout source")
			},
			newObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, readyMessage)
			},
			wantEvent: "Normal Succeeded repository up-to-date",
		},
		{
			name:       "failure recovery, with new update",
			pushResult: testPushResult,
			syncNeeded: true,
			oldObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkFalse(obj, meta.ReadyCondition, meta.FailedReason, "failed to checkout source")
			},
			newObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, readyMessage)
			},
			wantEvent: "Normal Succeeded pushed commit 'rev' to branch 'branch'\ntest commit message",
		},
		{
			name:       "failed",
			pushResult: nil,
			syncNeeded: true,
			oldObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, readyMessage)
			},
			newObjBeforeFunc: func(obj conditions.Setter) {
				conditions.MarkFalse(obj, meta.ReadyCondition, imagev1.GitOperationFailedReason, "failed to checkout source")
			},
			wantEvent: "Warning GitOperationFailed failed to checkout source",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			recorder := record.NewFakeRecorder(32)

			oldObj := &imagev1.ImageUpdateAutomation{}
			newObj := oldObj.DeepCopy()

			if tt.oldObjBeforeFunc != nil {
				tt.oldObjBeforeFunc(oldObj)
			}
			if tt.newObjBeforeFunc != nil {
				tt.newObjBeforeFunc(newObj)
			}

			reconciler := &ImageUpdateAutomationReconciler{
				EventRecorder: recorder,
			}
			reconciler.notify(ctx, oldObj, newObj, tt.pushResult, tt.syncNeeded)

			select {
			case x, ok := <-recorder.Events:
				g.Expect(ok).To(Equal(tt.wantEvent != ""), "unexpected event received")
				if tt.wantEvent != "" {
					g.Expect(x).To(ContainSubstring(tt.wantEvent))
				}
			default:
				if tt.wantEvent != "" {
					g.Fail("expected some event to be emitted")
				}
			}
		})
	}
}

func Test_getPolicies(t *testing.T) {
	testNS1 := "foo"
	testNS2 := "bar"

	type policyArgs struct {
		name        string
		namespace   string
		latestImage string
	}

	tests := []struct {
		name          string
		listNamespace string
		policies      []policyArgs
		wantPolicies  []string
	}{
		{
			name:          "lists policies with image and in same namespace",
			listNamespace: testNS1,
			policies: []policyArgs{
				{name: "p1", namespace: testNS1, latestImage: "aaa:bbb"},
				{name: "p2", namespace: testNS1, latestImage: "ccc:ddd"},
				{name: "p3", namespace: testNS2, latestImage: "eee:fff"},
				{name: "p4", namespace: testNS1, latestImage: ""},
			},
			wantPolicies: []string{"p1", "p2"},
		},
		{
			name:          "no policies in empty namespace",
			listNamespace: testNS2,
			policies: []policyArgs{
				{name: "p1", namespace: testNS1, latestImage: "aaa:bbb"},
			},
			wantPolicies: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create all the test policies.
			testObjects := []client.Object{}
			for _, p := range tt.policies {
				aPolicy := &imagev1_reflect.ImagePolicy{}
				aPolicy.Name = p.name
				aPolicy.Namespace = p.namespace
				aPolicy.Status = imagev1_reflect.ImagePolicyStatus{
					LatestImage: p.latestImage,
				}
				testObjects = append(testObjects, aPolicy)
			}
			kClient := fakeclient.NewClientBuilder().
				WithScheme(testEnv.GetScheme()).
				WithObjects(testObjects...).Build()

			result, err := getPolicies(context.TODO(), kClient, tt.listNamespace)
			g.Expect(err).ToNot(HaveOccurred())

			// Extract policy name from the result and compare with the expected
			// result.
			resultPolicyNames := []string{}
			for _, r := range result {
				resultPolicyNames = append(resultPolicyNames, r.Name)
			}
			g.Expect(resultPolicyNames).To(ContainElements(tt.wantPolicies))
		})
	}
}

func Test_observedPolicies(t *testing.T) {
	tests := []struct {
		name            string
		policyWithImage map[string]string
		want            imagev1.ObservedPolicies
		wantErr         bool
	}{
		{
			name: "good policies",
			policyWithImage: map[string]string{
				"p1": "aaa:bbb",
				"p2": "ccc:ddd",
				"p3": "eee:latest",
				"p4": "fff:ggg:hhh",
			},
			want: imagev1.ObservedPolicies{
				"p1": imagev1.ImageRef{Name: "aaa", Tag: "bbb"},
				"p2": imagev1.ImageRef{Name: "ccc", Tag: "ddd"},
				"p3": imagev1.ImageRef{Name: "eee", Tag: "latest"},
				"p4": imagev1.ImageRef{Name: "fff", Tag: "ggg:hhh"},
			},
		},
		{
			name: "bad policy image with no tag",
			policyWithImage: map[string]string{
				"p1": "aaa",
			},
			wantErr: true,
		},
		{
			name: "no policy",
			want: imagev1.ObservedPolicies{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			policies := []imagev1_reflect.ImagePolicy{}
			for name, image := range tt.policyWithImage {
				aPolicy := imagev1_reflect.ImagePolicy{}
				aPolicy.Name = name
				aPolicy.Status = imagev1_reflect.ImagePolicyStatus{
					LatestImage: image,
				}
				policies = append(policies, aPolicy)
			}

			result, err := observedPolicies(policies)
			if (err != nil) != tt.wantErr {
				g.Fail(fmt.Sprintf("unexpected error: %v", err))
			}
			if err == nil {
				g.Expect(result).To(Equal(tt.want))
			}
		})
	}
}

func Test_observedPoliciesChanged(t *testing.T) {
	tests := []struct {
		name     string
		previous imagev1.ObservedPolicies
		current  imagev1.ObservedPolicies
		want     bool
	}{
		{
			name: "no change",
			previous: imagev1.ObservedPolicies{
				"p1": imagev1.ImageRef{Name: "aaa", Tag: "bbb"},
				"p2": imagev1.ImageRef{Name: "ccc", Tag: "ddd"},
			},
			current: imagev1.ObservedPolicies{
				"p1": imagev1.ImageRef{Name: "aaa", Tag: "bbb"},
				"p2": imagev1.ImageRef{Name: "ccc", Tag: "ddd"},
			},
			want: false,
		},
		{
			name: "change due to new tag",
			previous: imagev1.ObservedPolicies{
				"p1": imagev1.ImageRef{Name: "aaa", Tag: "bbb"},
				"p2": imagev1.ImageRef{Name: "ccc", Tag: "ddd"},
			},
			current: imagev1.ObservedPolicies{
				"p1": imagev1.ImageRef{Name: "aaa", Tag: "bbb"},
				"p2": imagev1.ImageRef{Name: "ccc", Tag: "zzz"},
			},
			want: true,
		},
		{
			name: "change due to different policies, same count",
			previous: imagev1.ObservedPolicies{
				"p1": imagev1.ImageRef{Name: "aaa", Tag: "bbb"},
				"p2": imagev1.ImageRef{Name: "ccc", Tag: "ddd"},
			},
			current: imagev1.ObservedPolicies{
				"p1": imagev1.ImageRef{Name: "aaa", Tag: "bbb"},
				"p3": imagev1.ImageRef{Name: "ccc", Tag: "ddd"},
			},
			want: true,
		},
		{
			name: "change due to new policy, different count",
			previous: imagev1.ObservedPolicies{
				"p1": imagev1.ImageRef{Name: "aaa", Tag: "bbb"},
			},
			current: imagev1.ObservedPolicies{
				"p1": imagev1.ImageRef{Name: "aaa", Tag: "bbb"},
				"p2": imagev1.ImageRef{Name: "ccc", Tag: "ddd"},
			},
			want: true,
		},
		{
			name: "change due to deleted policy",
			previous: imagev1.ObservedPolicies{
				"p1": imagev1.ImageRef{Name: "aaa", Tag: "bbb"},
				"p2": imagev1.ImageRef{Name: "ccc", Tag: "ddd"},
			},
			current: imagev1.ObservedPolicies{
				"p1": imagev1.ImageRef{Name: "aaa", Tag: "bbb"},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			result := observedPoliciesChanged(tt.previous, tt.current)
			g.Expect(result).To(Equal(tt.want))
		})
	}
}

func compareRepoWithExpected(ctx context.Context, g *WithT, repoURL, branch, fixture string, changeFixture func(tmp string)) {
	g.THelper()

	expected, err := os.MkdirTemp("", "gotest-imageauto-expected")
	g.Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(expected)

	copy.Copy(fixture, expected)
	changeFixture(expected)
	repo, cloneDir, err := testutil.Clone(ctx, repoURL, branch, originRemote)
	g.Expect(err).ToNot(HaveOccurred(), "failed to clone")
	defer func() { os.RemoveAll(cloneDir) }()

	// NOTE: The workdir contains a trailing /. Clean it to not confuse the
	// DiffDirectories().
	wt, err := repo.Worktree()
	g.Expect(err).ToNot(HaveOccurred())

	defer wt.Filesystem.Remove(".")

	g.Expect(err).ToNot(HaveOccurred())
	test.ExpectMatchingDirectories(g, wt.Filesystem.Root(), expected)
}

func waitForNewHead(g *WithT, repo *extgogit.Repository, branch, preChangeHash string) {
	g.THelper()

	var commitToResetTo *object.Commit

	origin, err := repo.Remote(originRemote)
	g.Expect(err).ToNot(HaveOccurred())

	// Now try to fetch new commits from that remote branch
	g.Eventually(func() bool {
		err := origin.Fetch(&extgogit.FetchOptions{
			RemoteName: originRemote,
			RefSpecs:   []config.RefSpec{config.RefSpec(testutil.BranchRefName(branch))},
		})
		if err != nil {
			return false
		}

		wt, err := repo.Worktree()
		if err != nil {
			return false
		}

		err = wt.Checkout(&extgogit.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(branch),
		})
		if err != nil {
			return false
		}

		remoteHeadRef, err := repo.Head()
		if err != nil {
			return false
		}

		remoteHeadHash := remoteHeadRef.Hash()

		if preChangeHash != remoteHeadHash.String() {
			commitToResetTo, _ = repo.CommitObject(remoteHeadHash)
			return true
		}
		return false
	}, timeout, time.Second).Should(BeTrue())

	if commitToResetTo != nil {
		wt, err := repo.Worktree()
		g.Expect(err).ToNot(HaveOccurred())

		// New commits in the remote branch -- reset the working tree head
		// to that. Note this does not create a local branch tracking the
		// remote, so it is a detached head.
		g.Expect(wt.Reset(&extgogit.ResetOptions{
			Commit: commitToResetTo.Hash,
			Mode:   extgogit.HardReset,
		})).To(Succeed())
	}
}

type repoAndPolicyArgs struct {
	namespace, imagePolicyName, gitRepoName, branch, gitRepoNamespace string
}

// newRepoAndPolicyArgs generates random git repo, branch and image
// policy names to be used in the test. The gitRepoNamespace is set the same
// as the overall given namespace. For different git repo namespace, the caller
// may assign it as per the needs.
func newRepoAndPolicyArgs(namespace string) repoAndPolicyArgs {
	args := repoAndPolicyArgs{
		namespace:        namespace,
		gitRepoName:      "image-auto-test-" + rand.String(5),
		gitRepoNamespace: namespace,
		branch:           rand.String(8),
		imagePolicyName:  "policy-" + rand.String(5),
	}
	return args
}

// testWithRepoAndImagePolicyTestFunc is the test closure function type passed
// to testWithRepoAndImagePolicy.
type testWithRepoAndImagePolicyTestFunc func(g *WithT, s repoAndPolicyArgs, repoURL string, localRepo *extgogit.Repository)

// testWithRepoAndImagePolicy generates a repoAndPolicyArgs with all the
// resource in the given namespace and runs the given repo and image policy test.
func testWithRepoAndImagePolicy(
	ctx context.Context,
	g *WithT,
	kClient client.Client,
	namespace string,
	fixture string,
	policySpec imagev1_reflect.ImagePolicySpec,
	latest string,
	testFunc testWithRepoAndImagePolicyTestFunc) {
	// Generate unique repo and policy arguments.
	args := newRepoAndPolicyArgs(namespace)
	testWithCustomRepoAndImagePolicy(ctx, g, kClient, fixture, policySpec, latest, args, testFunc)
}

// testWithCustomRepoAndImagePolicy sets up a git server, a repository in the git
// server, a GitRepository object for the created git repo, and an ImagePolicy
// with the given policy spec based on a repoAndPolicyArgs. It calls testFunc
// to run the test in the created environment.
func testWithCustomRepoAndImagePolicy(
	ctx context.Context,
	g *WithT,
	kClient client.Client,
	fixture string,
	policySpec imagev1_reflect.ImagePolicySpec,
	latest string,
	args repoAndPolicyArgs,
	testFunc testWithRepoAndImagePolicyTestFunc) {
	repositoryPath := "/config-" + rand.String(6) + ".git"

	// Create test git server.
	gitServer := testutil.SetUpGitTestServer(g)
	defer os.RemoveAll(gitServer.Root())
	defer gitServer.StopHTTP()

	// Create a git repo.
	_ = testutil.InitGitRepo(g, gitServer, fixture, args.branch, repositoryPath)

	// Clone the repo.
	repoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
	cloneCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	localRepo, cloneDir, err := testutil.Clone(cloneCtx, repoURL, args.branch, originRemote)
	g.Expect(err).ToNot(HaveOccurred(), "failed to clone")
	defer func() { os.RemoveAll(cloneDir) }()

	err = localRepo.DeleteRemote(originRemote)
	g.Expect(err).ToNot(HaveOccurred(), "failed to delete existing remote origin")
	localRepo.CreateRemote(&config.RemoteConfig{
		Name: originRemote,
		URLs: []string{repoURL},
	})
	g.Expect(err).ToNot(HaveOccurred(), "failed to create new remote origin")

	// Create GitRepository resource for the above repo.
	err = createGitRepository(ctx, kClient, args.gitRepoName, args.gitRepoNamespace, repoURL, "")
	g.Expect(err).ToNot(HaveOccurred(), "failed to create GitRepository resource")

	// Create ImagePolicy with populated latest image in the status.
	err = createImagePolicyWithLatestImageForSpec(ctx, kClient, args.imagePolicyName, args.namespace, policySpec, latest)
	g.Expect(err).ToNot(HaveOccurred(), "failed to create ImagePolicy resource")

	testFunc(g, args, repoURL, localRepo)
}

func createGitRepository(ctx context.Context, kClient client.Client, name, namespace, repoURL, secretRef string) error {
	gitRepo := &sourcev1.GitRepository{
		Spec: sourcev1.GitRepositorySpec{
			URL:      repoURL,
			Interval: metav1.Duration{Duration: time.Hour},
			Timeout:  &metav1.Duration{Duration: time.Minute},
		},
	}
	gitRepo.Name = name
	gitRepo.Namespace = namespace
	if secretRef != "" {
		gitRepo.Spec.SecretRef = &meta.LocalObjectReference{Name: secretRef}
	}
	return kClient.Create(ctx, gitRepo)
}

func createImagePolicyWithLatestImage(ctx context.Context, kClient client.Client, name, namespace, repoRef, semverRange, latest string) error {
	policySpec := imagev1_reflect.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name: repoRef,
		},
		Policy: imagev1_reflect.ImagePolicyChoice{
			SemVer: &imagev1_reflect.SemVerPolicy{
				Range: semverRange,
			},
		},
	}
	return createImagePolicyWithLatestImageForSpec(ctx, kClient, name, namespace, policySpec, latest)
}

func createImagePolicyWithLatestImageForSpec(ctx context.Context, kClient client.Client, name, namespace string, policySpec imagev1_reflect.ImagePolicySpec, latest string) error {
	policy := &imagev1_reflect.ImagePolicy{
		Spec: policySpec,
	}
	policy.Name = name
	policy.Namespace = namespace
	err := kClient.Create(ctx, policy)
	if err != nil {
		return err
	}
	patch := client.MergeFrom(policy.DeepCopy())
	policy.Status.LatestImage = latest
	return kClient.Status().Patch(ctx, policy, patch)
}

func updateImagePolicyWithLatestImage(ctx context.Context, kClient client.Client, name, namespace, latest string) error {
	policy := &imagev1_reflect.ImagePolicy{}
	key := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	if err := kClient.Get(ctx, key, policy); err != nil {
		return err
	}
	patch := client.MergeFrom(policy.DeepCopy())
	policy.Status.LatestImage = latest
	return kClient.Status().Patch(ctx, policy, patch)
}

func createImageUpdateAutomation(ctx context.Context, kClient client.Client, name, namespace,
	gitRepo, gitRepoNamespace, checkoutBranch, pushBranch, pushRefspec, commitTemplate, signingKeyRef string,
	updateStrategy *imagev1.UpdateStrategy) error {
	updateAutomation := &imagev1.ImageUpdateAutomation{
		Spec: imagev1.ImageUpdateAutomationSpec{
			Interval: metav1.Duration{Duration: 2 * time.Hour}, // This is to ensure any subsequent run should be outside the scope of the testing.
			SourceRef: imagev1.CrossNamespaceSourceReference{
				Kind:      "GitRepository",
				Name:      gitRepo,
				Namespace: gitRepoNamespace,
			},
			GitSpec: &imagev1.GitSpec{
				Checkout: &imagev1.GitCheckoutSpec{
					Reference: sourcev1.GitRepositoryRef{
						Branch: checkoutBranch,
					},
				},
				Commit: imagev1.CommitSpec{
					MessageTemplate: commitTemplate,
					Author: imagev1.CommitUser{
						Name:  testAuthorName,
						Email: testAuthorEmail,
					},
				},
			},
			Update: updateStrategy,
		},
	}
	updateAutomation.Name = name
	updateAutomation.Namespace = namespace
	if pushRefspec != "" || pushBranch != "" {
		updateAutomation.Spec.GitSpec.Push = &imagev1.PushSpec{
			Refspec: pushRefspec,
			Branch:  pushBranch,
		}
	}
	if signingKeyRef != "" {
		updateAutomation.Spec.GitSpec.Commit.SigningKey = &imagev1.SigningKey{
			SecretRef: meta.LocalObjectReference{Name: signingKeyRef},
		}
	}
	return kClient.Create(ctx, updateAutomation)
}

func deleteImageUpdateAutomation(ctx context.Context, kClient client.Client, name, namespace string) error {
	update := &imagev1.ImageUpdateAutomation{}
	update.Name = name
	update.Namespace = namespace
	return kClient.Delete(ctx, update)
}

func deleteImagePolicy(ctx context.Context, kClient client.Client, name, namespace string) error {
	imagePolicy := &imagev1_reflect.ImagePolicy{}
	imagePolicy.Name = name
	imagePolicy.Namespace = namespace
	return kClient.Delete(ctx, imagePolicy)
}

func createSigningKeyPairSecret(ctx context.Context, g *WithT, kClient client.Client, name, namespace string) *openpgp.Entity {
	secret, pgpEntity := testutil.GetSigningKeyPairSecret(g, name, namespace)
	g.Expect(kClient.Create(ctx, secret)).To(Succeed())
	return pgpEntity
}

func createSSHIdentitySecret(kClient client.Client, name, namespace, repoURL string) error {
	url, err := url.Parse(repoURL)
	if err != nil {
		return err
	}
	knownhosts, err := ssh.ScanHostKey(url.Host, 5*time.Second, []string{}, false)
	if err != nil {
		return err
	}
	keygen := ssh.NewRSAGenerator(2048)
	pair, err := keygen.Generate()
	if err != nil {
		return err
	}
	sec := &corev1.Secret{
		StringData: map[string]string{
			"known_hosts":  string(knownhosts),
			"identity":     string(pair.PrivateKey),
			"identity.pub": string(pair.PublicKey),
		},
		// Without KAS, StringData and Data must be kept in sync manually.
		Data: map[string][]byte{
			"known_hosts":  knownhosts,
			"identity":     pair.PrivateKey,
			"identity.pub": pair.PublicKey,
		},
	}
	sec.Name = name
	sec.Namespace = namespace
	return kClient.Create(ctx, sec)
}

func getRepoURL(gitServer *gittestserver.GitServer, repoPath, proto string) (string, error) {
	if proto == "http" {
		return gitServer.HTTPAddressWithCredentials() + repoPath, nil
	} else if proto == "ssh" {
		// This is expected to use 127.0.0.1, but host key
		// checking usually wants a hostname, so use
		// "localhost".
		sshURL := strings.Replace(gitServer.SSHAddress(), "127.0.0.1", "localhost", 1)
		return sshURL + repoPath, nil
	}
	return "", fmt.Errorf("proto not set to http or ssh")
}
