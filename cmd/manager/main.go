/*
Copyright 2024 QA Wolf Inc.

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
	"flag"
	"net/http"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/qawolf/crik/internal/controller/node"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

func main() {
	var metricsPort string
	var healthProbesPort string
	var serverPort string
	var debug bool
	flag.StringVar(&metricsPort, "metrics-port", "8080", "The port used by the metrics server.")
	flag.StringVar(&healthProbesPort, "health-probes-port", "8081", "The port used to serve health probe endpoints.")
	flag.StringVar(&serverPort, "port", "9376", "The port used to serve node state endpoint.")
	flag.BoolVar(&debug, "debug", false, "Turn on debug logs.")
	flag.Parse()
	var zlog logr.Logger
	if debug {
		zlog = zap.New(
			zap.UseDevMode(true),
			zap.Level(zapcore.DebugLevel),
		)
	} else {
		zlog = zap.New(
			zap.UseDevMode(false),
		)
	}
	log := logging.NewLogrLogger(zlog)
	ctrl.SetLogger(zlog)
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Logger:                 zlog,
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: ":" + metricsPort},
		HealthProbeBindAddress: ":" + healthProbesPort,
		// We don't need Node controller to be a singleton since it doesn't manipulate any state.
		LeaderElection: false,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	s := node.NewServer()
	go func() {
		if err := (&http.Server{
			Addr:              ":" + serverPort,
			Handler:           s,
			ReadHeaderTimeout: 1 * time.Second,
		}).ListenAndServe(); err != nil {
			setupLog.Error(err, "unable to start server")
			os.Exit(1)
		}
	}()
	if err := node.Setup(mgr, s, log); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Node")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
