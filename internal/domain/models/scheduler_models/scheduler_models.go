package scheduler_models

import (
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// Regime représente l'état de charge instantané du datacenter pour l'Activity-Aware Job Scheduler.
type Regime string

const (
	RegimeOverload Regime = "OVERLOAD"
	RegimePeak     Regime = "PEAK"
	RegimeModerate Regime = "MODERATE"
	RegimeIdle     Regime = "IDLE"
	RegimeDeepIdle Regime = "DEEP_IDLE"
)

// Priority retourne l'ordre de sévérité (plus élevé = plus contraint).
func (r Regime) Priority() int {
	switch r {
	case RegimeOverload:
		return 5
	case RegimePeak:
		return 4
	case RegimeModerate:
		return 3
	case RegimeIdle:
		return 2
	case RegimeDeepIdle:
		return 1
	default:
		return 4
	}
}

// CPUBudgetPercent retourne le budget CPU autorisé pour les tâches de fond selon le régime actif.
func (r Regime) CPUBudgetPercent() int {
	switch r {
	case RegimeDeepIdle:
		return 95
	case RegimeIdle:
		return 75
	case RegimeModerate:
		return 30
	case RegimePeak:
		return 0
	case RegimeOverload:
		return 0
	default:
		return 0
	}
}

// Allows indique si le régime courant autorise un job exigeant au minimum minRegime.
func (r Regime) Allows(minRegime Regime) bool {
	return r.Priority() <= minRegime.Priority()
}

// IsValid vérifie si le régime correspond à l'une des 5 valeurs canoniques.
func (r Regime) IsValid() bool {
	switch r {
	case RegimeOverload, RegimePeak, RegimeModerate, RegimeIdle, RegimeDeepIdle:
		return true
	default:
		return false
	}
}

// LockScope définit la portée du verrou distribué pour un job planifié.
type LockScope string

const (
	// LockScopeLocalDC : verrou Redis Core local au datacenter (`lock:scheduler:<job>`).
	LockScopeLocalDC LockScope = "local_dc"

	// LockScopeGlobalSQL : verrou consultatif PostgreSQL global (1 seul datacenter au monde).
	LockScopeGlobalSQL LockScope = "global_sql"
)

// IsValid vérifie que la portée de verrou est reconnue.
func (s LockScope) IsValid() bool {
	return s == LockScopeLocalDC || s == LockScopeGlobalSQL
}

// RegimeConfig regroupe les seuils de classification et d'hystérésis du Scheduler.
type RegimeConfig struct {
	DeepIdleThreshold float64
	IdleThreshold     float64
	ModerateThreshold float64
	OverloadThreshold float64

	OverloadP95Ms          float64
	OverloadQueueAgeSec    float64
	OverloadPGWritePoolPct float64

	DownshiftHysteresis time.Duration
}

// Validate vérifie la cohérence des seuils du Scheduler.
func (c *RegimeConfig) Validate() error {
	if c == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La configuration de régime du scheduler est vide.", nil)
	}
	if c.DeepIdleThreshold < 0 || c.DeepIdleThreshold > 1 ||
		c.IdleThreshold < 0 || c.IdleThreshold > 1 ||
		c.ModerateThreshold < 0 || c.ModerateThreshold > 1 ||
		c.OverloadThreshold < 0 || c.OverloadThreshold > 1 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les seuils de régime doivent être compris entre 0 et 1.", nil)
	}
	if c.DeepIdleThreshold > c.IdleThreshold || c.IdleThreshold > c.ModerateThreshold || c.ModerateThreshold > c.OverloadThreshold {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les seuils de régime doivent être strictement croissants (DeepIdle <= Idle <= Moderate <= Overload).", nil)
	}
	return nil
}

// ActivitySample résume la charge instantanée agrégée du datacenter.
type ActivitySample struct {
	ActivityScore          float64 `json:"activity_score"`
	SaturationScore        float64 `json:"saturation_score"`
	HTTPP95LatencyMs       float64 `json:"http_p95_latency_ms"`
	QueueOldestEventAgeSec float64 `json:"queue_oldest_event_age_sec"`
	PostgresWritePoolUtil  float64 `json:"postgres_write_pool_util"`
}

