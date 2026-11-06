package controller

import (
	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
)

func splitReplicas(total int32, canary *servingv1alpha1.CanarySpec) (stable, canaryN int32) {
	if canary == nil {
		return total, 0
	}
	canaryN = (total*canary.Weight + 50) / 100
	if total >= 2 && canary.Weight > 0 && canary.Weight < 100 {
		canaryN = min(max(canaryN, 1), total-1)
	}
	stable = total - canaryN
	return stable, canaryN
}
