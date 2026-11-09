package controller

import (
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	servingv1alpha1 "github.com/zhexizx/modelserve-operator/api/v1alpha1"
)

// newDep 构造一个只填了判断所需字段的 Deployment。
// generation 是 spec 的版本；observedGen 是 Deployment controller 处理到的版本。
func newDep(generation, observedGen int64, replicas, updated, available int32) *appsv1.Deployment {
	dep := &appsv1.Deployment{}
	dep.Generation = generation
	dep.Status.ObservedGeneration = observedGen
	dep.Status.Replicas = replicas
	dep.Status.UpdatedReplicas = updated
	dep.Status.AvailableReplicas = available
	dep.Status.ReadyReplicas = available // 没设 minReadySeconds 时两者相等
	return dep
}

func TestDeploymentReady(t *testing.T) {
	tests := []struct {
		name string
		dep  *appsv1.Deployment
		want int32
		exp  bool
	}{
		{
			// 改了 image：旧 Pod 4 个都 ready，新 Pod 还没起来
			name: "rolling out, old pods still ready",
			dep:  newDep(2, 2, 4, 0, 4),
			want: 4,
			exp:  false,
		},
		{
			name: "complete",
			dep:  newDep(2, 2, 4, 4, 4),
			want: 4,
			exp:  true,
		},
		{
			// spec 刚改完，Deployment controller 还没处理，status 里是上一版的数据
			name: "controller has not seen new spec",
			dep:  newDep(3, 2, 4, 4, 4),
			want: 4,
			exp:  false,
		},
		{
			// 新 Pod 4 个都起来了，旧 Pod 还有 1 个没进入删除流程
			name: "old pods not yet removed",
			dep:  newDep(2, 2, 5, 4, 5),
			want: 4,
			exp:  false,
		},
		{
			name: "new pods not yet available",
			dep:  newDep(2, 2, 4, 4, 3),
			want: 4,
			exp:  false,
		},
		{
			// 改了 weight 导致副本数变化：Deployment controller 还没把 Pod 加够
			name: "scaling up",
			dep:  newDep(3, 3, 1, 1, 1),
			want: 2,
			exp:  false,
		},
		{
			// weight=0 的 canary，或 weight=100 时的 stable：不需要 Pod，也确实没有 Pod
			name: "want 0, all pods gone",
			dep:  newDep(2, 2, 0, 0, 0),
			want: 0,
			exp:  true,
		},
		{
			// 刚把副本数改成 0，旧 Pod 还没删完
			name: "want 0, old pods still running",
			dep:  newDep(2, 2, 2, 0, 2),
			want: 0,
			exp:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deploymentReady(tt.dep, tt.want); got != tt.exp {
				t.Errorf("deploymentReady() = %v, want %v", got, tt.exp)
			}
		})
	}
}

// wantCond 是对一个 condition 的期望：Status 和 Reason。
type wantCond struct {
	status metav1.ConditionStatus
	reason string
}

// checkCond 断言 md 上某个 Type 的 condition 存在，并且 Status、Reason 符合期望。
func checkCond(t *testing.T, md *servingv1alpha1.ModelDeployment, condType string, want wantCond) {
	t.Helper() // 出错时报告调用方的行号，而不是这里的行号
	c := meta.FindStatusCondition(md.Status.Conditions, condType)
	if c == nil {
		t.Errorf("condition %s not found, conditions = %+v", condType, md.Status.Conditions)
		return
	}
	if c.Status != want.status || c.Reason != want.reason {
		t.Errorf("%s = %s/%s, want %s/%s", condType, c.Status, c.Reason, want.status, want.reason)
	}
	if c.ObservedGeneration != md.Generation {
		t.Errorf("%s observedGeneration = %d, want %d", condType, c.ObservedGeneration, md.Generation)
	}
}

