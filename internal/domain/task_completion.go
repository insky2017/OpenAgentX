package domain

// TaskCompletionBasis records which acceptance contract was met. Runtime
// knowledge of mutation effects does not imply independent business verification.
type TaskCompletionBasis string

const (
	TaskCompletionQueryResultDelivered TaskCompletionBasis = "query_result_delivered"
	TaskCompletionMutationEffectsKnown TaskCompletionBasis = "mutation_effects_known"
)

func (b TaskCompletionBasis) Valid() bool {
	return b == "" || b == TaskCompletionQueryResultDelivered || b == TaskCompletionMutationEffectsKnown
}

type TaskCompletionEvidence struct {
	FinalReply       bool
	HasResult        bool
	Truncated        bool
	HasError         bool
	SideEffectsKnown bool
}

// AssessTaskSuccess is called only for a Runtime succeeded result. Control
// races are resolved by the caller before persisting a successful basis.
func AssessTaskSuccess(intent TaskIntent, evidence TaskCompletionEvidence) (TaskStatus, TaskCompletionBasis, string) {
	switch intent {
	case TaskIntentQuery:
		if evidence.FinalReply && evidence.HasResult && !evidence.Truncated && !evidence.HasError {
			return TaskStatusSucceeded, TaskCompletionQueryResultDelivered, ""
		}
		return TaskStatusUncertain, "", "query_result_unverified"
	case TaskIntentMutation:
		if evidence.SideEffectsKnown {
			return TaskStatusSucceeded, TaskCompletionMutationEffectsKnown, ""
		}
		return TaskStatusUncertain, "", "business_effect_unverified"
	default:
		return TaskStatusUncertain, "", "task_intent_invalid"
	}
}
