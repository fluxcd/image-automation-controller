package update

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
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
			assert.NoError(t, err)
			assert.Equal(t, test.expectedError, err != nil)
		})
	}
}
