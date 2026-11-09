package v1alpha1

// Condition 类型。约定：
//   - Ready 和 Progressing 互斥：所有 track 都升级完成时 Ready=True、Progressing=False，否则反过来。
//     （注意和原生 Deployment 不同：它的 Progressing 在升级完成后仍然保持 True）
//   - Degraded 独立于前两者：reconcile 出错时为 True，Message 里是错误信息。
const (
	ConditionReady       = "Ready"
	ConditionProgressing = "Progressing"
	ConditionDegraded    = "Degraded"
)

// Condition 的 Reason。必须是 CamelCase，不能有空格（metav1.Condition 的校验规则）。
const (
	ReasonAllReplicasReady = "AllReplicasReady"
	ReasonReplicasNotReady = "ReplicasNotReady"
	ReasonRollingOut       = "RollingOut"
	ReasonRolloutComplete  = "RolloutComplete"
	ReasonReconcileError   = "ReconcileError"
	ReasonReconcileSuccess = "ReconcileSuccess"
)
