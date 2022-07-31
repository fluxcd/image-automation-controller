/*
Copyright 2020 The Flux authors

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

package controllers

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	git2go "github.com/libgit2/git2go/v33"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta1"
	"github.com/fluxcd/pkg/runtime/testenv"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/fluxcd/source-controller/pkg/git/libgit2/managed"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta1"
	// +kubebuilder:scaffold:imports
)

// These tests make use of plain Go using Gomega for assertions.
// At the beginning of every (sub)test Gomega can be initialized
// using gomega.NewWithT.
// Refer to http://onsi.github.io/gomega/ to learn more about
// Gomega.

var (
	testEnv *testenv.Environment
	ctx     = ctrl.SetupSignalHandler()
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func TestMain(m *testing.M) {
	mustHaveNoThreadSupport()

	utilruntime.Must(imagev1_reflect.AddToScheme(scheme.Scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme.Scheme))
	utilruntime.Must(imagev1.AddToScheme(scheme.Scheme))

	testEnv = testenv.New(testenv.WithCRDPath(
		filepath.Join("..", "config", "crd", "bases"),
		filepath.Join("testdata", "crds"),
	))

	managed.InitManagedTransport()

	controllerName := "image-automation-controller"
	if err := (&ImageUpdateAutomationReconciler{
		Client:        testEnv,
		Scheme:        scheme.Scheme,
		EventRecorder: testEnv.GetEventRecorderFor(controllerName),
	}).SetupWithManager(testEnv, ImageUpdateAutomationReconcilerOptions{}); err != nil {
		panic(fmt.Sprintf("Failed to start ImageUpdateAutomationReconciler: %v", err))
	}

	go func() {
		fmt.Println("Starting the test environment")
		if err := testEnv.Start(ctx); err != nil {
			panic(fmt.Sprintf("Failed to start the test environment manager: %v", err))
		}
	}()
	<-testEnv.Manager.Elected()

	code := m.Run()

	fmt.Println("Stopping the test environment")
	if err := testEnv.Stop(); err != nil {
		panic(fmt.Sprintf("Failed to stop the test environment: %v", err))
	}

	os.Exit(code)
}

// This provides a regression assurance for image-automation-controller/#339.
// Validates that:
// - libgit2 was built with no support for threads.
// - git2go accepts libgit2 built with no support for threads.
//
// The logic below does the validation of the former, whilst
// referring to git2go forces its init() execution, which is
// where any validation to that effect resides.
//
// git2go does not support threadless libgit2 by default,
// hence a fork is being used which disables such validation.
//
// TODO: extract logic into pkg.
func mustHaveNoThreadSupport() {
	if git2go.Features()&git2go.FeatureThreads != 0 {
		panic("libgit2 must not be build with thread support")
	}
}
