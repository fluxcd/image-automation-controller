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

package main

import (
	"fmt"
	"os"

	flag "github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	imagev1_reflect "github.com/fluxcd/image-reflector-controller/api/v1beta1"
	"github.com/fluxcd/pkg/runtime/acl"
	"github.com/fluxcd/pkg/runtime/client"
	helper "github.com/fluxcd/pkg/runtime/controller"
	"github.com/fluxcd/pkg/runtime/events"
	feathelper "github.com/fluxcd/pkg/runtime/features"
	"github.com/fluxcd/pkg/runtime/leaderelection"
	"github.com/fluxcd/pkg/runtime/logger"
	"github.com/fluxcd/pkg/runtime/metrics"
	"github.com/fluxcd/pkg/runtime/pprof"
	"github.com/fluxcd/pkg/runtime/probes"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"

	imagev1 "github.com/fluxcd/image-automation-controller/api/v1beta1"
	"github.com/fluxcd/image-automation-controller/internal/features"
	"github.com/fluxcd/source-controller/pkg/git"
	"github.com/fluxcd/source-controller/pkg/git/libgit2/managed"

	// +kubebuilder:scaffold:imports
	"github.com/fluxcd/image-automation-controller/controllers"
)

const controllerName = "image-automation-controller"

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(imagev1_reflect.AddToScheme(scheme))
	utilruntime.Must(sourcev1.AddToScheme(scheme))
	utilruntime.Must(imagev1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr           string
		eventsAddr            string
		healthAddr            string
		clientOptions         client.Options
		aclOptions            acl.Options
		logOptions            logger.Options
		leaderElectionOptions leaderelection.Options
		rateLimiterOptions    helper.RateLimiterOptions
		featureGates          feathelper.FeatureGates
		watchAllNamespaces    bool
		concurrent            int
		kexAlgos              []string
	)

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&eventsAddr, "events-addr", "", "The address of the events receiver.")
	flag.StringVar(&healthAddr, "health-addr", ":9440", "The address the health endpoint binds to.")
	flag.BoolVar(&watchAllNamespaces, "watch-all-namespaces", true,
		"Watch for custom resources in all namespaces, if set to false it will only watch the runtime namespace.")
	flag.IntVar(&concurrent, "concurrent", 4, "The number of concurrent resource reconciles.")
	flag.StringSliceVar(&kexAlgos, "ssh-kex-algos", []string{},
		"The list of key exchange algorithms to use for ssh connections, arranged from most preferred to the least.")

	clientOptions.BindFlags(flag.CommandLine)
	logOptions.BindFlags(flag.CommandLine)
	leaderElectionOptions.BindFlags(flag.CommandLine)
	aclOptions.BindFlags(flag.CommandLine)
	rateLimiterOptions.BindFlags(flag.CommandLine)
	featureGates.BindFlags(flag.CommandLine)

	flag.Parse()

	log := logger.NewLogger(logOptions)
	ctrl.SetLogger(log)

	err := featureGates.WithLogger(setupLog).
		SupportedFeatures(features.FeatureGates())
	if err != nil {
		setupLog.Error(err, "unable to load feature gates")
		os.Exit(1)
	}

	metricsRecorder := metrics.NewRecorder()
	ctrlmetrics.Registry.MustRegister(metricsRecorder.Collectors()...)

	watchNamespace := ""
	if !watchAllNamespaces {
		watchNamespace = os.Getenv("RUNTIME_NAMESPACE")
	}

	restConfig := client.GetConfigOrDie(clientOptions)
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                        scheme,
		MetricsBindAddress:            metricsAddr,
		HealthProbeBindAddress:        healthAddr,
		Port:                          9443,
		LeaderElection:                leaderElectionOptions.Enable,
		LeaderElectionReleaseOnCancel: leaderElectionOptions.ReleaseOnCancel,
		LeaseDuration:                 &leaderElectionOptions.LeaseDuration,
		RenewDeadline:                 &leaderElectionOptions.RenewDeadline,
		RetryPeriod:                   &leaderElectionOptions.RetryPeriod,
		LeaderElectionID:              fmt.Sprintf("%s-leader-election", controllerName),
		Namespace:                     watchNamespace,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	probes.SetupChecks(mgr, setupLog)
	pprof.SetupHandlers(mgr, setupLog)

	var eventRecorder *events.Recorder
	if eventRecorder, err = events.NewRecorder(mgr, ctrl.Log, eventsAddr, controllerName); err != nil {
		setupLog.Error(err, "unable to create event recorder")
		os.Exit(1)
	}

	if err = (&controllers.ImageUpdateAutomationReconciler{
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		EventRecorder:       eventRecorder,
		MetricsRecorder:     metricsRecorder,
		NoCrossNamespaceRef: aclOptions.NoCrossNamespaceRefs,
	}).SetupWithManager(mgr, controllers.ImageUpdateAutomationReconcilerOptions{
		MaxConcurrentReconciles: concurrent,
		RateLimiter:             helper.GetRateLimiter(rateLimiterOptions),
	}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImageUpdateAutomation")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if enabled, _ := features.Enabled(features.GitManagedTransport); enabled {
		managed.InitManagedTransport(ctrl.Log.WithName("managed-transport"))
	}

	setPreferredKexAlgos(kexAlgos)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func setPreferredKexAlgos(algos []string) {
	git.KexAlgos = algos
}
