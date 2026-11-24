package admin_models

import (
	"strings"
	"unicode/utf8"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// PauseSchedulerInput représente le corps JSON de mise en pause globale du Scheduler.
type PauseSchedulerInput struct {
	TTLMinutes int    `json:"ttl_minutes,omitempty" binding:"omitempty,gte=0,lte=1440"`
	Reason     string `json:"reason,omitempty" binding:"omitempty,max=256"`
}

// Validate vérifie les bornes métier de la requête de mise en pause du Scheduler.
func (p *PauseSchedulerInput) Validate() error {
	if p == nil {
		return nil
	}
	if p.TTLMinutes < 0 || p.TTLMinutes > 1440 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "ttl_minutes doit être compris entre 0 et 1440 (24h maximum).", nil)
	}
	p.Reason = strings.TrimSpace(p.Reason)
	if utf8.RuneCountInString(p.Reason) > 256 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "reason ne doit pas dépasser 256 caractères.", nil)
	}
	return nil
}
