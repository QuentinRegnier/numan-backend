package admin_models

import (
	"regexp"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

var validSchedulerJobNamePattern = regexp.MustCompile(`^[a-z0-9_]{2,128}$`)

// TriggerSchedulerJobInput représente le corps JSON de déclenchement manuel d'un job planifié.
type TriggerSchedulerJobInput struct {
	JobName string `json:"job_name" binding:"required,min=2,max=128"`
	Force   bool   `json:"force,omitempty"`
}

// Validate vérifie les invariants métier de la requête de déclenchement manuel d'un job.
func (p *TriggerSchedulerJobInput) Validate() error {
	if p == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La requête de déclenchement de job est vide.", nil)
	}
	p.JobName = strings.TrimSpace(p.JobName)
	if p.JobName == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom du job (job_name) est requis.", nil)
	}
	if !validSchedulerJobNamePattern.MatchString(p.JobName) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom du job (job_name) contient des caractères invalides.", nil)
	}
	return nil
}

// SchedulerActionOutput résume le résultat d'une action administrative sur le Scheduler (trigger / pause / resume).
type SchedulerActionOutput struct {
	Status     string                      `json:"status"`
	Job        *scheduler_models.JobStatus `json:"job,omitempty"`
	Queued     bool                        `json:"queued,omitempty"`
	TTLMinutes int                         `json:"ttl_minutes,omitempty"`
	Reason     string                      `json:"reason,omitempty"`
}
