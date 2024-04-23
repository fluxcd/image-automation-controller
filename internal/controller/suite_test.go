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

package controller

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	"github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/testenv"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta2"
	// +kubebuilder:scaffold:imports
)

// These tests make use of plain Go using Gomega for assertions.
// At the beginning of every (sub)test Gomega can be initialized
// using gomega.NewWithT.
// Refer to http://onsi.github.io/gomega/ to learn more about
// Gomega.

var (
	k8sClient client.Client
	testEnv   *testenv.Environment
	ctx       = ctrl.SetupSignalHandler()
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func TestMain(m *testing.M) {
	utilruntime.Must(imagev1_reflect.AddToScheme(scheme.Scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme.Scheme))
	utilruntime.Must(imagev1.AddToScheme(scheme.Scheme))

	code := runTestsWithFeatures(m, nil)
	if code != 0 {
		fmt.Println("failed with default feature values")
	}

	os.Exit(code)
}

func runTestsWithFeatures(m *testing.M, feats map[string]bool) int {
	testEnv = testenv.New(
		testenv.WithCRDPath(
			filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("testdata", "crds"),
		),
		testenv.WithMaxConcurrentReconciles(2),
	)

	var err error
	// Initialize a cacheless client for tests that need the latest objects.
	k8sClient, err = client.New(testEnv.Config, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		panic(fmt.Sprintf("failed to create k8s client: %v", err))
	}

	controllerName := "image-automation-controller"
	if err := (&ImageUpdateAutomationReconciler{
		Client:         testEnv,
		EventRecorder:  record.NewFakeRecorder(32),
		features:       feats,
		ControllerName: controllerName,
	}).SetupWithManager(ctx, testEnv, ImageUpdateAutomationReconcilerOptions{
		RateLimiter: controller.GetDefaultRateLimiter(),
	}); err != nil {
		panic(fmt.Sprintf("failed to start ImageUpdateAutomationReconciler: %v", err))
	}

	go func() {
		fmt.Println("Starting the test environment")
		if err := testEnv.Start(ctx); err != nil {
			panic(fmt.Sprintf("failed to start the test environment manager: %v", err))
		}
	}()
	<-testEnv.Manager.Elected()

	code := m.Run()

	fmt.Println("Stopping the test environment")
	if err := testEnv.Stop(); err != nil {
		panic(fmt.Sprintf("failed to stop the test environment: %v", err))
	}

	return code
}
