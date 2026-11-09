package controller

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
)

// reconcileDeployment 确保 <name>-<track> 存在且符合期望，返回集群里的最新对象（调用方用它的 Status 判断就绪情况）。
func (r *ModelDeploymentReconciler) reconcileDeployment(
	ctx context.Context,
	md *servingv1alpha1.ModelDeployment,
	track string,
	replicas int32,
	image, modelURI string,
) (*appsv1.Deployment, error) {
	log := logf.FromContext(ctx)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      md.Name + "-" + track,
			Namespace: md.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		dep.Spec.Replicas = &replicas
		dep.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: labelsFor(md, track),
		}
		dep.Spec.Template = podTemplateFor(md, track, image, modelURI)
		return controllerutil.SetControllerReference(md, dep, r.Scheme)
	})
	if err != nil {
		return nil, err
	}

	log.Info("reconciled deployment", "name", dep.Name, "replicas", replicas, "operation", op)
	// CreateOrUpdate 执行完后，dep 里是 API server 返回的最新对象，dep.Status 可以直接读
	return dep, nil
}
