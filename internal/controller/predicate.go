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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta2"
)

// latestImageChangePredicate implements a predicate for latest image change.
// This can be used to filter events from ImagePolicies for change in the latest
// image.
type latestImageChangePredicate struct {
	predicate.Funcs
}

func (latestImageChangePredicate) Create(e event.CreateEvent) bool {
	return false
}

func (latestImageChangePredicate) Delete(e event.DeleteEvent) bool {
	return false
}

func (latestImageChangePredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldSource, ok := e.ObjectOld.(*imagev1_reflect.ImagePolicy)
	if !ok {
		return false
	}

	newSource, ok := e.ObjectNew.(*imagev1_reflect.ImagePolicy)
	if !ok {
		return false
	}

	if newSource.Status.LatestRef == nil {
		return false
	}

	if oldSource.Status.LatestRef == nil || *oldSource.Status.LatestRef != *newSource.Status.LatestRef {
		return true
	}

	return false
}

// sourceConfigChangePredicate implements a predicate for source configuration
// change. This can be used to filter events from source objects for change in
// source configuration.
type sourceConfigChangePredicate struct {
	predicate.Funcs
}

func (sourceConfigChangePredicate) Create(e event.CreateEvent) bool {
	return false
}

func (sourceConfigChangePredicate) Delete(e event.DeleteEvent) bool {
	return false
}

func (sourceConfigChangePredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
}
