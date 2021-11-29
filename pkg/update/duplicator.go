/*
Copyright 2020, 2021 The Flux authors

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
	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	"github.com/go-logr/logr"
)

// UpdateWithDuplicator takes all YAML files from `inpath`, updates/create any
// that contain an "in scope" image policy marker, and writes files it
// updated (and only those files) back to `outpath`.
func UpdateWithDuplicator(tracelog logr.Logger, inpath, outpath string, policies []imagev1_reflect.ImagePolicy) (Result, error) {
}

