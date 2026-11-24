package scheduler_service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/infra_telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
)

const (
	defaultMaxStaleness = 48 * time.Hour
	defaultPauseTTL     = 30 * time.Minute
	defaultTickInterval = 5 * time.Second
)

// JobFunc exécute une passe (complète ou incrémentale) d'un job planifié.
//   - trickle == true indique le mode anti-famine (48h SLA) : petits lots + pauses pour protéger l'UX.
//   - Retourne (nextCheckpoint, completed, err). Si ctx est annulé (préemption), retourner (partialCP, false, ctx.Err()).
type JobFunc func(ctx context.Context, cp scheduler_models.JobCheckpoint, trickle bool) (nextCP scheduler_models.JobCheckpoint, completed bool, err error)

// JobDefinition déclare les métadonnées, contraintes de régime et logique d'un job planifié.
type JobDefinition struct {
	Name            string
	Category        string // "hygiene" | "database" | "algo_cache" | "migrated_cron"
	Description     string
	MinRegime       scheduler_models.Regime
	LockScope       scheduler_models.LockScope
	MinInterval     time.Duration
	MaxStaleness    time.Duration
	LockTTL         time.Duration
	WindowStartHour int // -1 si pas de fenêtre horaire restreinte, sinon 0..23 heure locale DC
	WindowEndHour   int // exclusif (ex: 4..6 => 04:00..05:59)
	Essential       bool
	Fn              JobFunc
}

type runningJobState struct {
	name      string
	minRegime scheduler_models.Regime
	trickle   bool
	cancel    context.CancelFunc
	done      chan struct{}
}

// Engine orchestre les jobs planifiés sensibles à l'activité du datacenter.
type Engine struct {
	datacenter string
	location   *time.Location
	tracker    *RegimeTracker
	nowFn      func() time.Time
	tickEvery  time.Duration

	mu       sync.RWMutex
	jobs     map[string]JobDefinition
	order    []string
	statuses map[string]*scheduler_models.JobStatus
	active   *runningJobState
	paused   bool
	pauseMsg string

	runMu   sync.Mutex
	stopCtx context.Context
	stopFn  context.CancelFunc
	loopWG  sync.WaitGroup
}

var (
	defaultEngineMu sync.RWMutex
	defaultEngine   *Engine
)

// Default retourne l'instance globale du Scheduler (peut être nil sur api_node/ws_node/worker_node).
func Default() *Engine {
	defaultEngineMu.RLock()
	defer defaultEngineMu.RUnlock()
	return defaultEngine
}

// SetDefault enregistre l'instance globale du Scheduler.
func SetDefault(e *Engine) {
	defaultEngineMu.Lock()
	defaultEngine = e
	defaultEngineMu.Unlock()
}

// NewEngine construit un moteur Scheduler prêt à enregistrer des jobs.
func NewEngine(datacenter string, cfg scheduler_models.RegimeConfig, nowFn func() time.Time) *Engine {
	if nowFn == nil {
		nowFn = time.Now
	}
	if datacenter == "" {
		datacenter = "default"
	}
	return &Engine{
		datacenter: datacenter,
		location:   time.Local,
		tracker:    NewRegimeTracker(cfg, nowFn),
		nowFn:      nowFn,
		tickEvery:  defaultTickInterval,
		jobs:       make(map[string]JobDefinition),
		statuses:   make(map[string]*scheduler_models.JobStatus),
	}
}

// SetLocation configure le fuseau horaire local du datacenter (utilisé pour les fenêtres horaires ex: 04:00-06:00).
func (e *Engine) SetLocation(loc *time.Location) {
	if loc == nil {
		return
	}
	e.mu.Lock()
	e.location = loc
	e.mu.Unlock()
}

// SetTickInterval ajuste la période d'évaluation interne (utile pour les tests unitaires).
func (e *Engine) SetTickInterval(d time.Duration) {
	if d > 0 {
		e.tickEvery = d
	}
}

// Tracker expose le RegimeTracker interne.
func (e *Engine) Tracker() *RegimeTracker {
	return e.tracker
}

