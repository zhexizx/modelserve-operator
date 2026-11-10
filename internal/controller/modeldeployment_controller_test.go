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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
)

// 注意：envtest 里只有 etcd + kube-apiserver，没有 kube-controller-manager：
//   - Deployment 的 status 永远是空的 → MD 永远是 Progressing（除非测试自己填 Deployment 的 status）
//   - 没有 garbage collector → 删掉 MD 后子资源还在。所以每个 It 用不同的 MD 名字，互不干扰
var _ = Describe("ModelDeployment Controller", func() {
	ctx := context.Background()

	var controllerReconciler *ModelDeploymentReconciler

	BeforeEach(func() {
		controllerReconciler = &ModelDeploymentReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	})

	// reconcileMD 只传名字给 reconciler，它会自己去 apiserver 读 md
	reconcileMD := func(md *servingv1alpha1.ModelDeployment) error {
		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(md),
		})
		return err
	}

	// childKey 返回 md 的子资源的 key（和 md 同一个 namespace）
	childKey := func(md *servingv1alpha1.ModelDeployment, name string) types.NamespacedName {
		return types.NamespacedName{Name: name, Namespace: md.Namespace}
	}
	stableKeyOf := func(md *servingv1alpha1.ModelDeployment) types.NamespacedName {
		return childKey(md, md.Name+"-"+trackStable)
	}
	canaryKeyOf := func(md *servingv1alpha1.ModelDeployment) types.NamespacedName {
		return childKey(md, md.Name+"-"+trackCanary)
	}

	// refresh 从 apiserver 重新读 md：reconciler 写的 status、apiserver 改的 resourceVersion 都只在 apiserver 里
	refresh := func(md *servingv1alpha1.ModelDeployment) {
		GinkgoHelper()
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(md), md)).To(Succeed())
	}

	// updateMD 先读最新的 md 再改再提交，避免 resourceVersion 过旧导致 Conflict
	updateMD := func(md *servingv1alpha1.ModelDeployment, mutate func(*servingv1alpha1.ModelDeployment)) {
		GinkgoHelper()
		refresh(md)
		mutate(md)
		Expect(k8sClient.Update(ctx, md)).To(Succeed())
	}

	getDeployment := func(key types.NamespacedName) *appsv1.Deployment {
		GinkgoHelper()
		dep := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, key, dep)).To(Succeed())
		return dep
	}

	expectNotFound := func(key types.NamespacedName, obj client.Object) {
		GinkgoHelper()
		err := k8sClient.Get(ctx, key, obj)
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected %s to be NotFound, got err=%v", key, err)
	}

	// expectControlledBy 检查 obj 有且只有一条指向 md 的 controller ownerReference。
	// 比较 UID 而不只是名字：同名的 MD 删掉重建后 UID 会变，级联删除也是按 UID 判断的。
	expectControlledBy := func(obj client.Object, md *servingv1alpha1.ModelDeployment) {
		GinkgoHelper()
		refs := obj.GetOwnerReferences()
		Expect(refs).To(HaveLen(1))
		ref := refs[0]
		Expect(ref.Kind).To(Equal("ModelDeployment"))
		Expect(ref.Name).To(Equal(md.Name))
		Expect(ref.UID).To(Equal(md.UID))
		Expect(ref.Controller).To(HaveValue(BeTrue()))
	}

	// expectTrackLabels 检查 Deployment 的 selector 和 Pod 模板标签都是 {instance: md 名字, track: track}。
	// 期望值用常量独立写出来，不调用 labelsFor（否则 labelsFor 写错时两边一起错，测试照样通过）。
	expectTrackLabels := func(dep *appsv1.Deployment, md *servingv1alpha1.ModelDeployment, track string) {
		GinkgoHelper()
		want := map[string]string{labelInstance: md.Name, labelTrack: track}
		Expect(dep.Spec.Selector.MatchLabels).To(Equal(want))
		Expect(dep.Spec.Template.Labels).To(Equal(want))
	}

	expectCondition := func(md *servingv1alpha1.ModelDeployment, condType string, status metav1.ConditionStatus, reason string) {
		GinkgoHelper()
		c := meta.FindStatusCondition(md.Status.Conditions, condType)
		Expect(c).NotTo(BeNil(), "condition %s not found", condType)
		Expect(c.Status).To(Equal(status), "condition %s", condType)
		Expect(c.Reason).To(Equal(reason), "condition %s", condType)
		Expect(c.ObservedGeneration).To(Equal(md.Generation), "condition %s", condType)
	}

	// markAvailable 扮演 envtest 里不存在的 Deployment controller：把 Deployment 的 status 填成"升级完成"
	markAvailable := func(key types.NamespacedName) {
		GinkgoHelper()
		dep := getDeployment(key)
		want := *dep.Spec.Replicas
		dep.Status = appsv1.DeploymentStatus{
			ObservedGeneration: dep.Generation,
			Replicas:           want,
			UpdatedReplicas:    want,
			ReadyReplicas:      want,
			AvailableReplicas:  want,
		}
		Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())
	}

	Context("When creating a ModelDeployment without canary", func() {
		It("creates stable deployment and service with defaults", func() {
			By("creating a minimal ModelDeployment")
			md := createMinimumModelDeployment("basic")
			Expect(k8sClient.Create(ctx, md)).To(Succeed())

			By("checking that defaults are filled by the apiserver")
			fetched := &servingv1alpha1.ModelDeployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(md), fetched)).To(Succeed())
			Expect(fetched.Spec.Replicas).To(HaveValue(Equal(int32(1))))
			Expect(fetched.Spec.Port).To(HaveValue(Equal(int32(8080))))

			// stable Deployment 的名字，和 deployment.go 里的命名规则一致：<md 名字>-<track>
			stableKey := childKey(md, md.Name+"-"+trackStable)
			stable := &appsv1.Deployment{}

			By("reconciling")
			Expect(reconcileMD(md)).To(Succeed())
			Expect(k8sClient.Get(ctx, stableKey, stable)).To(Succeed())
			firstGen := stable.Generation // 复制出数值，下一次 Get 会覆盖 stable

			By("reconciling again should change nothing")
			// 第二次走 CreateOrUpdate 的 update 分支，用来验证 Reconcile 是幂等的：
			// spec 没变，stable 的 generation 也不能变（变了就说明每次 Reconcile 都会触发滚动升级）
			Expect(reconcileMD(md)).To(Succeed())
			Expect(k8sClient.Get(ctx, stableKey, stable)).To(Succeed())
			Expect(stable.Generation).To(Equal(firstGen))

			By("checking the stable deployment")
			Expect(stable.Spec.Replicas).To(HaveValue(Equal(int32(1))))
			Expect(stable.Spec.Selector.MatchLabels).To(HaveKeyWithValue(labelTrack, trackStable))
			expectTrackLabels(stable, md, trackStable)
			expectControlledBy(stable, md)

			By("checking the pod template uses the spec values")
			Expect(stable.Spec.Template.Spec.Containers).To(HaveLen(1))
			server := stable.Spec.Template.Spec.Containers[0]
			Expect(server.Image).To(Equal(md.Spec.Image))
			Expect(server.Ports).To(ConsistOf(HaveField("ContainerPort", int32(8080))))
			Expect(stable.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			Expect(stable.Spec.Template.Spec.InitContainers[0].Command).To(ContainElement(md.Spec.ModelURI))

			By("checking the service")
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, childKey(md, md.Name), svc)).To(Succeed())
			// Service 只按 instance 选，不带 track：这样 stable 和 canary 的 Pod 都会被选中，按 Pod 数分流
			Expect(svc.Spec.Selector).To(Equal(map[string]string{labelInstance: md.Name}))
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(80)))
			Expect(svc.Spec.Ports[0].TargetPort).To(Equal(intstr.FromString(portName)))
			expectControlledBy(svc, md)

			By("checking that no canary deployment exists")
			expectNotFound(canaryKeyOf(md), &appsv1.Deployment{})

			By("checking the status")
			// 手上的 md 是 Create 时的快照，reconciler 写的 status 只在 apiserver 里，要重新读
			refresh(md)
			Expect(md.Status.ObservedGeneration).To(Equal(md.Generation))
			// envtest 里没有 Deployment controller，stable 永远不会 available → 一直在升级中
			expectCondition(md, servingv1alpha1.ConditionDegraded, metav1.ConditionFalse, servingv1alpha1.ReasonReconcileSuccess)
			expectCondition(md, servingv1alpha1.ConditionReady, metav1.ConditionFalse, servingv1alpha1.ReasonReplicasNotReady)
			expectCondition(md, servingv1alpha1.ConditionProgressing, metav1.ConditionTrue, servingv1alpha1.ReasonRollingOut)
			Expect(md.Status.StableReadyReplicas).To(Equal(int32(0)))
			Expect(md.Status.CanaryReadyReplicas).To(Equal(int32(0)))
		})

		It("returns no error when the ModelDeployment does not exist", func() {
			md := createMinimumModelDeployment("never-created")
			Expect(reconcileMD(md)).To(Succeed())
		})
	})

	Context("When creating a ModelDeployment with canary", func() {
		It("splits replicas and uses the canary image and model", func() {
			By("creating a ModelDeployment with 5 replicas and canary weight 20")
			md := createCanaryModelDeployment("with-canary")
			Expect(k8sClient.Create(ctx, md)).To(Succeed())
			Expect(reconcileMD(md)).To(Succeed())

			By("checking the stable deployment")
			stable := getDeployment(stableKeyOf(md))
			Expect(stable.Spec.Replicas).To(HaveValue(Equal(int32(4))))
			expectTrackLabels(stable, md, trackStable)
			Expect(stable.Spec.Template.Spec.Containers[0].Image).To(Equal(md.Spec.Image))
			Expect(stable.Spec.Template.Spec.InitContainers[0].Command).To(ContainElement(md.Spec.ModelURI))
			expectControlledBy(stable, md)

			By("checking the canary deployment")
			canary := getDeployment(canaryKeyOf(md))
			Expect(canary.Spec.Replicas).To(HaveValue(Equal(int32(1))))
			expectTrackLabels(canary, md, trackCanary)
			Expect(canary.Spec.Template.Spec.Containers[0].Image).To(Equal(md.Spec.Canary.Image))
			Expect(canary.Spec.Template.Spec.InitContainers[0].Command).To(ContainElement(md.Spec.Canary.ModelURI))
			expectControlledBy(canary, md)

			By("checking that one service selects both tracks")
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, childKey(md, md.Name), svc)).To(Succeed())
			Expect(svc.Spec.Selector).To(Equal(map[string]string{labelInstance: md.Name}))
		})
	})

	Context("When updating a ModelDeployment", func() {
		It("follows weight changes and deletes canary when the canary section is removed", func() {
			By("creating with weight 20")
			md := createCanaryModelDeployment("update-weight")
			Expect(k8sClient.Create(ctx, md)).To(Succeed())
			Expect(reconcileMD(md)).To(Succeed())
			Expect(getDeployment(stableKeyOf(md)).Spec.Replicas).To(HaveValue(Equal(int32(4))))
			Expect(getDeployment(canaryKeyOf(md)).Spec.Replicas).To(HaveValue(Equal(int32(1))))

			By("changing weight to 60")
			updateMD(md, func(md *servingv1alpha1.ModelDeployment) { md.Spec.Canary.Weight = 60 })
			Expect(reconcileMD(md)).To(Succeed())
			Expect(getDeployment(stableKeyOf(md)).Spec.Replicas).To(HaveValue(Equal(int32(2))))
			Expect(getDeployment(canaryKeyOf(md)).Spec.Replicas).To(HaveValue(Equal(int32(3))))
			refresh(md)
			Expect(md.Status.ObservedGeneration).To(Equal(md.Generation))

			By("removing the canary section")
			updateMD(md, func(md *servingv1alpha1.ModelDeployment) { md.Spec.Canary = nil })
			Expect(reconcileMD(md)).To(Succeed())
			expectNotFound(canaryKeyOf(md), &appsv1.Deployment{})
			Expect(getDeployment(stableKeyOf(md)).Spec.Replicas).To(HaveValue(Equal(int32(5))))

			By("reconciling again after canary is gone")
			// canary 已经不存在了，再删一次会得到 NotFound，必须被忽略
			Expect(reconcileMD(md)).To(Succeed())
		})

		It("rolls out a new image to the stable deployment", func() {
			md := createMinimumModelDeployment("update-image")
			Expect(k8sClient.Create(ctx, md)).To(Succeed())
			Expect(reconcileMD(md)).To(Succeed())
			oldGen := getDeployment(stableKeyOf(md)).Generation

			By("changing the image")
			updateMD(md, func(md *servingv1alpha1.ModelDeployment) { md.Spec.Image = testImageV2 })
			Expect(reconcileMD(md)).To(Succeed())

			// 和幂等测试相反：spec 变了，Deployment 的 generation 应该增加（会触发滚动升级）
			stable := getDeployment(stableKeyOf(md))
			Expect(stable.Spec.Template.Spec.Containers[0].Image).To(Equal(testImageV2))
			Expect(stable.Generation).To(BeNumerically(">", oldGen))
		})
	})

	Context("When child resources are changed by someone else", func() {
		It("keeps the service ClusterIP on update and recreates a deleted service", func() {
			md := createMinimumModelDeployment("self-heal")
			Expect(k8sClient.Create(ctx, md)).To(Succeed())
			Expect(reconcileMD(md)).To(Succeed())

			svcKey := childKey(md, md.Name)
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, svcKey, svc)).To(Succeed())
			oldUID, oldClusterIP := svc.UID, svc.Spec.ClusterIP
			Expect(oldClusterIP).NotTo(BeEmpty())

			By("reconciling again keeps the same service")
			Expect(reconcileMD(md)).To(Succeed())
			Expect(k8sClient.Get(ctx, svcKey, svc)).To(Succeed())
			Expect(svc.UID).To(Equal(oldUID))
			Expect(svc.Spec.ClusterIP).To(Equal(oldClusterIP))

			By("deleting the service")
			Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
			expectNotFound(svcKey, &corev1.Service{})

			By("reconciling recreates it")
			Expect(reconcileMD(md)).To(Succeed())
			recreated := &corev1.Service{}
			Expect(k8sClient.Get(ctx, svcKey, recreated)).To(Succeed())
			Expect(recreated.UID).NotTo(Equal(oldUID))
			expectControlledBy(recreated, md)
		})

		It("restores the replica count when someone scales the deployment by hand", func() {
			md := createMinimumModelDeployment("manual-scale")
			md.Spec.Replicas = ptr.To[int32](3)
			Expect(k8sClient.Create(ctx, md)).To(Succeed())
			Expect(reconcileMD(md)).To(Succeed())

			By("scaling stable to 10 by hand")
			stable := getDeployment(stableKeyOf(md))
			stable.Spec.Replicas = ptr.To[int32](10)
			Expect(k8sClient.Update(ctx, stable)).To(Succeed())

			By("reconciling restores it")
			Expect(reconcileMD(md)).To(Succeed())
			Expect(getDeployment(stableKeyOf(md)).Spec.Replicas).To(HaveValue(Equal(int32(3))))
		})
	})

	Context("When the deployments become available", func() {
		// envtest 没有 Deployment controller，这里由测试自己填 Deployment 的 status，
		// 把 deploymentReady → applyStatus → Status().Update 整条链路接起来测
		It("reports Ready, then Progressing again after a spec change", func() {
			md := createCanaryModelDeployment("becomes-ready")
			Expect(k8sClient.Create(ctx, md)).To(Succeed())
			Expect(reconcileMD(md)).To(Succeed())

			By("pretending both deployments finished rolling out")
			markAvailable(stableKeyOf(md))
			markAvailable(canaryKeyOf(md))
			Expect(reconcileMD(md)).To(Succeed())

			refresh(md)
			Expect(md.Status.ObservedGeneration).To(Equal(md.Generation))
			expectCondition(md, servingv1alpha1.ConditionReady, metav1.ConditionTrue, servingv1alpha1.ReasonAllReplicasReady)
			expectCondition(md, servingv1alpha1.ConditionProgressing, metav1.ConditionFalse, servingv1alpha1.ReasonRolloutComplete)
			expectCondition(md, servingv1alpha1.ConditionDegraded, metav1.ConditionFalse, servingv1alpha1.ReasonReconcileSuccess)
			Expect(md.Status.StableReadyReplicas).To(Equal(int32(4)))
			Expect(md.Status.CanaryReadyReplicas).To(Equal(int32(1)))

			By("changing weight: the deployments' status is now stale")
			// Deployment 的 spec 变了，generation 增加，但（假装的）Deployment controller 还没处理
			updateMD(md, func(md *servingv1alpha1.ModelDeployment) { md.Spec.Canary.Weight = 60 })
			Expect(reconcileMD(md)).To(Succeed())

			refresh(md)
			Expect(md.Status.ObservedGeneration).To(Equal(md.Generation))
			expectCondition(md, servingv1alpha1.ConditionReady, metav1.ConditionFalse, servingv1alpha1.ReasonReplicasNotReady)
			expectCondition(md, servingv1alpha1.ConditionProgressing, metav1.ConditionTrue, servingv1alpha1.ReasonRollingOut)

			By("finishing the new rollout")
			markAvailable(stableKeyOf(md))
			markAvailable(canaryKeyOf(md))
			Expect(reconcileMD(md)).To(Succeed())

			refresh(md)
			expectCondition(md, servingv1alpha1.ConditionReady, metav1.ConditionTrue, servingv1alpha1.ReasonAllReplicasReady)
			Expect(md.Status.StableReadyReplicas).To(Equal(int32(2)))
			Expect(md.Status.CanaryReadyReplicas).To(Equal(int32(3)))
		})

		It("is Ready without canary pods after the canary section is removed", func() {
			md := createCanaryModelDeployment("ready-no-canary")
			Expect(k8sClient.Create(ctx, md)).To(Succeed())
			Expect(reconcileMD(md)).To(Succeed())

			updateMD(md, func(md *servingv1alpha1.ModelDeployment) { md.Spec.Canary = nil })
			Expect(reconcileMD(md)).To(Succeed())
			markAvailable(stableKeyOf(md))
			Expect(reconcileMD(md)).To(Succeed())

			// 没有 canary 时 canaryDone 必须算 true，否则永远不会 Ready（05 里的坑）
			refresh(md)
			expectCondition(md, servingv1alpha1.ConditionReady, metav1.ConditionTrue, servingv1alpha1.ReasonAllReplicasReady)
			Expect(md.Status.StableReadyReplicas).To(Equal(int32(5)))
			Expect(md.Status.CanaryReadyReplicas).To(Equal(int32(0)))
		})
	})
})

