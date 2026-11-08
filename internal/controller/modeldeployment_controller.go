/*
Copyright 2026.

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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// ModelDeploymentReconciler reconciles a ModelDeployment object
type ModelDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=serving.zhexi.dev,resources=modeldeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=serving.zhexi.dev,resources=modeldeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=serving.zhexi.dev,resources=modeldeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ModelDeployment object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.25.0/pkg/reconcile
func (r *ModelDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var md servingv1alpha1.ModelDeployment
	err := r.Get(ctx, req.NamespacedName, &md)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	stable, canary := splitReplicas(*md.Spec.Replicas, md.Spec.Canary)
	log.Info("split replicas", "stable", stable, "canary", canary)

	if err := r.reconcileDeployment(ctx, &md, trackStable, stable, md.Spec.Image, md.Spec.ModelURI); err != nil {
		return ctrl.Result{}, err
	}

	if md.Spec.Canary == nil {
		obj := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      md.Name + "-" + trackCanary,
				Namespace: md.Namespace,
			},
		}
		// canary 本来就不存在（从没开过灰度，或者上次已经删了）是正常情况，不算错误
		if err := client.IgnoreNotFound(r.Delete(ctx, obj)); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err := r.reconcileDeployment(ctx, &md, trackCanary, canary, md.Spec.Canary.Image, md.Spec.Canary.ModelURI); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcileService(ctx, &md); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModelDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&servingv1alpha1.ModelDeployment{}).
		Named("modeldeployment").
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
