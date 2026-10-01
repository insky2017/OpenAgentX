package panel

import (
	"context"
	"errors"
	"net/http"
	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
)

type taskReviewReader interface {
	GetTaskReview(context.Context, string) (*domain.TaskReview, error)
}

func (h *Handler) reviewTask(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.session(w, r, true)
	if !ok {
		return
	}
	var req openapi.ReviewTaskRequest
	if err := openapi.DecodeStrictJSON(r.Body, &req); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if !requireIdempotencyHeader(w, r, req.Meta) {
		return
	}
	review, err := h.commands.ReviewTaskResult(r.Context(), principal.ID, r.PathValue("taskID"), req)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, domain.ErrStaleVersion) || errors.Is(err, domain.ErrIdempotencyConflict) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, review)
}