// 测试用的镜像和模型地址：v1 给 stable，v2 给 canary 或升级后的镜像
const (
	testImageV1    = "model-server:v1"
	testImageV2    = "model-server:v2"
	testModelURIV1 = "https://example.com/keywords-v1.txt"
	testModelURIV2 = "https://example.com/keywords-v2.txt"
)

func createMinimumModelDeployment(name string) *servingv1alpha1.ModelDeployment {
	md := &servingv1alpha1.ModelDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: servingv1alpha1.ModelDeploymentSpec{
			Image:    testImageV1,
			ModelURI: testModelURIV1,
		},
	}

	return md
}

// createCanaryModelDeployment 在最小 MD 的基础上设成 5 个副本、canary weight 20（canary 用 v2 镜像和模型），
// 预期 stable 4 个、canary 1 个。和 kind 上的 review-moderator 是同一组数字；要别的 weight 就创建后再 updateMD
func createCanaryModelDeployment(name string) *servingv1alpha1.ModelDeployment {
	md := createMinimumModelDeployment(name)
	md.Spec.Replicas = ptr.To[int32](5)
	md.Spec.Canary = &servingv1alpha1.CanarySpec{
		Image:    testImageV2,
		ModelURI: testModelURIV2,
		Weight:   20,
	}
	return md
}
