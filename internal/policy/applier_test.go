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

package policy

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/otiai10/copy"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta2"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta2"
	"github.com/fluxcd/image-automation-controller/internal/testutil"
	"github.com/fluxcd/image-automation-controller/pkg/test"
	"github.com/fluxcd/image-automation-controller/pkg/update"
)

func testdataPath(path string) string {
	return filepath.Join("testdata", path)
}

func Test_applyPolicies(t *testing.T) {
	tests := []struct {
		name               string
		updateStrategy     *imagev1.UpdateStrategy
		policyLatestImages map[string]string
		targetPolicyName   string
		replaceMarkerFunc  func(g *WithT, path string, policyKey types.NamespacedName)
		inputPath          string
		expectedPath       string
		wantErr            bool
		wantResult         update.Result
	}{
		{
			name: "valid update strategy and one policy",
			updateStrategy: &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			},
			policyLatestImages: map[string]string{
				"policy1": "helloworld:1.0.1",
			},
			targetPolicyName: "policy1",
			inputPath:        testdataPath("appconfig"),
			expectedPath:     testdataPath("appconfig-setters-expected"),
			wantErr:          false,
		},
		{
			name:           "no update strategy",
			updateStrategy: nil,
			wantErr:        true,
		},
		{
			name: "unknown update strategy",
			updateStrategy: &imagev1.UpdateStrategy{
				Strategy: "foo",
			},
			wantErr: true,
		},
		{
			name: "valid update strategy and multiple policies",
			updateStrategy: &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
			},
			policyLatestImages: map[string]string{
				"policy1": "foo:1.1.1",
				"policy2": "helloworld:1.0.1",
				"policy3": "bar:2.2.2",
			},
			targetPolicyName: "policy2",
			inputPath:        testdataPath("appconfig"),
			expectedPath:     testdataPath("appconfig-setters-expected"),
			wantErr:          false,
		},
		{
			name: "valid update strategy with update path",
			updateStrategy: &imagev1.UpdateStrategy{
				Strategy: imagev1.UpdateStrategySetters,
				Path:     "./yes",
			},
			policyLatestImages: map[string]string{
				"policy1": "helloworld:1.0.1",
			},
			targetPolicyName: "policy1",
			replaceMarkerFunc: func(g *WithT, path string, policyKey types.NamespacedName) {
				g.Expect(testutil.ReplaceMarker(filepath.Join(path, "yes", "deploy.yaml"), policyKey)).ToNot(HaveOccurred())
				g.Expect(testutil.ReplaceMarker(filepath.Join(path, "no", "deploy.yaml"), policyKey)).ToNot(HaveOccurred())
			},
			inputPath:    testdataPath("pathconfig"),
			expectedPath: testdataPath("pathconfig-expected"),
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			testNS := "test-ns"
			workDir := t.TempDir()

			// Create all the policy objects.
			policyList := []imagev1_reflect.ImagePolicy{}
			for name, image := range tt.policyLatestImages {
				policy := &imagev1_reflect.ImagePolicy{}
				policy.Name = name
				policy.Namespace = testNS
				policy.Status = imagev1_reflect.ImagePolicyStatus{
					LatestImage: image,
				}
				policyList = append(policyList, *policy)
			}
			targetPolicyKey := types.NamespacedName{
				Name: tt.targetPolicyName, Namespace: testNS,
			}

			if tt.inputPath != "" {
				g.Expect(copy.Copy(tt.inputPath, workDir)).ToNot(HaveOccurred())
				// Update the test files with the target policy.
				if tt.replaceMarkerFunc != nil {
					tt.replaceMarkerFunc(g, workDir, targetPolicyKey)
				} else {
					g.Expect(testutil.ReplaceMarker(filepath.Join(workDir, "deploy.yaml"), targetPolicyKey)).ToNot(HaveOccurred())
				}
			}

			updateAuto := &imagev1.ImageUpdateAutomation{}
			updateAuto.Name = "test-update"
			updateAuto.Namespace = testNS
			updateAuto.Spec = imagev1.ImageUpdateAutomationSpec{
				Update: tt.updateStrategy,
			}

			scheme := runtime.NewScheme()
			imagev1_reflect.AddToScheme(scheme)
			imagev1.AddToScheme(scheme)

			_, err := ApplyPolicies(context.TODO(), workDir, updateAuto, policyList)
			g.Expect(err != nil).To(Equal(tt.wantErr))

			// Check the results if there wasn't any error.
			if !tt.wantErr {
				expected := t.TempDir()
				copy.Copy(tt.expectedPath, expected)
				// Update the markers in the expected test data.
				if tt.replaceMarkerFunc != nil {
					tt.replaceMarkerFunc(g, expected, targetPolicyKey)
				} else {
					g.Expect(testutil.ReplaceMarker(filepath.Join(expected, "deploy.yaml"), targetPolicyKey)).ToNot(HaveOccurred())
				}
				test.ExpectMatchingDirectories(g, workDir, expected)
			}
		})
	}
}