func TestSetStatusConditions(t *testing.T) {
	tests := []struct {
		name        string
		obs         observation
		err         error
		degraded    wantCond
		ready       wantCond
		progressing wantCond
	}{
		{
			name:        "all done",
			obs:         observation{stableDone: true, canaryDone: true},
			degraded:    wantCond{metav1.ConditionFalse, servingv1alpha1.ReasonReconcileSuccess},
			ready:       wantCond{metav1.ConditionTrue, servingv1alpha1.ReasonAllReplicasReady},
			progressing: wantCond{metav1.ConditionFalse, servingv1alpha1.ReasonRolloutComplete},
		},
		{
			name:        "canary still rolling out",
			obs:         observation{stableDone: true, canaryDone: false},
			degraded:    wantCond{metav1.ConditionFalse, servingv1alpha1.ReasonReconcileSuccess},
			ready:       wantCond{metav1.ConditionFalse, servingv1alpha1.ReasonReplicasNotReady},
			progressing: wantCond{metav1.ConditionTrue, servingv1alpha1.ReasonRollingOut},
		},
		{
			name:        "stable still rolling out",
			obs:         observation{stableDone: false, canaryDone: true},
			degraded:    wantCond{metav1.ConditionFalse, servingv1alpha1.ReasonReconcileSuccess},
			ready:       wantCond{metav1.ConditionFalse, servingv1alpha1.ReasonReplicasNotReady},
			progressing: wantCond{metav1.ConditionTrue, servingv1alpha1.ReasonRollingOut},
		},
		{
			// 出错时 obs 不可信：就算两边都显示完成，也不能说 Ready
			name:        "reconcile error overrides done",
			obs:         observation{stableDone: true, canaryDone: true},
			err:         errors.New("boom"),
			degraded:    wantCond{metav1.ConditionTrue, servingv1alpha1.ReasonReconcileError},
			ready:       wantCond{metav1.ConditionFalse, servingv1alpha1.ReasonReconcileError},
			progressing: wantCond{metav1.ConditionFalse, servingv1alpha1.ReasonReconcileError},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md := &servingv1alpha1.ModelDeployment{}
			md.Generation = 7

			setStatusConditions(md, tt.obs, tt.err)

			if len(md.Status.Conditions) != 3 {
				t.Errorf("got %d conditions, want 3: %+v", len(md.Status.Conditions), md.Status.Conditions)
			}
			checkCond(t, md, servingv1alpha1.ConditionDegraded, tt.degraded)
			checkCond(t, md, servingv1alpha1.ConditionReady, tt.ready)
			checkCond(t, md, servingv1alpha1.ConditionProgressing, tt.progressing)
		})
	}
}

// 出错时，错误信息要写进 Degraded 的 Message，用户 kubectl describe 就能看到
func TestSetStatusConditionsErrorMessage(t *testing.T) {
	md := &servingv1alpha1.ModelDeployment{}

	setStatusConditions(md, observation{}, errors.New("deployments.apps is forbidden"))

	c := meta.FindStatusCondition(md.Status.Conditions, servingv1alpha1.ConditionDegraded)
	if c == nil || c.Message != "deployments.apps is forbidden" {
		t.Errorf("Degraded = %+v, want message %q", c, "deployments.apps is forbidden")
	}
}

// 对同一个对象连续调用：状态要能变回去，不能残留上一次的值（"灯不会自己熄灭"）
func TestSetStatusConditionsTransition(t *testing.T) {
	md := &servingv1alpha1.ModelDeployment{}

	// 第 1 次：完成
	setStatusConditions(md, observation{stableDone: true, canaryDone: true}, nil)
	checkCond(t, md, servingv1alpha1.ConditionReady, wantCond{metav1.ConditionTrue, servingv1alpha1.ReasonAllReplicasReady})

	// 第 2 次：用户改了 image，又开始升级
	md.Generation = 2
	setStatusConditions(md, observation{stableDone: false, canaryDone: true}, nil)
	checkCond(t, md, servingv1alpha1.ConditionReady, wantCond{metav1.ConditionFalse, servingv1alpha1.ReasonReplicasNotReady})
	checkCond(t, md, servingv1alpha1.ConditionProgressing, wantCond{metav1.ConditionTrue, servingv1alpha1.ReasonRollingOut})

	// 第 3 次：出错后又恢复了，Degraded 要变回 False，Message 要清空
	setStatusConditions(md, observation{}, errors.New("boom"))
	setStatusConditions(md, observation{stableDone: true, canaryDone: true}, nil)
	checkCond(t, md, servingv1alpha1.ConditionDegraded, wantCond{metav1.ConditionFalse, servingv1alpha1.ReasonReconcileSuccess})
	if c := meta.FindStatusCondition(md.Status.Conditions, servingv1alpha1.ConditionDegraded); c.Message != "" {
		t.Errorf("Degraded message = %q, want empty after recovery", c.Message)
	}

	if len(md.Status.Conditions) != 3 {
		t.Errorf("got %d conditions after several calls, want 3 (no duplicates)", len(md.Status.Conditions))
	}
}
