package domain

import "testing"

func TestAssessTaskSuccessEvidenceMatrix(t *testing.T) {
	for bits := 0; bits < 32; bits++ {
		e := TaskCompletionEvidence{FinalReply: bits&1 != 0, HasResult: bits&2 != 0, Truncated: bits&4 != 0, HasError: bits&8 != 0, SideEffectsKnown: bits&16 != 0}
		for _, intent := range []TaskIntent{TaskIntentQuery, TaskIntentMutation} {
			status, basis, reason := AssessTaskSuccess(intent, e)
			success := intent == TaskIntentQuery && bits&15 == 3 || intent == TaskIntentMutation && bits&16 != 0
			if success {
				want := TaskCompletionQueryResultDelivered
				if intent == TaskIntentMutation {
					want = TaskCompletionMutationEffectsKnown
				}
				if status != TaskStatusSucceeded || basis != want || reason != "" {
					t.Fatalf("%s evidence=%+v: %s %s %s", intent, e, status, basis, reason)
				}
			} else {
				want := "query_result_unverified"
				if intent == TaskIntentMutation {
					want = "business_effect_unverified"
				}
				if status != TaskStatusUncertain || basis != "" || reason != want {
					t.Fatalf("%s evidence=%+v: %s %s %s", intent, e, status, basis, reason)
				}
			}
		}
	}
	for _, intent := range []TaskIntent{"", "unknown"} {
		status, basis, _ := AssessTaskSuccess(intent, TaskCompletionEvidence{FinalReply: true, HasResult: true, SideEffectsKnown: true})
		if status != TaskStatusUncertain || basis != "" {
			t.Fatal("invalid intent was accepted")
		}
	}
}
