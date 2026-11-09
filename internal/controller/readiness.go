package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
)

type observation struct {
	stableReady int32
	canaryReady int32
	stableDone  bool
	canaryDone  bool
}

func deploymentReady(dep *appsv1.Deployment, want int32) bool {
	status := dep.Status
	complete := status.ObservedGeneration >= dep.Generation &&
		status.UpdatedReplicas == want && status.AvailableReplicas == want && status.Replicas == want

	return complete
}

func setStatusConditions(md *servingv1alpha1.ModelDeployment, obs observation, reconcileErr error) {
	switch {
	case reconcileErr != nil:
		setCond(md, servingv1alpha1.ConditionDegraded, metav1.ConditionTrue, servingv1alpha1.ReasonReconcileError, reconcileErr.Error())
		setCond(md, servingv1alpha1.ConditionReady, metav1.ConditionFalse, servingv1alpha1.ReasonReconcileError, "")
		setCond(md, servingv1alpha1.ConditionProgressing, metav1.ConditionFalse, servingv1alpha1.ReasonReconcileError, "")
	case obs.canaryDone && obs.stableDone:
		setCond(md, servingv1alpha1.ConditionDegraded, metav1.ConditionFalse, servingv1alpha1.ReasonReconcileSuccess, "")
		setCond(md, servingv1alpha1.ConditionReady, metav1.ConditionTrue, servingv1alpha1.ReasonAllReplicasReady, "")
		setCond(md, servingv1alpha1.ConditionProgressing, metav1.ConditionFalse, servingv1alpha1.ReasonRolloutComplete, "")
	default:
		setCond(md, servingv1alpha1.ConditionDegraded, metav1.ConditionFalse, servingv1alpha1.ReasonReconcileSuccess, "")
		setCond(md, servingv1alpha1.ConditionReady, metav1.ConditionFalse, servingv1alpha1.ReasonReplicasNotReady, "")
		setCond(md, servingv1alpha1.ConditionProgressing, metav1.ConditionTrue, servingv1alpha1.ReasonRollingOut, "")
	}
}

func setCond(md *servingv1alpha1.ModelDeployment, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&md.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: md.Generation,
	})
}
