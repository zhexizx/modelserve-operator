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
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	if err := r.Get(ctx, req.NamespacedName, &md); err != nil {
		// 拿不到对象就没法写 status，直接返回
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 1. 创建/更新子资源。出错了也不立刻 return, 但是Conflict是例外
	obs, reconcileErr := r.reconcileChildren(ctx, &md)
	if apierrors.IsConflict(reconcileErr) {
		log.Info("conflict, will retry", "reconcileErr", reconcileErr)
		return ctrl.Result{}, reconcileErr
	}

	// 2. 根据观察结果计算 status（只改内存里的 md.Status）
	applyStatus(&md, obs, reconcileErr)

	// 3. 写回集群。两个 error 都保留：reconcileErr 不能被 statusErr 盖掉；都为 nil 时结果也是 nil
	statusErr := r.Status().Update(ctx, &md)
	return ctrl.Result{}, errors.Join(reconcileErr, statusErr)
}

// reconcileChildren 创建/更新 stable、canary 两个 Deployment 和 Service，
// 返回观察到的就绪情况。遇到第一个错误就停下返回，此时 obs 可能只填了一部分。
func (r *ModelDeploymentReconciler) reconcileChildren(ctx context.Context, md *servingv1alpha1.ModelDeployment) (observation, error) {
	var obs observation

	stable, canary := splitReplicas(*md.Spec.Replicas, md.Spec.Canary)

	// stable
	stableDep, err := r.reconcileDeployment(ctx, md, trackStable, stable, md.Spec.Image, md.Spec.ModelURI)
	if err != nil {
		return obs, err
	}
	obs.stableReady = stableDep.Status.AvailableReplicas
	obs.stableDone = deploymentReady(stableDep, stable)

	// canary
	if md.Spec.Canary == nil {
		obj := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      md.Name + "-" + trackCanary,
				Namespace: md.Namespace,
			},
		}
		// canary 本来就不存在（从没开过灰度，或者上次已经删了）是正常情况，不算错误
		if err := client.IgnoreNotFound(r.Delete(ctx, obj)); err != nil {
			return obs, err
		}
		// 没有 canary = 不需要 canary Pod，也确实没有，算完成；否则整体永远不会 Ready
		obs.canaryDone = true
		obs.canaryReady = 0
	} else {
		canaryDep, err := r.reconcileDeployment(ctx, md, trackCanary, canary, md.Spec.Canary.Image, md.Spec.Canary.ModelURI)
		if err != nil {
			return obs, err
		}
		obs.canaryReady = canaryDep.Status.AvailableReplicas
		obs.canaryDone = deploymentReady(canaryDep, canary)
	}

	// Service
	if err := r.reconcileService(ctx, md); err != nil {
		return obs, err
	}

	return obs, nil
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
