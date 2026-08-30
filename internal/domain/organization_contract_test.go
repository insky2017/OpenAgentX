package domain

import "testing"

func TestAuthorityPolicyRejectsCrossOrgAndUnauthorizedDirectDispatch(t *testing.T) {
	policy := AuthorityPolicy{OrganizationID: "org-main", PrincipalIDs: []string{"human-owner"}, DirectDispatchPrincipals: []string{"manager"}}
	if err := policy.Authorize(AuthorityInput{PrincipalID: "human-owner", OrganizationID: "org-other", TargetAgentID: "quote", DispatchMode: DispatchModeCoordinated, Action: AuthorityTaskCreate}); err == nil {
		t.Fatal("cross-organization command accepted")
	}
	if err := policy.Authorize(AuthorityInput{PrincipalID: "human-owner", OrganizationID: "org-main", TargetAgentID: "quote", DispatchMode: DispatchModeDirect, Action: AuthorityDirectDispatch}); err == nil {
		t.Fatal("unauthorized direct dispatch accepted")
	}
}

func TestWorkerInstanceCannotBeBusinessTarget(t *testing.T) {
	if err := ValidateBusinessTarget("worker-1", []string{"worker-1"}); err == nil {
		t.Fatal("Worker Instance accepted as business target")
	}
}
