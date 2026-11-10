package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
)

// 这里测的不是 Go 代码，而是 *_types.go 里的 +kubebuilder:validation / default marker
// 经过 make manifests 生成到 CRD 以后，apiserver 是否真的按它们校验。
// 只 Create，不 Reconcile。
var _ = Describe("ModelDeployment schema validation", func() {
	ctx := context.Background()

	type mutation func(md *servingv1alpha1.ModelDeployment)

	withCanary := func(weight int32) mutation {
		return func(md *servingv1alpha1.ModelDeployment) {
			md.Spec.Canary = &servingv1alpha1.CanarySpec{
				Image:    testImageV2,
				ModelURI: testModelURIV2,
				Weight:   weight,
			}
		}
	}

	DescribeTable("rejects invalid specs",
		func(mutate mutation) {
			md := createMinimumModelDeployment("invalid")
			mutate(md)
			err := k8sClient.Create(ctx, md)
			Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected Invalid, got err=%v", err)
		},
		Entry("empty image", func(md *servingv1alpha1.ModelDeployment) { md.Spec.Image = "" }),
		Entry("modelURI with ftp scheme", func(md *servingv1alpha1.ModelDeployment) { md.Spec.ModelURI = "ftp://example.com/m.txt" }),
		Entry("modelURI without scheme", func(md *servingv1alpha1.ModelDeployment) { md.Spec.ModelURI = "models/m.txt" }),
		Entry("replicas 0", func(md *servingv1alpha1.ModelDeployment) { md.Spec.Replicas = ptr.To[int32](0) }),
		Entry("port 0", func(md *servingv1alpha1.ModelDeployment) { md.Spec.Port = ptr.To[int32](0) }),
		Entry("port 70000", func(md *servingv1alpha1.ModelDeployment) { md.Spec.Port = ptr.To[int32](70000) }),
		Entry("canary weight -1", withCanary(-1)),
		Entry("canary weight 150", withCanary(150)),
		Entry("empty canary image", func(md *servingv1alpha1.ModelDeployment) {
			withCanary(20)(md)
			md.Spec.Canary.Image = ""
		}),
		Entry("canary modelURI without scheme", func(md *servingv1alpha1.ModelDeployment) {
			withCanary(20)(md)
			md.Spec.Canary.ModelURI = "models/m.txt"
		}),
	)

	// 只测非法值的话，边界写错（比如写成 ExclusiveMaximum）是发现不了的，所以边界值也要能创建成功
	DescribeTable("accepts boundary values",
		func(name string, mutate mutation) {
			md := createMinimumModelDeployment(name)
			mutate(md)
			Expect(k8sClient.Create(ctx, md)).To(Succeed())
			Expect(k8sClient.Delete(ctx, md)).To(Succeed())
		},
		Entry("replicas 1", "valid-replicas-1", func(md *servingv1alpha1.ModelDeployment) { md.Spec.Replicas = ptr.To[int32](1) }),
		Entry("port 1", "valid-port-1", func(md *servingv1alpha1.ModelDeployment) { md.Spec.Port = ptr.To[int32](1) }),
		Entry("port 65535", "valid-port-65535", func(md *servingv1alpha1.ModelDeployment) { md.Spec.Port = ptr.To[int32](65535) }),
		Entry("http modelURI", "valid-http", func(md *servingv1alpha1.ModelDeployment) { md.Spec.ModelURI = "http://example.com/m.txt" }),
		Entry("canary weight 0", "valid-weight-0", withCanary(0)),
		Entry("canary weight 100", "valid-weight-100", withCanary(100)),
	)
})