// Validate vérifie que l'échantillon d'activité contient des valeurs cohérentes.
func (s *ActivitySample) Validate() error {
	if s == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'échantillon d'activité est vide.", nil)
	}
	if s.ActivityScore < 0 || s.SaturationScore < 0 || s.HTTPP95LatencyMs < 0 || s.QueueOldestEventAgeSec < 0 || s.PostgresWritePoolUtil < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les métriques d'activité ne peuvent pas être négatives.", nil)
	}
	return nil
}

// JobCheckpoint capture l'avancement persistant d'un job afin de reprendre exactement au curseur après préemption (<200ms).
type JobCheckpoint struct {
	LastID      int64             `json:"last_id,omitempty"`
	ScanCursor  uint64            `json:"scan_cursor,omitempty"`
	ItemsDone   int64             `json:"items_done"`
	ItemsTotal  int64             `json:"items_total"`
	Extra       map[string]string `json:"extra,omitempty"`
	UpdatedAtMs int64             `json:"updated_at_ms"`
}

// Validate vérifie l'intégrité du checkpoint de job.
func (cp *JobCheckpoint) Validate() error {
	if cp == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le checkpoint de job est vide.", nil)
	}
	if cp.ItemsDone < 0 || cp.ItemsTotal < 0 {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les compteurs du checkpoint ne peuvent pas être négatifs.", nil)
	}
	return nil
}

// JobStatus résume l'état temps réel & historique d'un job (persisté dans Redis Core `scheduler:job:<name>`).
type JobStatus struct {
	Name              string        `json:"name"`
	Category          string        `json:"category"`
	Description       string        `json:"description"`
	MinRegime         Regime        `json:"min_regime"`
	LockScope         LockScope     `json:"lock_scope"`
	Status            string        `json:"status"`
	LastRunAtMs       int64         `json:"last_run_at_ms"`
	LastCompletedAtMs int64         `json:"last_completed_at_ms"`
	LastDurationMs    int64         `json:"last_duration_ms"`
	LastError         string        `json:"last_error,omitempty"`
	PreemptedCount    int64         `json:"preempted_count"`
	Checkpoint        JobCheckpoint `json:"checkpoint"`
	ProgressPct       float64       `json:"progress_pct"`
	StarvationWarning bool          `json:"starvation_warning"`
	TrickleMode       bool          `json:"trickle_mode"`
	NextEligibleAtMs  int64         `json:"next_eligible_at_ms"`
}

// Validate vérifie l'intégrité du statut d'un job.
func (s *JobStatus) Validate() error {
	if s == nil || strings.TrimSpace(s.Name) == "" {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le nom du job est requis.", nil)
	}
	if !s.MinRegime.IsValid() {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le régime minimal du job est invalide.", nil)
	}
	if !s.LockScope.IsValid() {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La portée de verrou du job est invalide.", nil)
	}
	return s.Checkpoint.Validate()
}

// SchedulerOverview est le snapshot unifié consommé par l'Admin Panel, `:9090/internal/cli-telemetry` et Prometheus.
type SchedulerOverview struct {
	Datacenter         string      `json:"datacenter"`
	Regime             Regime      `json:"regime"`
	Paused             bool        `json:"paused"`
	PauseReason        string      `json:"pause_reason,omitempty"`
	Overload           bool        `json:"overload"`
	ActiveJob          string      `json:"active_job,omitempty"`
	ProgressPct        float64     `json:"progress_pct"`
	CPUBudgetPct       int         `json:"cpu_budget_pct"`
	Starvation48hAlert bool        `json:"starvation_48h_warning"`
	LastDeepIdleAtMs   int64       `json:"last_deep_idle_at_ms"`
	Jobs               []JobStatus `json:"jobs"`
}

// Validate vérifie l'intégrité de la vue synthétique du scheduler.
func (o *SchedulerOverview) Validate() error {
	if o == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le snapshot du scheduler est vide.", nil)
	}
	if !o.Regime.IsValid() {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le régime du scheduler est invalide.", nil)
	}
	return nil
}

// PausePayload représente l'état de pause globale stocké dans Redis Core.
type PausePayload struct {
	PausedAtMs int64  `json:"paused_at_ms"`
	Reason     string `json:"reason"`
}

// RemoteTriggerPayload représente une commande de déclenchement manuel inter-pods.
type RemoteTriggerPayload struct {
	JobName       string `json:"job_name"`
	Force         bool   `json:"force"`
	RequestedAtMs int64  `json:"requested_at_ms"`
}
