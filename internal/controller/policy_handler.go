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

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/fluxcd/pkg/runtime/logger"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1"
)

// imagePolicyHandler enqueues the automations in the policy's namespace,
// immediately or after their PolicyChangeDelay. With a delay, an automation is
// enqueued only if its selector matches the old or new policy labels, the
// policy is in its observed policies, or its selector is invalid.
func (r *ImageUpdateAutomationReconciler) imagePolicyHandler() handler.TypedFuncs[client.Object, reconcile.Request] {
	return handler.TypedFuncs[client.Object, reconcile.Request]{
		UpdateFunc: func(ctx context.Context, e event.TypedUpdateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			log := ctrl.LoggerFrom(ctx)
			var automations imagev1.ImageUpdateAutomationList
			if err := r.List(ctx, &automations, client.InNamespace(e.ObjectNew.GetNamespace())); err != nil {
				log.Error(err, "failed to list ImageUpdateAutomations for ImagePolicy change")
				return
			}

			for i := range automations.Items {
				auto := &automations.Items[i]
				req := reconcile.Request{NamespacedName: client.ObjectKeyFromObject(auto)}
				delay := auto.GetPolicyChangeDelay()
				if delay > 0 {
					selector, err := policySelector(auto.Spec.PolicySelector)
					_, observed := auto.Status.ObservedPolicies[e.ObjectNew.GetName()]
					// Invalid selectors must reach Reconcile to report Stalled.
					if err == nil && !observed &&
						!selector.Matches(labels.Set(e.ObjectNew.GetLabels())) &&
						!selector.Matches(labels.Set(e.ObjectOld.GetLabels())) {
						log.V(logger.TraceLevel).Info("skipping ImagePolicy change: policy is not selected or observed",
							"automation", req.NamespacedName, "policy", client.ObjectKeyFromObject(e.ObjectNew))
						continue
					}
					q.AddAfter(req, delay)
				} else {
					q.Add(req)
				}
				log.V(logger.TraceLevel).Info("requesting reconciliation for ImagePolicy change",
					"automation", req.NamespacedName, "policy", client.ObjectKeyFromObject(e.ObjectNew), "delay", delay)
			}
		},
	}
}
