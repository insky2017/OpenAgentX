package domain

import "time"

// TaskReview records a human's assessment of one completed result. It never
// changes the execution outcome or claims independent verification of effects.
type TaskReview struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	RunID        string    `json:"run_id"`
	RunVersion   int64     `json:"run_version"`
	ResultSHA256 string    `json:"result_sha256"`
	Decision     string    `json:"decision"`
	Note         string    `json:"note,omitempty"`
	ReviewedBy   string    `json:"reviewed_by"`
	CreatedAt    time.Time `json:"created_at"`
	Sequence     int64     `json:"sequence"`
	TaskVersion  int64     `json:"task_version"`
}
