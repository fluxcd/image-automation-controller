/*
Copyright 2025 The Flux authors

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

package update

import (
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	"k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func TestSetAllCallbackAccept(t *testing.T) {
	tests := []struct {
		name          string
		object        *yaml.RNode
		settersSchema *spec.Schema
		expectedError bool
	}{
		{
			name: "Accept - Scalar Node",
			object: yaml.NewRNode(&yaml.Node{
				Kind:  yaml.ScalarNode,
				Value: "test",
			}),
			settersSchema: &spec.Schema{},
			expectedError: false,
		},
		{
			name: "Accept - Scalar Node - Error",
			object: yaml.NewRNode(&yaml.Node{
				Kind:  yaml.ScalarNode,
				Value: "test",
			}),
			settersSchema: nil,
			expectedError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			callbackInstance := SetAllCallback{
				SettersSchema: test.settersSchema,
				Trace:         logr.Discard(),
			}

			err := accept(&callbackInstance, test.object, "", test.settersSchema)
			g := NewWithT(t)
			if test.expectedError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
		})
	}
}

func TestGetExtFromSchema(t *testing.T) {
	tests := []struct {
		name              string
		schema            *spec.Schema
		expectedExtension *extension
		expectedError     bool
	}{
		{
			name: "Extension Present",
			schema: &spec.Schema{
				VendorExtensible: spec.VendorExtensible{
					Extensions: map[string]interface{}{
						K8sCliExtensionKey: &extension{
							Setter: &setter{
								Name:  "testSetter",
								Value: "testValue",
							},
						},
					},
				},
			},
			expectedExtension: &extension{
				Setter: &setter{
					Name:  "testSetter",
					Value: "testValue",
				},
			},
			expectedError: false,
		},
		{
			name:          "Extension Not Present",
			schema:        &spec.Schema{},
			expectedError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			ext, err := getExtFromSchema(test.schema)

			if test.expectedError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(ext).To(Equal(test.expectedExtension))
			}
		})
	}
}
