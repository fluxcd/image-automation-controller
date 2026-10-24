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
	"testing"
	"testing/synctest"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/priorityqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	reflectorv1 "github.com/fluxcd/image-reflector-controller/api/v1"
	helper "github.com/fluxcd/pkg/runtime/controller"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1"
)

type policyEnqueue struct {
	request reconcile.Request
	delay   time.Duration
	delayed bool
}

type recordingPolicyQueue struct {
	workqueue.TypedRateLimitingInterface[reconcile.Request]
	calls []policyEnqueue
}

func (q *recordingPolicyQueue) Add(req reconcile.Request) {
	q.calls = append(q.calls, policyEnqueue{request: req})
}

func (q *recordingPolicyQueue) AddAfter(req reconcile.Request, delay time.Duration) {
	q.calls = append(q.calls, policyEnqueue{request: req, delay: delay, delayed: true})
}

func TestImagePolicyHandlerMapping(t *testing.T) {
	matching := map[string]string{"app": "selected"}
	unmatched := map[string]string{"app": "other"}
	selector := &metav1.LabelSelector{MatchLabels: matching}
	tests := []struct {
		name          string
		selector      *metav1.LabelSelector
		oldLabels     map[string]string
		newLabels     map[string]string
		observed      bool
		labelsOnly    bool
		suspended     bool
		eventType     string
		wantWithDelay bool
	}{
		{name: "matching labels", selector: selector, oldLabels: matching, newLabels: matching, wantWithDelay: true},
		{name: "nil selector", wantWithDelay: true},
		{name: "empty selector", selector: &metav1.LabelSelector{}, wantWithDelay: true},
		{
			name: "match expressions",
			selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"selected"}},
			}},
			oldLabels: matching, newLabels: matching, wantWithDelay: true,
		},
		{name: "unselected policy", selector: selector, oldLabels: unmatched, newLabels: unmatched},
		{name: "selector exit", selector: selector, oldLabels: matching, newLabels: unmatched, wantWithDelay: true},
		{name: "selector entry", selector: selector, oldLabels: unmatched, newLabels: matching, wantWithDelay: true},
		{name: "unselected but observed", selector: selector, oldLabels: unmatched, newLabels: unmatched, observed: true, wantWithDelay: true},
		{name: "invalid selector", selector: &metav1.LabelSelector{MatchLabels: map[string]string{"invalid/key/key": "value"}}, wantWithDelay: true},
		{name: "suspended automation", suspended: true, wantWithDelay: true},
		{name: "labels only exit", selector: selector, oldLabels: matching, newLabels: unmatched, labelsOnly: true},
		{name: "labels only entry", selector: selector, oldLabels: unmatched, newLabels: matching, labelsOnly: true},
		{name: "create", eventType: "create"},
		{name: "delete", eventType: "delete"},
	}
	delays := []struct {
		name         string
		value        *metav1.Duration
		anotherValue *metav1.Duration
	}{
		{name: "unset"},
		{name: "zero", value: &metav1.Duration{}, anotherValue: &metav1.Duration{}},
		{name: "positive", value: &metav1.Duration{Duration: 3 * time.Second}, anotherValue: &metav1.Duration{Duration: 3 * time.Second}},
		{name: "mixed per-automation delays", anotherValue: &metav1.Duration{Duration: 5 * time.Second}},
	}
	for _, tt := range tests {
		for _, delay := range delays {
			t.Run(tt.name+"/"+delay.name, func(t *testing.T) {
				g := NewWithT(t)
				scheme := runtime.NewScheme()
				g.Expect(imagev1.AddToScheme(scheme)).To(Succeed())
				auto := &imagev1.ImageUpdateAutomation{
					ObjectMeta: metav1.ObjectMeta{Name: "automation", Namespace: "default"},
					Spec: imagev1.ImageUpdateAutomationSpec{
						PolicySelector: tt.selector, PolicyChangeDelay: delay.value, Suspend: tt.suspended,
					},
				}
				if tt.observed {
					auto.Status.ObservedPolicies = imagev1.ObservedPolicies{"policy": {}}
				}
				another := &imagev1.ImageUpdateAutomation{
					ObjectMeta: metav1.ObjectMeta{Name: "another", Namespace: "default"},
					Spec:       imagev1.ImageUpdateAutomationSpec{PolicyChangeDelay: delay.anotherValue},
				}
				foreign := another.DeepCopy()
				foreign.Namespace = "foreign"
				r := &ImageUpdateAutomationReconciler{Client: fakeclient.NewClientBuilder().
					WithScheme(scheme).WithObjects(auto, another, foreign).Build()}
				h := r.imagePolicyHandler()
				p := latestImageChangePredicate{}
				q := &recordingPolicyQueue{}
				ctx := context.Background()
				oldPolicy := &reflectorv1.ImagePolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: "default", Labels: tt.oldLabels},
					Status:     reflectorv1.ImagePolicyStatus{LatestRef: &reflectorv1.ImageRef{Name: "app", Tag: "v1"}},
				}
				newPolicy := oldPolicy.DeepCopy()
				newPolicy.Labels = tt.newLabels
				if !tt.labelsOnly {
					newPolicy.Status.LatestRef.Tag = "v2"
				}
				switch tt.eventType {
				case "create":
					e := event.CreateEvent{Object: newPolicy}
					g.Expect(p.Create(e)).To(BeFalse())
				case "delete":
					e := event.DeleteEvent{Object: oldPolicy}
					g.Expect(p.Delete(e)).To(BeFalse())
				default:
					e := event.UpdateEvent{ObjectOld: oldPolicy, ObjectNew: newPolicy}
					g.Expect(p.Update(e)).To(Equal(!tt.labelsOnly))
					if p.Update(e) {
						h.Update(ctx, e, q)
					}
				}

				var want []policyEnqueue
				if tt.eventType == "" && !tt.labelsOnly {
					delayed := auto.GetPolicyChangeDelay() > 0
					want = append(want, policyEnqueue{
						request: reconcile.Request{NamespacedName: client.ObjectKeyFromObject(another)},
						delay:   another.GetPolicyChangeDelay(), delayed: another.GetPolicyChangeDelay() > 0,
					})
					if !delayed || tt.wantWithDelay {
						want = append(want, policyEnqueue{
							request: reconcile.Request{NamespacedName: client.ObjectKeyFromObject(auto)},
							delay:   auto.GetPolicyChangeDelay(), delayed: delayed,
						})
					}
				}
				g.Expect(q.calls).To(ConsistOf(want))
			})
		}
	}
}

