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
	"errors"
	"fmt"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/fluxcd/pkg/runtime/logger"
	"sigs.k8s.io/controller-runtime/pkg/log"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta2"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta2"
	"github.com/fluxcd/image-automation-controller/pkg/update"
)

var (
	// ErrNoUpdateStrategy is an update error when the update strategy is not
	// specified.
	ErrNoUpdateStrategy = errors.New("no update strategy")
	// ErrUnsupportedUpdateStrategy is an update error when the provided update
	// strategy is not supported.
	ErrUnsupportedUpdateStrategy = errors.New("unsupported update strategy")
)

// ApplyPolicies applies the given set of policies on the source present in the
// workDir based on the provided ImageUpdateAutomation configuration.
func ApplyPolicies(ctx context.Context, workDir string, obj *imagev1.ImageUpdateAutomation, policies []imagev1_reflect.ImagePolicy) (update.ResultV2, error) {
	var result update.ResultV2
	if obj.Spec.Update == nil {
		return result, ErrNoUpdateStrategy
	}
	if obj.Spec.Update.Strategy != imagev1.UpdateStrategySetters {
		return result, fmt.Errorf("%w: %s", ErrUnsupportedUpdateStrategy, obj.Spec.Update.Strategy)
	}

	// Resolve the path to the manifests to apply policies on.
	manifestPath := workDir
	if obj.Spec.Update.Path != "" {
		p, err := securejoin.SecureJoin(workDir, obj.Spec.Update.Path)
		if err != nil {
			return result, fmt.Errorf("failed to secure join manifest path: %w", err)
		}
		manifestPath = p
	}

	tracelog := log.FromContext(ctx).V(logger.TraceLevel)
	return update.UpdateV2WithSetters(tracelog, manifestPath, manifestPath, policies)
}
