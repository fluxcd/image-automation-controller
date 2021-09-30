//build +gofuzz
/*
Copyright 2021 The Flux authors
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
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fluxcd/image-automation-controller/pkg/update"
	"github.com/fluxcd/pkg/gittestserver"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta1"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	cfgFuzz                 *rest.Config
	k8sClientFuzz           client.Client
	k8sManagerFuzz          ctrl.Manager
	imageAutoReconcilerFuzz *ImageUpdateAutomationReconciler
	testEnvFuzz             *envtest.Environment
	initter             sync.Once
)

// createKUBEBUILDER_ASSETS runs "setup-envtest use"
// and returns the path of the 3 binaries
func createKUBEBUILDER_ASSETS() string {
	out, err := exec.Command("setup-envtest", "use").Output()
	if err != nil {
		panic(err)
	}

	// split the output to get the path:
	splitString := strings.Split(string(out), " ")
	binPath := strings.TrimSuffix(splitString[len(splitString)-1], "\n")
	if err != nil {
		panic(err)
	}
	return binPath
}

// An init function that is invoked by way of sync.Do
func initFunction() {
	kubebuilder_assets := createKUBEBUILDER_ASSETS()
	os.Setenv("KUBEBUILDER_ASSETS", kubebuilder_assets)
	ctrl.SetLogger(
		zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)),
	)

	testEnvFuzz = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "config", "crd", "bases"),
			filepath.Join("testdata", "crds"),
		},
	}

	var err error
	cfgFuzz, err = testEnvFuzz.Start()
	if err != nil {
		panic(err)
	}
	if cfgFuzz == nil {
		panic("cfgFuzz is nil")
	}

	err = sourcev1.AddToScheme(scheme.Scheme)
	if err != nil {
		panic(err)
	}
	err = imagev1_reflect.AddToScheme(scheme.Scheme)
	if err != nil {
		panic(err)
	}
	err = imagev1.AddToScheme(scheme.Scheme)
	if err != nil {
		panic(err)
	}
	k8sManagerFuzz, err = ctrl.NewManager(cfgFuzz, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		panic(err)
	}

	imageAutoReconcilerFuzz = &ImageUpdateAutomationReconciler{
		Client: k8sManagerFuzz.GetClient(),
		Scheme: scheme.Scheme,
	}
	err = imageAutoReconcilerFuzz.SetupWithManager(k8sManagerFuzz, ImageUpdateAutomationReconcilerOptions{})
	if err != nil {
		panic(err)
	}
	time.Sleep(1 * time.Second)
	go func() {
		err = k8sManagerFuzz.Start(ctrl.SetupSignalHandler())
		if err != nil {
			panic(err)
		}
	}()

	k8sClientFuzz, err = client.New(cfgFuzz, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		panic(err)
	}
	if k8sClientFuzz == nil {
		panic("k8sClientFuzz is nil")
	}
}

// This fuzzer randomized 2 things:
// 1: The files in the git repository
// 2: The values of ImageUpdateAutomationSpec
//    and ImagePolicy resources
func FuzzReconciler(data []byte) int {
	initter.Do(initFunction)

	f := fuzz.NewConsumer(data)

	// We start by creating a lot of the values that
	// need for the various resources later on
	runes := "abcdefghijklmnopqrstuvwxyz1234567890"
	branch, err := f.GetStringFrom(runes, 80)
	if err != nil {
		return 0
	}
	repPath, err := f.GetStringFrom(runes, 80)
	if err != nil {
		return 0
	}
	repositoryPath := "/config-" + repPath + ".git"

	namespaceName, err := f.GetStringFrom(runes, 59)
	if err != nil {
		return 0
	}

	gitRepoKeyName, err := f.GetStringFrom(runes, 80)
	if err != nil {
		return 0
	}

	username, err := f.GetStringFrom(runes, 80)
	if err != nil {
		return 0
	}
	password, err := f.GetStringFrom(runes, 80)
	if err != nil {
		return 0
	}

	ipSpec := imagev1_reflect.ImagePolicySpec{}
	err = f.GenerateStruct(&ipSpec)
	if err != nil {
		return 0
	}

	ipStatus := imagev1_reflect.ImagePolicyStatus{}
	err = f.GenerateStruct(&ipStatus)
	if err != nil {
		return 0
	}

	iuaSpec := imagev1.ImageUpdateAutomationSpec{}
	err = f.GenerateStruct(&iuaSpec)
	if err != nil {
		return 0
	}
	gitSpec := &imagev1.GitSpec{}
	err = f.GenerateStruct(&gitSpec)
	if err != nil {
		return 0
	}

	policyKeyName, err := f.GetStringFrom(runes, 80)
	if err != nil {
		return 0
	}

	updateKeyName, err := f.GetStringFrom("abcdefghijklmnopqrstuvwxy.-", 120)
	if err != nil {
		return 0
	}

	// Create random git files
	gitPath, err := os.MkdirTemp("", "git-dir-")
	if err != nil {
		return 0
	}
	defer os.RemoveAll(gitPath)
	err = f.CreateFiles(gitPath)
	if err != nil {
		return 0
	}

	// Done with creating the random values

	// Create a namespace
	namespace := &corev1.Namespace{}
	namespace.Name = namespaceName
	err = k8sClientFuzz.Create(context.Background(), namespace)
	if err != nil {
		return 0
	}
	defer func() {
		err = k8sClientFuzz.Delete(context.Background(), namespace)
		if err != nil {
			panic(err)
		}
		time.Sleep(80 * time.Millisecond)
	}()

	// Set up git-related stuff
	gitServer, err := gittestserver.NewTempGitServer()
	if err != nil {
		return 0
	}
	gitServer.Auth(username, password)
	gitServer.AutoCreate()
	err = gitServer.StartHTTP()
	if err != nil {
		return 0
	}
	defer func() {
		gitServer.StopHTTP()
		os.RemoveAll(gitServer.Root())
	}()
	gitServer.KeyDir(filepath.Join(gitServer.Root(), "keys"))
	err = gitServer.ListenSSH()
	if err != nil {
		return 0
	}
	err = initGitRepo(gitServer, gitPath, branch, repositoryPath)
	if err != nil {
		return 0
	}
	repoURL := gitServer.HTTPAddressWithCredentials() + repositoryPath
	// Done with setting up git related stuff

	// Create git repository object
	gitRepoKey := types.NamespacedName{
		Name:      "image-auto-" + gitRepoKeyName,
		Namespace: namespace.Name,
	}

	gitRepo := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gitRepoKey.Name,
			Namespace: namespace.Name,
		},
		Spec: sourcev1.GitRepositorySpec{
			URL:      repoURL,
			Interval: metav1.Duration{Duration: time.Minute},
		},
	}
	err = k8sClientFuzz.Create(context.Background(), gitRepo)
	if err != nil {
		return 0
	}
	defer k8sClientFuzz.Delete(context.Background(), gitRepo)

	// Create image policy object
	policyKey := types.NamespacedName{
		Name:      "policy-" + policyKeyName,
		Namespace: namespace.Name,
	}
	policy := &imagev1_reflect.ImagePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyKey.Name,
			Namespace: policyKey.Namespace,
		},
		Spec:   ipSpec,
		Status: ipStatus,
	}
	err = k8sClientFuzz.Create(context.Background(), policy)
	if err != nil {
		return 0
	}
	err = k8sClientFuzz.Status().Update(context.Background(), policy)
	if err != nil {
		return 0
	}

	// Create ImageUpdateAutomation object
	updateKey := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      updateKeyName,
	}

	// Setting these fields manually to help the fuzzer
	gitSpec.Checkout.Reference.Branch = branch
	iuaSpec.GitSpec = gitSpec
	iuaSpec.SourceRef.Kind = "GitRepository"
	iuaSpec.SourceRef.Name = gitRepoKey.Name
	iuaSpec.Update.Strategy = imagev1.UpdateStrategySetters

	iua := &imagev1.ImageUpdateAutomation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      updateKey.Name,
			Namespace: updateKey.Namespace,
		},
		Spec: iuaSpec,
	}
	err = k8sClientFuzz.Create(context.Background(), iua)
	if err != nil {
		return 0
	}
	defer k8sClientFuzz.Delete(context.Background(), iua)
	time.Sleep(time.Millisecond * 70)
	return 1
}

// A fuzzer that is more focused on UpdateWithSetters
// that the reconciler fuzzer is
func FuzzUpdateWithSetters(data []byte) int {
	f := fuzz.NewConsumer(data)

	// Create dir1
	tmp1, err := ioutil.TempDir("", "fuzztest1")
	if err != nil {
		return 0
	}
	defer os.RemoveAll(tmp1)
	// Add files to dir1
	err = f.CreateFiles(tmp1)
	if err != nil {
		return 0
	}

	// Create dir2
	tmp2, err := ioutil.TempDir("", "fuzztest2")
	if err != nil {
		return 0
	}
	defer os.RemoveAll(tmp2)

	// Create policies
	policies := make([]imagev1_reflect.ImagePolicy, 0)
	noOfPolicies, err := f.GetInt()
	if err != nil {
		return 0
	}
	for i := 0; i < noOfPolicies%10; i++ {
		policy := imagev1_reflect.ImagePolicy{}
		err = f.GenerateStruct(&policy)
		if err != nil {
			return 0
		}
		policies = append(policies, policy)
	}

	// Call the target
	_, _ = update.UpdateWithSetters(logr.Discard(), tmp1, tmp2, policies)
	return 1
}