// policyQueueTest drives the production queue using synctest's clock.
type policyQueueTest struct {
	g       *WithT
	q       priorityqueue.PriorityQueue[reconcile.Request]
	req     reconcile.Request
	enqueue func()
}

func (s *policyQueueTest) ready(count int) {
	s.g.THelper()
	synctest.Wait()
	s.g.Expect(s.q.Len()).To(Equal(count))
}

func (s *policyQueueTest) advance(duration time.Duration, count int) {
	s.g.THelper()
	time.Sleep(duration)
	s.ready(count)
}

func (s *policyQueueTest) get() {
	s.g.THelper()
	s.ready(1)
	req, shutdown := s.q.Get()
	s.g.Expect(shutdown).To(BeFalse())
	s.g.Expect(req).To(Equal(s.req))
}

func (s *policyQueueTest) finish() {
	s.q.Forget(s.req)
	s.q.Done(s.req)
}

func TestImagePolicyHandlerQueue(t *testing.T) {
	const delay = 3 * time.Second
	tests := []struct {
		name  string
		delay time.Duration
		run   func(*policyQueueTest)
	}{
		{
			name: "no delay is immediately ready",
			run: func(s *policyQueueTest) {
				s.enqueue()
				s.get()
				s.finish()
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(2*delay, 0)
			},
		},
		{
			name: "fixed deadline not sliding", delay: delay,
			run: func(s *policyQueueTest) {
				s.enqueue()
				s.ready(0)
				s.advance(time.Second, 0)
				s.enqueue()
				s.ready(0)
				s.advance(2*time.Second-time.Nanosecond, 0)
				s.advance(time.Nanosecond, 1)
				s.get()
				s.finish()
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(2*delay, 0)
			},
		},
		{
			name: "ready key stays ready without residual wakeup", delay: delay,
			run: func(s *policyQueueTest) {
				s.q.Add(s.req)
				s.ready(1)
				s.enqueue()
				s.get()
				s.finish()
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(2*delay, 0)
			},
		},
		{
			name: "second burst has a new deadline", delay: delay,
			run: func(s *policyQueueTest) {
				s.enqueue()
				s.ready(0)
				s.advance(delay, 1)
				s.get()
				s.finish()
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(2*delay, 0)
				s.enqueue()
				s.ready(0)
				s.advance(delay-time.Nanosecond, 0)
				s.advance(time.Nanosecond, 1)
				s.get()
				s.finish()
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(2*delay, 0)
			},
		},
		{
			name: "processing finishes before deadline", delay: delay,
			run: func(s *policyQueueTest) {
				s.q.Add(s.req)
				s.get()
				s.enqueue()
				s.ready(0)
				s.advance(time.Second, 0)
				s.finish()
				s.advance(2*time.Second-time.Nanosecond, 0)
				s.advance(time.Nanosecond, 1)
				s.get()
				s.finish()
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(2*delay, 0)
			},
		},
		{
			name: "deadline passes while processing", delay: delay,
			run: func(s *policyQueueTest) {
				s.q.Add(s.req)
				s.get()
				s.enqueue()
				s.ready(0)
				type result struct {
					req      reconcile.Request
					shutdown bool
				}
				got := make(chan result, 1)
				go func() {
					req, shutdown := s.q.Get()
					got <- result{req, shutdown}
				}()
				synctest.Wait()
				time.Sleep(2 * delay)
				synctest.Wait()
				s.g.Expect(got).NotTo(Receive())
				s.finish()
				synctest.Wait()
				s.g.Expect(got).To(Receive(Equal(result{req: s.req})))
				s.finish()
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(2*delay, 0)
			},
		},
		{
			name: "earlier interval takes over", delay: delay,
			run: func(s *policyQueueTest) {
				s.q.Add(s.req)
				s.get()
				s.enqueue()
				s.ready(0)
				s.q.Forget(s.req)
				s.q.AddAfter(s.req, time.Second)
				s.q.Done(s.req)
				s.ready(0)
				s.advance(time.Second-time.Nanosecond, 0)
				s.advance(time.Nanosecond, 1)
				s.get()
				s.finish()
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(2*delay, 0)
			},
		},
		{
			name: "policy shortens interval", delay: delay,
			run: func(s *policyQueueTest) {
				s.q.Add(s.req)
				s.get()
				s.q.Forget(s.req)
				s.q.AddAfter(s.req, time.Hour)
				s.q.Done(s.req)
				s.ready(0)
				s.enqueue()
				s.ready(0)
				s.advance(delay-time.Nanosecond, 0)
				s.advance(time.Nanosecond, 1)
				s.get()
				s.finish()
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(time.Hour, 0)
			},
		},
		{
			name: "immediate trigger takes over", delay: delay,
			run: func(s *policyQueueTest) {
				s.enqueue()
				s.ready(0)
				s.advance(time.Second, 0)
				s.q.Add(s.req)
				s.get()
				s.finish()
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(2*delay, 0)
			},
		},
		{
			name: "earlier retry takes over", delay: delay,
			run: func(s *policyQueueTest) {
				s.q.Add(s.req)
				s.get()
				s.enqueue()
				s.ready(0)
				s.q.AddRateLimited(s.req)
				s.q.Done(s.req)
				s.ready(0)
				s.g.Expect(s.q.NumRequeues(s.req)).To(Equal(1))
				s.advance(750*time.Millisecond-time.Nanosecond, 0)
				s.advance(time.Nanosecond, 1)
				s.get()
				s.finish()
				s.g.Expect(s.q.NumRequeues(s.req)).To(BeZero())
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(2*delay, 0)
			},
		},
		{
			name: "policy shortens 12s retry backoff", delay: delay,
			run: func(s *policyQueueTest) {
				s.q.Add(s.req)
				for _, backoff := range []time.Duration{750 * time.Millisecond, 1500 * time.Millisecond, 3 * time.Second, 6 * time.Second} {
					s.get()
					s.q.AddRateLimited(s.req)
					s.q.Done(s.req)
					s.ready(0)
					s.advance(backoff, 1)
				}
				s.get()
				s.q.AddRateLimited(s.req)
				s.q.Done(s.req)
				s.ready(0)
				s.g.Expect(s.q.NumRequeues(s.req)).To(Equal(5))
				s.enqueue()
				s.ready(0)
				s.g.Expect(s.q.NumRequeues(s.req)).To(Equal(5))
				s.advance(delay-time.Nanosecond, 0)
				s.advance(time.Nanosecond, 1)
				s.get()
				s.finish()
				// Guard against residual wakeups if the queue implementation changes.
				s.advance(12*time.Second, 0)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				g := NewWithT(t)
				scheme := runtime.NewScheme()
				g.Expect(imagev1.AddToScheme(scheme)).To(Succeed())
				auto := &imagev1.ImageUpdateAutomation{
					ObjectMeta: metav1.ObjectMeta{Name: "automation", Namespace: "default"},
				}
				if tt.delay > 0 {
					auto.Spec.PolicyChangeDelay = &metav1.Duration{Duration: tt.delay}
				}
				r := &ImageUpdateAutomationReconciler{Client: fakeclient.NewClientBuilder().
					WithScheme(scheme).WithObjects(auto).Build()}
				q := priorityqueue.New("", func(o *priorityqueue.Opts[reconcile.Request]) {
					o.RateLimiter = helper.GetDefaultRateLimiter()
				})
				defer q.ShutDown()
				h := r.imagePolicyHandler()
				p := latestImageChangePredicate{}
				s := &policyQueueTest{
					g: g, q: q, req: reconcile.Request{NamespacedName: client.ObjectKeyFromObject(auto)},
					enqueue: func() {
						oldPolicy := &reflectorv1.ImagePolicy{
							ObjectMeta: metav1.ObjectMeta{Name: "policy", Namespace: auto.Namespace},
							Status:     reflectorv1.ImagePolicyStatus{LatestRef: &reflectorv1.ImageRef{Name: "app", Tag: "v1"}},
						}
						newPolicy := oldPolicy.DeepCopy()
						newPolicy.Status.LatestRef.Tag = "v2"
						e := event.UpdateEvent{ObjectOld: oldPolicy, ObjectNew: newPolicy}
						g.Expect(p.Update(e)).To(BeTrue())
						h.Update(context.Background(), e, q)
					},
				}
				tt.run(s)
			})
		})
	}
}
