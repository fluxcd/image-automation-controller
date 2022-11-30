//go:build gofuzz_libfuzzer
// +build gofuzz_libfuzzer

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
	"embed"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	"github.com/fluxcd/go-git/v5"
	gogit "github.com/fluxcd/go-git/v5"
	"github.com/fluxcd/go-git/v5/config"
	"github.com/fluxcd/go-git/v5/plumbing"
	"github.com/fluxcd/go-git/v5/plumbing/object"
	"github.com/fluxcd/go-git/v5/storage/memory"
	"github.com/fluxcd/image-automation-controller/pkg/update"
	"github.com/fluxcd/pkg/gittestserver"
	"github.com/fluxcd/pkg/runtime/testenv"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	image_automationv1 "github.com/fluxcd/image-automation-controller/api/v1beta1"
	image_reflectv1 "github.com/fluxcd/image-reflector-controller/api/v1beta1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	cfgFuzz                 *rest.Config
	k8sClient               client.Client
	imageAutoReconcilerFuzz *ImageUpdateAutomationReconciler
	testEnvFuzz             *testenv.Environment
	initter                 sync.Once
)

const defaultBinVersion = "1.24"

//go:embed testdata/crd
var testFiles embed.FS

// This fuzzer randomized 2 things:
// 1: The files in the git repository
// 2: The values of ImageUpdateAutomationSpec
//
//	and ImagePolicy resources
func Fuzz_ImageUpdateReconciler(f *testing.F) {
	f.Fuzz(func(t *testing.T, seed []byte) {
		initter.Do(func() {
			utilruntime.Must(ensureDependencies(func(m manager.Manager) {
				utilruntime.Must((&ImageUpdateAutomationReconciler{
					Client: m.GetClient(),
				}).SetupWithManager(m, ImageUpdateAutomationReconcilerOptions{MaxConcurrentReconciles: 4}))
			}))
		})

		f := fuzz.NewConsumer(seed)

		// We start by creating a lot of the values that
		// need for the various resources later on
		runes := "abcdefghijklmnopqrstuvwxyz1234567890"
		branch, err := f.GetStringFrom(runes, 80)
		if err != nil {
			return
		}
		repPath, err := f.GetStringFrom(runes, 80)
		if err != nil {
			return
		}
		repositoryPath := "/config-" + repPath + ".git"

		namespaceName, err := f.GetStringFrom(runes, 59)
		if err != nil {
			return
		}

		gitRepoKeyName, err := f.GetStringFrom(runes, 80)
		if err != nil {
			return
		}

		username, err := f.GetStringFrom(runes, 80)
		if err != nil {
			return
		}
		password, err := f.GetStringFrom(runes, 80)
		if err != nil {
			return
		}

		ipSpec := image_reflectv1.ImagePolicySpec{}
		err = f.GenerateStruct(&ipSpec)
		if err != nil {
			return
		}

		ipStatus := image_reflectv1.ImagePolicyStatus{}
		err = f.GenerateStruct(&ipStatus)
		if err != nil {
			return
		}

		iuaSpec := image_automationv1.ImageUpdateAutomationSpec{}
		err = f.GenerateStruct(&iuaSpec)
		if err != nil {
			return
		}
		gitSpec := &image_automationv1.GitSpec{}
		err = f.GenerateStruct(&gitSpec)
		if err != nil {
			return
		}

		policyKeyName, err := f.GetStringFrom(runes, 80)
		if err != nil {
			return
		}

		updateKeyName, err := f.GetStringFrom("abcdefghijklmnopqrstuvwxy.-", 120)
		if err != nil {
			return
		}

		// Create random git files
		gitPath, err := os.MkdirTemp("", "git-dir-")
		if err != nil {
			return
		}
		defer os.RemoveAll(gitPath)
		err = f.CreateFiles(gitPath)
		if err != nil {
			return
		}

		// Done with creating the random values

		// Create a namespace
		namespace := &corev1.Namespace{}
		namespace.Name = namespaceName
		err = k8sClient.Create(context.Background(), namespace)
		if err != nil {
			return
		}
		defer func() {
			err = k8sClient.Delete(context.Background(), namespace)
			if err != nil {
				panic(err)
			}
			time.Sleep(80 * time.Millisecond)
		}()

		// Set up git-related stuff
		gitServer, err := gittestserver.NewTempGitServer()
		if err != nil {
			return
		}
		gitServer.Auth(username, password)
		gitServer.AutoCreate()
		err = gitServer.StartHTTP()
		if err != nil {
			return
		}
		defer func() {
			gitServer.StopHTTP()
			os.RemoveAll(gitServer.Root())
		}()
		gitServer.KeyDir(filepath.Join(gitServer.Root(), "keys"))
		err = gitServer.ListenSSH()
		if err != nil {
			return
		}
		err = initGitRepo(gitServer, gitPath, branch, repositoryPath)
		if err != nil {
			return
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
		err = k8sClient.Create(context.Background(), gitRepo)
		if err != nil {
			return
		}
		defer k8sClient.Delete(context.Background(), gitRepo)

		// Create image policy object
		policyKey := types.NamespacedName{
			Name:      "policy-" + policyKeyName,
			Namespace: namespace.Name,
		}
		policy := &image_reflectv1.ImagePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      policyKey.Name,
				Namespace: policyKey.Namespace,
			},
			Spec:   ipSpec,
			Status: ipStatus,
		}
		err = k8sClient.Create(context.Background(), policy)
		if err != nil {
			return
		}
		err = k8sClient.Status().Update(context.Background(), policy)
		if err != nil {
			return
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
		iuaSpec.Update.Strategy = image_automationv1.UpdateStrategySetters

		iua := &image_automationv1.ImageUpdateAutomation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      updateKey.Name,
				Namespace: updateKey.Namespace,
			},
			Spec: iuaSpec,
		}
		err = k8sClient.Create(context.Background(), iua)
		if err != nil {
			return
		}
		defer k8sClient.Delete(context.Background(), iua)
		time.Sleep(time.Millisecond * 70)
	})
}

