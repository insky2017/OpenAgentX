package api

import "openagentx/internal/domain"

const (
	AdminWorkerDrainPath       = "/api/admin/v1/workers/{worker-id}/drain"
	AdminWorkerLeaseRevokePath = "/api/admin/v1/workers/{worker-id}/lease/revoke"
	AdminWorkerHealthPath      = "/api/admin/v1/workers/{worker-id}/health-check"
	AdminWorkerStopPath        = "/api/admin/v1/workers/{worker-id}/stop"
)

type WorkerAdminRequest struct {
	Meta               CommandMeta `json:"meta"`
	RequestedBy        string      `json:"requested_by"`
	ExpectedGeneration int64       `json:"expected_generation"`
}

func (r WorkerAdminRequest) Validate() error {
	if err := r.Meta.Validate(true); err != nil {
		return err
	}
	if err := domain.ValidateOpaqueID("requested_by", r.RequestedBy); err != nil {
		return err
	}
	return domain.ValidatePositiveVersion("expected_generation", r.ExpectedGeneration)
}

type WorkerCommandResponse struct {
	Command domain.WorkerCommand `json:"command"`
}

type LeaseRevokeResponse struct {
	WorkerInstanceID string `json:"worker_instance_id"`
	Generation       int64  `json:"generation"`
	FencingToken     int64  `json:"fencing_token"`
	Sequence         int64  `json:"sequence"`
}
