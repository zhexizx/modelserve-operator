package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
)

const (
	servicePort = 80
)

// reconcileService 确保 Service <name> 存在，并且同时选中 stable 和 canary 的 Pod，
// 流量按两边的 Pod 数量自然分配（这就是金丝雀的分流方式）。
func (r *ModelDeploymentReconciler) reconcileService(ctx context.Context, md *servingv1alpha1.ModelDeployment) error {
	log := logf.FromContext(ctx)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      md.Name,
			Namespace: md.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Spec.Selector = instanceLabels(md)

		svc.Spec.Ports = []corev1.ServicePort{
			{
				Name:       portName,
				Port:       servicePort,
				TargetPort: intstr.FromString(portName),
			},
		}

		return controllerutil.SetControllerReference(md, svc, r.Scheme)
	})
	if err != nil {
		return err
	}

	log.Info("reconciled service", "name", svc.Name, "operation", op)
	return nil
}
