package domain

import "strings"

type OrgUnit struct {
	ID             string `json:"org_unit_id"`
	OrganizationID string `json:"organization_id"`
	ParentID       string `json:"parent_id,omitempty"`
	Name           string `json:"name"`
}
type Position struct {
	ID             string `json:"position_id"`
	OrganizationID string `json:"organization_id"`
	OrgUnitID      string `json:"org_unit_id"`
	Title          string `json:"title"`
}
type Role struct {
	ID             string `json:"role_id"`
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
}
type PositionAssignment struct {
	PositionID  string `json:"position_id"`
	PrincipalID string `json:"principal_id"`
	Active      bool   `json:"active"`
}
type ReportingLine struct {
	OrganizationID    string `json:"organization_id"`
	ManagerPositionID string `json:"manager_position_id"`
	MemberPositionID  string `json:"member_position_id"`
}

type AuthorityAction string

const (
	AuthorityTaskCreate        AuthorityAction = "task.create"
	AuthorityMessageSend       AuthorityAction = "message.send"
	AuthorityApprovalDecide    AuthorityAction = "approval.decide"
	AuthorityTaskCancel        AuthorityAction = "task.cancel"
	AuthorityDirectDispatch    AuthorityAction = "dispatch.direct"
	AuthorityExecutionOverride AuthorityAction = "execution.override"
)

type AuthorityInput struct {
	PrincipalID    string
	OrganizationID string
	TargetAgentID  string
	DispatchMode   DispatchMode
	Action         AuthorityAction
}

// AuthorityPolicy is intentionally deterministic and side-effect free. The
// Command Service supplies the stable principal/action/resource/context tuple.
type AuthorityPolicy struct {
	OrganizationID              string
	DefaultPrincipalID          string
	PrincipalIDs                []string
	DirectDispatchPrincipals    []string
	ExecutionOverridePrincipals []string
}

func (p AuthorityPolicy) Authorize(input AuthorityInput) error {
	if input.PrincipalID == "" || input.OrganizationID == "" || input.Action == "" {
		return ErrInvalidInput("authority input is incomplete")
	}
	if p.OrganizationID != "" && input.OrganizationID != p.OrganizationID {
		return ErrForbidden("principal is outside organization")
	}
	if len(p.PrincipalIDs) > 0 && !containsPrincipal(p.PrincipalIDs, input.PrincipalID) {
		return ErrForbidden("principal is not assigned to organization")
	}
	if input.DispatchMode == DispatchModeDirect && !containsPrincipal(p.DirectDispatchPrincipals, input.PrincipalID) {
		return ErrForbidden("direct dispatch is not authorized")
	}
	if input.Action == AuthorityExecutionOverride && !containsPrincipal(p.ExecutionOverridePrincipals, input.PrincipalID) {
		return ErrForbidden("execution override is not authorized")
	}
	return nil
}

func containsPrincipal(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func ValidateBusinessTarget(targetAgentID string, workerInstanceIDs []string) error {
	if strings.TrimSpace(targetAgentID) == "" {
		return ErrInvalidInput("target agent is required")
	}
	for _, workerID := range workerInstanceIDs {
		if targetAgentID == workerID {
			return ErrInvalidInput("Worker Instance cannot be a business target")
		}
	}
	return nil
}
