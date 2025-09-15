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

package controller

import (
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/event"

	reflectorv1 "github.com/fluxcd/image-reflector-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
)

func Test_latestImageChangePredicate_Update(t *testing.T) {
	tests := []struct {
		name       string
		beforeFunc func(oldObj, newObj *reflectorv1.ImagePolicy)
		want       bool
	}{
		{
			name: "no latest image",
			beforeFunc: func(oldObj, newObj *reflectorv1.ImagePolicy) {
				oldObj.Status.LatestRef = nil
				newObj.Status.LatestRef = nil
			},
			want: false,
		},
		{
			name: "new image, no old image",
			beforeFunc: func(oldObj, newObj *reflectorv1.ImagePolicy) {
				oldObj.Status.LatestRef = nil
				newObj.Status.LatestRef = &reflectorv1.ImageRef{Name: "foo"}
			},
			want: true,
		},
		{
			name: "different old and new image",
			beforeFunc: func(oldObj, newObj *reflectorv1.ImagePolicy) {
				oldObj.Status.LatestRef = &reflectorv1.ImageRef{Name: "bar"}
				newObj.Status.LatestRef = &reflectorv1.ImageRef{Name: "foo"}
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			oldObj := &reflectorv1.ImagePolicy{}
			newObj := oldObj.DeepCopy()
			if tt.beforeFunc != nil {
				tt.beforeFunc(oldObj, newObj)
			}
			e := event.UpdateEvent{
				ObjectOld: oldObj,
				ObjectNew: newObj,
			}
			p := latestImageChangePredicate{}
			g.Expect(p.Update(e)).To(Equal(tt.want))
		})
	}
}

func Test_sourceConfigChangePredicate_Update(t *testing.T) {
	tests := []struct {
		name       string
		beforeFunc func(oldObj, newObj *sourcev1.GitRepository)
		want       bool
	}{
		{
			name: "no generation change, same config",
			beforeFunc: func(oldObj, newObj *sourcev1.GitRepository) {
				oldObj.Generation = 0
				newObj.Generation = 0
			},
			want: false,
		},
		{
			name: "new generation, config change",
			beforeFunc: func(oldObj, newObj *sourcev1.GitRepository) {
				oldObj.Generation = 1
				newObj.Generation = 2
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			oldObj := &sourcev1.GitRepository{}
			newObj := oldObj.DeepCopy()
			if tt.beforeFunc != nil {
				tt.beforeFunc(oldObj, newObj)
			}
			e := event.UpdateEvent{
				ObjectOld: oldObj,
				ObjectNew: newObj,
			}
			p := sourceConfigChangePredicate{}
			g.Expect(p.Update(e)).To(Equal(tt.want))
		})
	}

}
