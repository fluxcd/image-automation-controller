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

// Package v1beta2 contains API types for the image API group, version
// v1beta2. The types here are concerned with automated updates to
// git, based on metadata from OCI image registries gathered by the
// image-reflector-controller.
//
// +kubebuilder:object:generate=true
// +groupName=image.toolkit.fluxcd.io
package v1beta2