// Register enregistre une définition de job dans le Scheduler.
func (e *Engine) Register(def JobDefinition) {
	if def.Name == "" || def.Fn == nil {
		return
	}
	if def.MaxStaleness <= 0 {
		def.MaxStaleness = defaultMaxStaleness
	}
	if def.MinInterval <= 0 {
		def.MinInterval = 24 * time.Hour
	}
	if def.LockTTL <= 0 {
		def.LockTTL = 10 * time.Minute
	}
	if def.LockScope == "" {
		def.LockScope = scheduler_models.LockScopeLocalDC
	}
	if def.MinRegime == "" {
		def.MinRegime = scheduler_models.RegimeDeepIdle
	}
	if def.WindowStartHour == 0 && def.WindowEndHour == 0 {
		def.WindowStartHour = -1
		def.WindowEndHour = -1
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.jobs[def.Name]; !exists {
		e.order = append(e.order, def.Name)
	}
	e.jobs[def.Name] = def
	if _, exists := e.statuses[def.Name]; !exists {
		e.statuses[def.Name] = &scheduler_models.JobStatus{
			Name:        def.Name,
			Category:    def.Category,
			Description: def.Description,
			MinRegime:   def.MinRegime,
			LockScope:   def.LockScope,
			Status:      "idle",
		}
	}
}

// Start lance la boucle périodique du Scheduler en arrière-plan.
func (e *Engine) Start(parentCtx context.Context) {
	e.runMu.Lock()
	defer e.runMu.Unlock()
	if e.stopFn != nil {
		return
	}
	e.stopCtx, e.stopFn = context.WithCancel(parentCtx)
	e.loadCheckpointsFromCache(e.stopCtx)
	e.loopWG.Add(1)
	go func() {
		defer e.loopWG.Done()
		ticker := time.NewTicker(e.tickEvery)
		defer ticker.Stop()
		for {
			select {
			case <-e.stopCtx.Done():
				e.preemptRunning()
				return
			case <-ticker.C:
				e.Tick(e.stopCtx)
			}
		}
	}()
}

// Stop arrête proprement le Scheduler et attend la sauvegarde des checkpoints en cours (timeout borné).
func (e *Engine) Stop(timeout time.Duration) {
	e.runMu.Lock()
	stopFn := e.stopFn
	e.stopFn = nil
	e.runMu.Unlock()

	if stopFn != nil {
		stopFn()
	}
	e.preemptRunning()

	done := make(chan struct{})
	go func() {
		e.loopWG.Wait()
		close(done)
	}()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// EvaluateSample met à jour le régime actif et déclenche immédiatement la préemption si le trafic monte (PEAK/OVERLOAD).
func (e *Engine) EvaluateSample(ctx context.Context, sample scheduler_models.ActivitySample) scheduler_models.Regime {
	reg, changed := e.tracker.Update(sample)

	if reg == scheduler_models.RegimeOverload {
		_ = cache_service.SetSchedulerOverloadFlag(ctx, true, 2*time.Minute)
	} else if changed {
		_ = cache_service.SetSchedulerOverloadFlag(ctx, false, 0)
	}

	e.mu.RLock()
	active := e.active
	e.mu.RUnlock()

	if active != nil {
		shouldPreempt := reg == scheduler_models.RegimeOverload || (!active.trickle && !reg.Allows(active.minRegime))
		if shouldPreempt {
			numan_log.Warn(ctx).
				Str("job", active.name).
				Str("regime", string(reg)).
				Msg("Préemption coopérative déclenchée suite à une hausse d'activité DC")
			active.cancel()
		}
	}

	return reg
}

// Pause met le Scheduler en pause (avec TTL auto-expirant de sécurité) et préempte immédiatement tout job actif.
func (e *Engine) Pause(ctx context.Context, ttl time.Duration, reason string) error {
	if ttl <= 0 {
		ttl = defaultPauseTTL
	}
	if reason == "" {
		reason = "maintenance_pause"
	}
	err := cache_service.SetSchedulerGlobalPause(ctx, scheduler_models.PausePayload{
		PausedAtMs: domain.TimeToMillis(e.nowFn()),
		Reason:     reason,
	}, ttl)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.paused = true
	e.pauseMsg = reason
	e.mu.Unlock()

	e.preemptRunning()
	return nil
}

// Resume lève la pause globale du Scheduler.
func (e *Engine) Resume(ctx context.Context) error {
	if err := cache_service.ClearSchedulerGlobalPause(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	e.paused = false
	e.pauseMsg = ""
	e.mu.Unlock()
	return nil
}

// IsPaused vérifie l'état de pause (en synchronisant avec Redis Core pour les contrôles inter-pods).
func (e *Engine) IsPaused(ctx context.Context) (bool, string) {
	if paused, reason, authoritative := cache_service.GetSchedulerGlobalPause(ctx); authoritative {
		e.mu.Lock()
		e.paused = paused
		e.pauseMsg = reason
		e.mu.Unlock()
		return paused, reason
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.paused, e.pauseMsg
}

func (e *Engine) preemptRunning() {
	e.mu.RLock()
	active := e.active
	e.mu.RUnlock()
	if active == nil {
		return
	}
	active.cancel()
	select {
	case <-active.done:
	case <-time.After(200 * time.Millisecond):
	}
}

// Tick évalue les jobs éligibles dans le régime courant et lance au plus un job lourd à la fois (séquentiel prédictible).
func (e *Engine) Tick(ctx context.Context) bool {
	if e.consumeRemoteTrigger(ctx) {
		return true
	}
	if paused, _ := e.IsPaused(ctx); paused {
		e.preemptRunning()
		return false
	}

	reg := e.tracker.Current()
	if reg == scheduler_models.RegimeOverload {
		e.preemptRunning()
		return false
	}

	e.mu.RLock()
	if e.active != nil {
		e.mu.RUnlock()
		return false
	}
	now := e.nowFn()
	nowMs := domain.TimeToMillis(now)
	localHour := now.In(e.location).Hour()

	var selectedDef *JobDefinition
	selectedTrickle := false

	for _, name := range e.order {
		def := e.jobs[name]
		st := e.statuses[name]

		lastRefMs := st.LastCompletedAtMs
		if lastRefMs == 0 {
			lastRefMs = domain.TimeToMillis(e.tracker.LastDeepIdleAt())
		}
		staleDuration := time.Duration(nowMs-lastRefMs) * time.Millisecond
		isStale := staleDuration >= def.MaxStaleness

		if def.Essential && isStale {
			st.StarvationWarning = true
		} else if !isStale {
			st.StarvationWarning = false
		}

		if st.Status != "preempted" && st.LastCompletedAtMs > 0 {
			elapsed := time.Duration(nowMs-st.LastCompletedAtMs) * time.Millisecond
			if elapsed < def.MinInterval {
				continue
			}
		}

		if def.WindowStartHour >= 0 && def.WindowEndHour >= 0 {
			if !inHourWindow(localHour, def.WindowStartHour, def.WindowEndHour) {
				continue
			}
		}

		if reg != scheduler_models.RegimePeak && reg != scheduler_models.RegimeOverload && reg.Allows(def.MinRegime) {
			dCopy := def
			selectedDef = &dCopy
			selectedTrickle = false
			break
		}
		if def.Essential && isStale && reg != scheduler_models.RegimeOverload {
			dCopy := def
			selectedDef = &dCopy
			selectedTrickle = true
			break
		}
	}
	e.mu.RUnlock()

	if selectedDef == nil {
		return false
	}
	e.executeJob(ctx, *selectedDef, selectedTrickle)
	return true
}

// TriggerJob déclenche manuellement un job spécifique (ex: Admin Panel / CLI).
// Refuse l'exécution pendant OVERLOAD ou pause sauf si force == true.
func (e *Engine) TriggerJob(ctx context.Context, name string, force bool) (*scheduler_models.JobStatus, error) {
	if paused, reason := e.IsPaused(ctx); paused && !force {
		return nil, numan_error.NewConflict(numan_error.CodeConflict, fmt.Sprintf("Le scheduler est en pause (%s)", reason), nil)
	}
	if e.tracker.Current() == scheduler_models.RegimeOverload && !force {
		return nil, numan_error.NewConflict(numan_error.CodeConflict, "Déclenchement refusé en régime OVERLOAD (utilisez force=true pour forcer)", nil)
	}

	e.mu.RLock()
	def, ok := e.jobs[name]
	active := e.active
	e.mu.RUnlock()
	if !ok {
		return nil, numan_error.NewNotFound(numan_error.CodeNotFound, "Job planifié introuvable: "+name, nil)
	}
	if active != nil {
		return nil, numan_error.NewConflict(numan_error.CodeConflict, "Un autre job est déjà en cours d'exécution: "+active.name, nil)
	}

	e.executeJob(ctx, def, false)
	e.mu.RLock()
	stCopy := *e.statuses[name]
	e.mu.RUnlock()
	return &stCopy, nil
}

func (e *Engine) executeJob(parentCtx context.Context, def JobDefinition, trickle bool) {
	release, acquired, errLock := AcquireJobLock(parentCtx, def.Name, def.LockScope, def.LockTTL)
	if errLock != nil {
		numan_log.Warn(parentCtx).Err(errLock).Str("job", def.Name).Msg("Erreur d'acquisition du verrou scheduler")
		return
	}
	if !acquired {
		return
	}
	defer release(context.Background())

	jobCtx, cancel := context.WithCancel(parentCtx)
	doneCh := make(chan struct{})

	e.mu.Lock()
	st := e.statuses[def.Name]
	prevCP := st.Checkpoint
	st.Status = "running"
	st.TrickleMode = trickle
	st.LastRunAtMs = domain.TimeToMillis(e.nowFn())
	st.LastError = ""
	e.active = &runningJobState{
		name:      def.Name,
		minRegime: def.MinRegime,
		trickle:   trickle,
		cancel:    cancel,
		done:      doneCh,
	}
	e.mu.Unlock()

	start := e.nowFn()
	nextCP, completed, err := def.Fn(jobCtx, prevCP, trickle)
	dur := e.nowFn().Sub(start)
	cancel()

	nowMs := domain.TimeToMillis(e.nowFn())
	nextCP.UpdatedAtMs = nowMs

	e.mu.Lock()
	e.active = nil
	st.LastDurationMs = dur.Milliseconds()
	st.TrickleMode = false

	switch {
	case completed && err == nil:
		st.Status = "completed"
		st.LastCompletedAtMs = nowMs
		st.Checkpoint = scheduler_models.JobCheckpoint{UpdatedAtMs: nowMs}
		st.ProgressPct = 100
		st.StarvationWarning = false
		st.NextEligibleAtMs = nowMs + def.MinInterval.Milliseconds()
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (!completed && jobCtx.Err() != nil):
		st.Status = "preempted"
		st.PreemptedCount++
		st.Checkpoint = nextCP
		st.ProgressPct = computeProgressPct(nextCP)
	default:
		st.Status = "failed"
		if err != nil {
			st.LastError = err.Error()
		}
		st.Checkpoint = nextCP
		st.ProgressPct = computeProgressPct(nextCP)
	}
	statusSnapshot := *st
	e.mu.Unlock()

	_ = cache_service.SaveSchedulerJobStatus(context.Background(), statusSnapshot, 7*24*time.Hour)
	close(doneCh)
}

func computeProgressPct(cp scheduler_models.JobCheckpoint) float64 {
	if cp.ItemsTotal <= 0 {
		return 0
	}
	pct := (float64(cp.ItemsDone) / float64(cp.ItemsTotal)) * 100.0
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func inHourWindow(hour, start, end int) bool {
	if start <= end {
		return hour >= start && hour < end
	}
	return hour >= start || hour < end
}

func (e *Engine) loadCheckpointsFromCache(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, name := range e.order {
		saved, ok := cache_service.GetSchedulerJobStatus(ctx, name)
		if !ok || saved == nil {
			continue
		}
		st := e.statuses[name]
		st.LastRunAtMs = saved.LastRunAtMs
		st.LastCompletedAtMs = saved.LastCompletedAtMs
		st.LastDurationMs = saved.LastDurationMs
		st.PreemptedCount = saved.PreemptedCount
		st.Checkpoint = saved.Checkpoint
		st.ProgressPct = saved.ProgressPct
		st.NextEligibleAtMs = saved.NextEligibleAtMs
		if saved.Status == "preempted" {
			st.Status = "preempted"
		}
	}
}

// StatusSnapshot retourne l'état complet du Scheduler (pour Admin Panel, CLI Telemetry et Prometheus).
func (e *Engine) StatusSnapshot(ctx context.Context) scheduler_models.SchedulerOverview {
	paused, pauseReason := e.IsPaused(ctx)
	reg := e.tracker.Current()

	e.mu.RLock()
	defer e.mu.RUnlock()

	jobs := make([]scheduler_models.JobStatus, 0, len(e.order))
	starvationAny := false
	activeJob := ""
	activeProgress := 0.0
	if e.active != nil {
		activeJob = e.active.name
	}
	if e.nowFn().Sub(e.tracker.LastDeepIdleAt()) >= defaultMaxStaleness {
		starvationAny = true
	}

	for _, name := range e.order {
		st := *e.statuses[name]
		if st.StarvationWarning {
			starvationAny = true
		}
		if st.Name == activeJob {
			activeProgress = st.ProgressPct
		}
		jobs = append(jobs, st)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })

	return scheduler_models.SchedulerOverview{
		Datacenter:         e.datacenter,
		Regime:             reg,
		Paused:             paused,
		PauseReason:        pauseReason,
		Overload:           reg == scheduler_models.RegimeOverload,
		ActiveJob:          activeJob,
		ProgressPct:        activeProgress,
		CPUBudgetPct:       reg.CPUBudgetPercent(),
		Starvation48hAlert: starvationAny,
		LastDeepIdleAtMs:   domain.TimeToMillis(e.tracker.LastDeepIdleAt()),
		Jobs:               jobs,
	}
}

// MetricsSnapshot convertit l'état du Scheduler au format Prometheus de infra_telemetry_models.
func (e *Engine) MetricsSnapshot() (infra_telemetry_models.SchedulerMetricSnapshot, bool) {
	ov := e.StatusSnapshot(context.Background())
	out := infra_telemetry_models.SchedulerMetricSnapshot{
		Datacenter:      ov.Datacenter,
		Regime:          string(ov.Regime),
		StarvationAlert: ov.Starvation48hAlert,
		Jobs:            make([]infra_telemetry_models.SchedulerJobMetric, 0, len(ov.Jobs)),
	}
	for _, j := range ov.Jobs {
		out.Jobs = append(out.Jobs, infra_telemetry_models.SchedulerJobMetric{
			Name:            j.Name,
			Running:         j.Status == "running",
			LastDurationSec: float64(j.LastDurationMs) / 1000.0,
		})
	}
	return out, true
}
