package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
)

const testName = "foo"

func TestLabelsFor(t *testing.T) {
	md := &servingv1alpha1.ModelDeployment{}
	md.Name = testName

	got := labelsFor(md, trackCanary)

	if got[labelInstance] != testName {
		t.Errorf("instance label = %q, want %q", got[labelInstance], testName)
	}
	if got[labelTrack] != trackCanary {
		t.Errorf("track label = %q, want %q", got[labelTrack], trackCanary)
	}
	if len(got) != 2 {
		t.Errorf("got %d labels, want 2: %v", len(got), got)
	}
}

func TestPodTemplateFor(t *testing.T) {
	const (
		image    = "model-server:v2"
		modelURI = "https://example.com/model-v2.txt"
	)

	md := &servingv1alpha1.ModelDeployment{}
	md.Name = testName
	// 故意不用默认的 8080，确认端口真的是从 spec 里取的
	md.Spec.Port = ptr.To[int32](9090)
	md.Spec.Env = []corev1.EnvVar{{Name: "API_KEY", Value: "secret"}}
	md.Spec.Resources = corev1.ResourceRequirements{
		Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("128Mi")},
	}

	got := podTemplateFor(md, trackCanary, image, modelURI)

	// 标签
	if got.Labels[labelInstance] != testName || got.Labels[labelTrack] != trackCanary {
		t.Errorf("labels = %v", got.Labels)
	}

	// 卷：一个 emptyDir
	if len(got.Spec.Volumes) != 1 {
		t.Fatalf("got %d volumes, want 1", len(got.Spec.Volumes))
	}
	vol := got.Spec.Volumes[0]
	if vol.Name != volumeModel || vol.EmptyDir == nil {
		t.Errorf("volume = %+v, want emptyDir named %q", vol, volumeModel)
	}

	// initContainer：用 busybox 把 modelURI 下载到 modelFile
	if len(got.Spec.InitContainers) != 1 {
		t.Fatalf("got %d init containers, want 1", len(got.Spec.InitContainers))
	}
	initC := got.Spec.InitContainers[0]
	if initC.Image != fetchModelImage {
		t.Errorf("init image = %q, want %q", initC.Image, fetchModelImage)
	}
	wantCmd := []string{"wget", "-O", modelFile, modelURI}
	if strings.Join(initC.Command, " ") != strings.Join(wantCmd, " ") {
		t.Errorf("init command = %v, want %v", initC.Command, wantCmd)
	}

	// 主容器
	if len(got.Spec.Containers) != 1 {
		t.Fatalf("got %d containers, want 1", len(got.Spec.Containers))
	}
	c := got.Spec.Containers[0]
	if c.Image != image {
		t.Errorf("image = %q, want %q", c.Image, image)
	}
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != 9090 || c.Ports[0].Name != portName {
		t.Errorf("ports = %+v, want [{Name:%s ContainerPort:9090}]", c.Ports, portName)
	}
	if !c.Resources.Limits.Memory().Equal(resource.MustParse("128Mi")) {
		t.Errorf("memory limit = %v, want 128Mi", c.Resources.Limits.Memory())
	}

	// env：operator 的两条在前，用户的接在后面
	wantEnv := []corev1.EnvVar{
		{Name: envModelPath, Value: modelFile},
		{Name: envPort, Value: "9090"},
		{Name: "API_KEY", Value: "secret"},
	}
	if len(c.Env) != len(wantEnv) {
		t.Fatalf("env = %+v, want %+v", c.Env, wantEnv)
	}
	for i := range wantEnv {
		if c.Env[i] != wantEnv[i] {
			t.Errorf("env[%d] = %+v, want %+v", i, c.Env[i], wantEnv[i])
		}
	}

	// 路径一致：两个容器挂同一个卷到 modelDir，下载的文件在挂载目录下面
	for _, ctr := range []corev1.Container{initC, c} {
		if len(ctr.VolumeMounts) != 1 ||
			ctr.VolumeMounts[0].Name != vol.Name ||
			ctr.VolumeMounts[0].MountPath != modelDir {
			t.Errorf("%s volumeMounts = %+v, want %q at %q", ctr.Name, ctr.VolumeMounts, vol.Name, modelDir)
		}
	}
	if !strings.HasPrefix(modelFile, modelDir+"/") {
		t.Errorf("modelFile %q is not under mount dir %q", modelFile, modelDir)
	}

	// readinessProbe：按端口名访问 /healthz
	p := c.ReadinessProbe
	if p == nil || p.HTTPGet == nil {
		t.Fatalf("readinessProbe = %+v, want httpGet", p)
	}
	if p.HTTPGet.Path != healthPath || p.HTTPGet.Port.String() != portName {
		t.Errorf("probe = %s %s, want %s %s", p.HTTPGet.Port.String(), p.HTTPGet.Path, portName, healthPath)
	}
}

// 用户追加的 env 不能改到 spec 里的原切片
func TestPodTemplateForDoesNotMutateSpecEnv(t *testing.T) {
	md := &servingv1alpha1.ModelDeployment{}
	md.Spec.Port = ptr.To[int32](8080)
	md.Spec.Env = []corev1.EnvVar{{Name: "A", Value: "1"}}

	_ = podTemplateFor(md, trackStable, "img", "uri")

	if len(md.Spec.Env) != 1 || md.Spec.Env[0].Name != "A" {
		t.Errorf("spec.env was mutated: %+v", md.Spec.Env)
	}
}
