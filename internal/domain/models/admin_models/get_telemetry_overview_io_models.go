package admin_models

import (
	"regexp"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/infra_telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

var validDatacenterIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)

// GetTelemetryOverviewInput représente le corps JSON optionnel pour consulter la télémétrie d'un datacenter.
type GetTelemetryOverviewInput struct {
	Datacenter string `json:"datacenter,omitempty" binding:"omitempty,max=64"`
}

// Validate vérifie les invariants métier du filtre datacenter.
func (p *GetTelemetryOverviewInput) Validate() error {
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

// GetTelemetryOverviewOutput représente la réponse complète de la télémétrie datacenter exposée à l'Admin Panel.
type GetTelemetryOverviewOutput = infra_telemetry_models.CLITelemetrySnapshot