// A fuzzer that is more focused on UpdateWithSetters
// that the reconciler fuzzer is
func FuzzUpdateWithSetters(f *testing.F) {
	f.Fuzz(func(t *testing.T, seed []byte) {
		f := fuzz.NewConsumer(seed)

		// Create dir1
		tmp1, err := ioutil.TempDir("", "fuzztest1")
		if err != nil {
			return
		}
		defer os.RemoveAll(tmp1)
		// Add files to dir1
		err = f.CreateFiles(tmp1)
		if err != nil {
			return
		}

		// Create dir2
		tmp2, err := ioutil.TempDir("", "fuzztest2")
		if err != nil {
			return
		}
		defer os.RemoveAll(tmp2)

		// Create policies
		policies := make([]image_reflectv1.ImagePolicy, 0)
		noOfPolicies, err := f.GetInt()
		if err != nil {
			return
		}
		for i := 0; i < noOfPolicies%10; i++ {
			policy := image_reflectv1.ImagePolicy{}
			err = f.GenerateStruct(&policy)
			if err != nil {
				return
			}
			policies = append(policies, policy)
		}

		_, _ = update.UpdateWithSetters(logr.Discard(), tmp1, tmp2, policies)
	})
}

// Initialise a git server with a repo including the files in dir.
func initGitRepo(gitServer *gittestserver.GitServer, fixture, branch, repositoryPath string) error {
	fs := memfs.New()
	repo, err := git.Init(memory.NewStorage(), fs)
	if err != nil {
		return err
	}

	err = populateRepoFromFixture(repo, fixture)
	if err != nil {
		return err
	}

	working, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err = working.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch),
		Create: true,
	}); err != nil {
		return err
	}

	remote, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{gitServer.HTTPAddressWithCredentials() + repositoryPath},
	})
	if err != nil {
		return err
	}

	return remote.Push(&git.PushOptions{
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)),
		},
	})
}

func populateRepoFromFixture(repo *gogit.Repository, fixture string) error {
	working, err := repo.Worktree()
	if err != nil {
		return err
	}
	fs := working.Filesystem

	if err = filepath.Walk(fixture, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return fs.MkdirAll(fs.Join(path[len(fixture):]), info.Mode())
		}
		// copy symlinks as-is, so I can test what happens with broken symlinks
		if info.Mode()&os.ModeSymlink > 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return fs.Symlink(target, path[len(fixture):])
		}

		fileBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		ff, err := fs.Create(path[len(fixture):])
		if err != nil {
			return err
		}
		defer ff.Close()

		_, err = ff.Write(fileBytes)
		return err
	}); err != nil {
		return err
	}

	_, err = working.Add(".")
	if err != nil {
		return err
	}

	if _, err = working.Commit("Initial revision from "+fixture, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Testbot",
			Email: "test@example.com",
			When:  time.Now(),
		},
	}); err != nil {
		return err
	}

	return nil
}

func envtestBinVersion() string {
	if binVersion := os.Getenv("ENVTEST_BIN_VERSION"); binVersion != "" {
		return binVersion
	}
	return defaultBinVersion
}

func ensureDependencies(setupReconcilers func(manager.Manager)) error {
	if _, err := os.Stat("/.dockerenv"); os.IsNotExist(err) {
		return nil
	}

	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		binVersion := envtestBinVersion()
		cmd := exec.Command("/usr/bin/bash", "-c", fmt.Sprintf(`go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest && \
		/root/go/bin/setup-envtest use -p path %s`, binVersion))

		cmd.Env = append(os.Environ(), "GOPATH=/root/go")
		assetsPath, err := cmd.Output()
		if err != nil {
			return err
		}
		os.Setenv("KUBEBUILDER_ASSETS", string(assetsPath))
	}

	// Output all embedded testdata files
	embedDirs := []string{"testdata/crd"}
	for _, dir := range embedDirs {
		err := os.MkdirAll(dir, 0o755)
		if err != nil {
			return fmt.Errorf("mkdir %s: %v", dir, err)
		}

		templates, err := fs.ReadDir(testFiles, dir)
		if err != nil {
			return fmt.Errorf("reading embedded dir: %v", err)
		}

		for _, template := range templates {
			fileName := fmt.Sprintf("%s/%s", dir, template.Name())
			fmt.Println(fileName)

			data, err := testFiles.ReadFile(fileName)
			if err != nil {
				return fmt.Errorf("reading embedded file %s: %v", fileName, err)
			}

			os.WriteFile(fileName, data, 0o644)
			if err != nil {
				return fmt.Errorf("writing %s: %v", fileName, err)
			}
		}
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("testdata", "crds"),
		},
	}
	fmt.Println("Starting the test environment")
	cfg, err := testEnv.Start()
	if err != nil {
		panic(fmt.Sprintf("Failed to start the test environment manager: %v", err))
	}

	utilruntime.Must(sourcev1.AddToScheme(scheme.Scheme))
	utilruntime.Must(image_reflectv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(image_automationv1.AddToScheme(scheme.Scheme))

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		panic(err)
	}
	if k8sClient == nil {
		panic("cfg is nil but should not be")
	}

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		panic(err)
	}

	setupReconcilers(k8sManager)

	time.Sleep(2 * time.Second)
	go func() {
		fmt.Println("Starting k8sManager...")
		utilruntime.Must(k8sManager.Start(context.TODO()))
	}()

	return nil
}
