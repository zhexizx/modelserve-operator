package controller

import (
	"strconv"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	labelInstance = "app.kubernetes.io/instance"
	labelTrack    = "serving.zhexi.dev/track"

	trackStable = "stable"
	trackCanary = "canary"

	volumeModel     = "model-store"
	fetchModel      = "fetch-model"
	fetchModelImage = "busybox:1.36"
	modelDir        = "/models"
	modelFile       = modelDir + "/model.txt"

	containerName = "server"
	portName      = "http"
	healthPath    = "/healthz"

	envModelPath = "MODEL_PATH"
	envPort      = "PORT"
)

func labelsFor(md *servingv1alpha1.ModelDeployment, track string) map[string]string {
	return map[string]string{
		labelInstance: md.Name,
		labelTrack:    track,
	}
}

func podTemplateFor(md *servingv1alpha1.ModelDeployment, track string, image string, modelURI string) corev1.PodTemplateSpec {
	port := *md.Spec.Port

	env := make([]corev1.EnvVar, 0, 2+len(md.Spec.Env))
	env = append(env,
		corev1.EnvVar{Name: envModelPath, Value: modelFile},
		corev1.EnvVar{Name: envPort, Value: strconv.Itoa(int(port))},
	)
	env = append(env, md.Spec.Env...)

	modelMount := []corev1.VolumeMount{
		{Name: volumeModel, MountPath: modelDir},
	}

	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labelsFor(md, track),
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: volumeModel,
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			InitContainers: []corev1.Container{
				{
					Name:         fetchModel,
					Image:        fetchModelImage,
					Command:      []string{"wget", "-O", modelFile, modelURI},
					VolumeMounts: modelMount,
				},
			},
			Containers: []corev1.Container{
				{
					Name:            containerName,
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{
						{Name: portName, ContainerPort: port},
					},
					Env:          env,
					Resources:    md.Spec.Resources,
					VolumeMounts: modelMount,
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: healthPath,
								Port: intstr.FromString(portName),
							},
						},
					},
				},
			},
		},
	}
}

func instanceLabels(md *servingv1alpha1.ModelDeployment) map[string]string {
	return map[string]string{labelInstance: md.Name}
}
