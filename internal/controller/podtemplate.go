package controller

import servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"

const (
	labelInstance = "app.kubernetes.io/instance"
	labelTrack    = "serving.zhexi.dev/track"

	trackStable = "stable"
	trackCanary = "canary"
)

func labelsFor(md *servingv1alpha1.ModelDeployment, track string) map[string]string {
	return map[string]string{
		labelInstance: md.Name,
		labelTrack:    track,
	}
}
