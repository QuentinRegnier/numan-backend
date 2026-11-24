package admin_models

import (
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// GetSchedulerJobsInput représente le corps JSON optionnel pour lister les jobs du Scheduler d'un datacenter.
type GetSchedulerJobsInput struct {
	Datacenter string `json:"datacenter,omitempty" binding:"omitempty,max=64"`
}

// Validate vérifie la validité du filtre datacenter.
func (p *GetSchedulerJobsInput) Validate() error {
	if p == nil {
		return nil
	}
	p.Datacenter = strings.TrimSpace(p.Datacenter)
	if p.Datacenter == "" {
		return nil
	}
	if !validDatacenterIDPattern.MatchString(p.Datacenter) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de datacenter contient des caractères invalides ou dépasse 64 caractères.", nil)
	}
	return nil
}

// GetSchedulerJobsOutput représente la vue synthétique des jobs planifiés retournée à l'Admin Panel.
type GetSchedulerJobsOutput = scheduler_models.SchedulerOverview
