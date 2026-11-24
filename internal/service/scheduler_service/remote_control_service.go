package scheduler_service

import (
	"context"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
)

// PauseGlobal met le Scheduler en pause (localement si ce pod exécute le Scheduler, et globalement via Redis Core).
func PauseGlobal(ctx context.Context, ttl time.Duration, reason string) error {
	if ttl <= 0 {
		ttl = defaultPauseTTL
	}
	if strings.TrimSpace(reason) == "" {
		reason = "maintenance_pause"
	}
	if eng := Default(); eng != nil {
		return eng.Pause(ctx, ttl, reason)
	}
	return cache_service.SetSchedulerGlobalPause(ctx, scheduler_models.PausePayload{
		PausedAtMs: domain.NowMillis(),
		Reason:     reason,
	}, ttl)
}

// ResumeGlobal lève la pause globale du Scheduler (localement + Redis Core).
func ResumeGlobal(ctx context.Context) error {
	if eng := Default(); eng != nil {
		return eng.Resume(ctx)
	}
	return cache_service.ClearSchedulerGlobalPause(ctx)
}

// GetJobsOverview retourne le snapshot des jobs planifiés (instance locale sur monolith/scheduler_node, sinon Redis Core).
func GetJobsOverview(ctx context.Context, datacenter string) scheduler_models.SchedulerOverview {
	if eng := Default(); eng != nil {
		return eng.StatusSnapshot(ctx)
	}
	if datacenter == "" {
		datacenter = "default"
	}
	ov := scheduler_models.SchedulerOverview{
		Datacenter: datacenter,
		Regime:     scheduler_models.RegimeModerate,
		Jobs:       []scheduler_models.JobStatus{},
	}
	if paused, reason, found := cache_service.GetSchedulerGlobalPause(ctx); found && paused {
		ov.Paused = true
		ov.PauseReason = reason
	}
	if cache_service.IsSchedulerOverloadFlagSet(ctx) {
		ov.Overload = true
		ov.Regime = scheduler_models.RegimeOverload
	}
	ov.CPUBudgetPct = ov.Regime.CPUBudgetPercent()

	jobs, err := cache_service.ListAllSchedulerJobStatuses(ctx)
	if err != nil {
		return ov
	}
	for _, st := range jobs {
		if st.StarvationWarning {
			ov.Starvation48hAlert = true
		}
		if st.Status == "running" {
			ov.ActiveJob = st.Name
			ov.ProgressPct = st.ProgressPct
		}
	}
	ov.Jobs = jobs
	return ov
}

// TriggerJobGlobal déclenche un job localement si le Scheduler tourne sur ce pod,
// ou dépose une commande de déclenchement distant dans Redis Core pour le pod scheduler_node.
func TriggerJobGlobal(ctx context.Context, name string, force bool) (*scheduler_models.JobStatus, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom du job est requis.", nil)
	}
	if eng := Default(); eng != nil {
		st, err := eng.TriggerJob(ctx, name, force)
		return st, false, err
	}

	if !force {
		if paused, _, found := cache_service.GetSchedulerGlobalPause(ctx); found && paused {
			return nil, false, numan_error.NewConflict(numan_error.CodeConflict, "Le scheduler est en pause (utilisez force=true pour forcer).", nil)
		}
		if cache_service.IsSchedulerOverloadFlagSet(ctx) {
			return nil, false, numan_error.NewConflict(numan_error.CodeConflict, "Déclenchement refusé en régime OVERLOAD (utilisez force=true pour forcer).", nil)
		}
	}

	err := cache_service.EnqueueSchedulerRemoteTrigger(ctx, scheduler_models.RemoteTriggerPayload{
		JobName:       name,
		Force:         force,
		RequestedAtMs: domain.NowMillis(),
	}, 2*time.Minute)
	if err != nil {
		return nil, false, err
	}
	return &scheduler_models.JobStatus{
		Name:   name,
		Status: "queued_remote_trigger",
	}, true, nil
}

func (e *Engine) consumeRemoteTrigger(ctx context.Context) bool {
	e.mu.RLock()
	if e.active != nil {
		e.mu.RUnlock()
		return false
	}
	names := append([]string(nil), e.order...)
	e.mu.RUnlock()

	for _, name := range names {
		req, ok := cache_service.PopSchedulerRemoteTrigger(ctx, name)
		if !ok || req == nil {
			continue
		}
		if _, errTrig := e.TriggerJob(ctx, name, req.Force); errTrig == nil {
			return true
		} else {
			numan_log.Warn(ctx).Err(errTrig).Str("job", name).Msg("Déclenchement distant du job ignoré")
		}
	}
	return false
}
