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

// Package node contains the controller logic for the Nodes.
package node

import (
	"context"
	"strings"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	controllerName = "node-controller"

	errGetNode = "failed to get node"
)

// Setup sets up the controller with the Manager.
func Setup(mgr ctrl.Manager, server *Server, log logging.Logger) error {
	r := NewReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		WithEventRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(controllerName))),
		WithLogger(log.WithValues("controller", controllerName)),
	)
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).Complete(r)
}

type NodeStateWriter interface {
	SetNodeState(name string, state NodeState)
	DeleteNodeState(name string)
}

type NopNodeStateWriter struct{}

func (NopNodeStateWriter) SetNodeState(name string, state NodeState) {}
func (NopNodeStateWriter) DeleteNodeState(name string)               {}

// WithEventRecorder sets the EventRecorder for the Reconciler.
func WithEventRecorder(e event.Recorder) ReconcilerOption {
	return func(r *Reconciler) {
		r.record = e
	}
}

// WithLogger sets the Logger for the Reconciler.
func WithLogger(l logging.Logger) ReconcilerOption {
	return func(r *Reconciler) {
		r.rootLog = l
	}
}

// WithNodeStateWriter sets the NodeStateWriter for the Reconciler.
func WithNodeStateWriter(s NodeStateWriter) ReconcilerOption {
	return func(r *Reconciler) {
		r.nodes = s
	}
}

// ReconcilerOption is a function that sets some option on the Reconciler.
type ReconcilerOption func(*Reconciler)

// NewReconciler returns a new Reconciler.
func NewReconciler(c client.Client, s *runtime.Scheme, opts ...ReconcilerOption) *Reconciler {
	r := &Reconciler{
		client:  c,
		Scheme:  s,
		record:  event.NewNopRecorder(),
		rootLog: logging.NewNopLogger(),
		nodes:   NopNodeStateWriter{},
	}
	for _, f := range opts {
		f(r)
	}
	return r
}

// Reconciler reconciles a Node object to detect shutdown events and notify Playground pods running on that Node.
type Reconciler struct {
	client client.Client
	Scheme *runtime.Scheme

	record  event.Recorder
	rootLog logging.Logger

	nodes NodeStateWriter
}

// Reconcile gets triggered by every event on Node resources.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.rootLog.WithValues("node", req.NamespacedName)

	n := &corev1.Node{}
	if err := r.client.Get(ctx, req.NamespacedName, n); err != nil {
		if kerrors.IsNotFound(err) {
			r.nodes.DeleteNodeState(req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, errors.Wrap(err, errGetNode)
	}
	var readyCondition corev1.NodeCondition
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			readyCondition = c
			break
		}
	}
	// NOTE(muvaf): This covers GKE node shutdown event. It may or may not work for Kubernetes deployments.
	if !(readyCondition.Status == corev1.ConditionFalse &&
		readyCondition.Reason == "KubeletNotReady" &&
		strings.Contains(readyCondition.Message, "node is shutting down")) {
		return ctrl.Result{}, nil
	}
	log.Debug("node is shutting down", "node", n.Name, "phase", n.Status.Phase)
	r.nodes.SetNodeState(n.Name, NodeStateShuttingDown)
	return ctrl.Result{}, nil
}
