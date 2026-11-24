package scheduler_service

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/scheduler_models"
)

// DefaultRegimeConfig retourne la configuration par défaut conforme à la spécification.
func DefaultRegimeConfig() scheduler_models.RegimeConfig {
	return scheduler_models.RegimeConfig{
		DeepIdleThreshold:      0.10,
		IdleThreshold:          0.30,
		ModerateThreshold:      0.60,
		OverloadThreshold:      0.85,
		OverloadP95Ms:          800.0,
		OverloadQueueAgeSec:    30.0,
		OverloadPGWritePoolPct: 0.90,
		DownshiftHysteresis:    3 * time.Minute,
	}
}

// ResolveRegimeConfigFromEnv lit les surcharges optionnelles depuis les variables d'environnement.
func ResolveRegimeConfigFromEnv() scheduler_models.RegimeConfig {
	cfg := DefaultRegimeConfig()
	if v := parseEnvFloatFallback("NUMAN_SCHEDULER_OVERLOAD_THRESHOLD", "SCHEDULER_OVERLOAD_THRESHOLD"); v > 0 && v <= 1 {
		cfg.OverloadThreshold = v
	}
	if v := parseEnvFloatFallback("NUMAN_SCHEDULER_PEAK_THRESHOLD", "SCHEDULER_PEAK_THRESHOLD"); v > 0 && v <= 1 {
		cfg.ModerateThreshold = v
	}
	if v := parseEnvFloatFallback("NUMAN_SCHEDULER_IDLE_THRESHOLD", "SCHEDULER_IDLE_THRESHOLD"); v > 0 && v <= 1 {
		cfg.IdleThreshold = v
	}
	if v := parseEnvFloatFallback("NUMAN_SCHEDULER_DEEP_IDLE_THRESHOLD", "SCHEDULER_DEEP_IDLE_THRESHOLD"); v > 0 && v <= 1 {
		cfg.DeepIdleThreshold = v
	}
	if raw := strings.TrimSpace(os.Getenv("NUMAN_SCHEDULER_HYSTERESIS_SEC")); raw != "" {
		if sec, err := strconv.Atoi(raw); err == nil && sec >= 0 {
			cfg.DownshiftHysteresis = time.Duration(sec) * time.Second
		}
	} else if raw := strings.TrimSpace(os.Getenv("SCHEDULER_HYSTERESIS_SEC")); raw != "" {
		if sec, err := strconv.Atoi(raw); err == nil && sec >= 0 {
			cfg.DownshiftHysteresis = time.Duration(sec) * time.Second
		}
	}
	return cfg
}

func parseEnvFloatFallback(primaryKey, fallbackKey string) float64 {
	if v := parseEnvFloat(primaryKey); v > 0 {
		return v
	}
	return parseEnvFloat(fallbackKey)
}

func parseEnvFloat(key string) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return v
}

// ClassifyRawRegime évalue le régime instantané (sans hystérésis) à partir d'un échantillon d'activité.
func ClassifyRawRegime(cfg scheduler_models.RegimeConfig, s scheduler_models.ActivitySample) scheduler_models.Regime {
	if s.SaturationScore >= cfg.OverloadThreshold ||
		s.HTTPP95LatencyMs > cfg.OverloadP95Ms ||
		s.QueueOldestEventAgeSec > cfg.OverloadQueueAgeSec ||
		s.PostgresWritePoolUtil >= cfg.OverloadPGWritePoolPct {
		return scheduler_models.RegimeOverload
	}
	if s.ActivityScore >= cfg.ModerateThreshold {
		return scheduler_models.RegimePeak
	}
	if s.ActivityScore >= cfg.IdleThreshold {
		return scheduler_models.RegimeModerate
	}
	if s.ActivityScore >= cfg.DeepIdleThreshold {
		return scheduler_models.RegimeIdle
	}
	return scheduler_models.RegimeDeepIdle
}

// RegimeTracker gère l'état du régime avec hystérésis configurable et escalade immédiate vers OVERLOAD/PEAK.
type RegimeTracker struct {
	mu             sync.Mutex
	cfg            scheduler_models.RegimeConfig
	current        scheduler_models.Regime
	pending        scheduler_models.Regime
	pendingSince   time.Time
	lastDeepIdleAt time.Time
	nowFn          func() time.Time
}

// NewRegimeTracker crée un tracker de régime initialisé à MODERATE (prudent au boot).
func NewRegimeTracker(cfg scheduler_models.RegimeConfig, nowFn func() time.Time) *RegimeTracker {
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	return &RegimeTracker{
		cfg:            cfg,
		current:        scheduler_models.RegimeModerate,
		lastDeepIdleAt: now,
		nowFn:          nowFn,
	}
}

// Current retourne le régime stabilisé courant.
func (t *RegimeTracker) Current() scheduler_models.Regime {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.current
}

// LastDeepIdleAt retourne l'horodatage de la dernière fenêtre DEEP_IDLE observée.
func (t *RegimeTracker) LastDeepIdleAt() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastDeepIdleAt
}

// SetLastDeepIdleAt initialise ou restaure l'horodatage DEEP_IDLE (ex: tests ou reprise).
func (t *RegimeTracker) SetLastDeepIdleAt(ts time.Time) {
	t.mu.Lock()
	t.lastDeepIdleAt = ts
	t.mu.Unlock()
}

// Update ingère un échantillon et retourne le régime actif après application des règles d'hystérésis.
//   - Escalade vers OVERLOAD ou PEAK : IMMÉDIATE (0s d'hystérésis) pour préempter en <200ms.
//   - Descente vers un régime plus calme : doit rester stable pendant cfg.DownshiftHysteresis.
func (t *RegimeTracker) Update(sample scheduler_models.ActivitySample) (regime scheduler_models.Regime, changed bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.nowFn()
	raw := ClassifyRawRegime(t.cfg, sample)

	if raw == t.current {
		t.pending = ""
		t.pendingSince = time.Time{}
		if t.current == scheduler_models.RegimeDeepIdle {
			t.lastDeepIdleAt = now
		}
		return t.current, false
	}

	// Escalade défensive immédiate vers OVERLOAD ou PEAK
	if raw == scheduler_models.RegimeOverload || (raw == scheduler_models.RegimePeak && t.current.Priority() < scheduler_models.RegimePeak.Priority()) {
		t.current = raw
		t.pending = ""
		t.pendingSince = time.Time{}
		return t.current, true
	}

	if t.cfg.DownshiftHysteresis <= 0 {
		t.current = raw
		t.pending = ""
		t.pendingSince = time.Time{}
		if t.current == scheduler_models.RegimeDeepIdle {
			t.lastDeepIdleAt = now
		}
		return t.current, true
	}

	if t.pending != raw {
		t.pending = raw
		t.pendingSince = now
		return t.current, false
	}

	if now.Sub(t.pendingSince) >= t.cfg.DownshiftHysteresis {
		t.current = raw
		t.pending = ""
		t.pendingSince = time.Time{}
		if t.current == scheduler_models.RegimeDeepIdle {
			t.lastDeepIdleAt = now
		}
		return t.current, true
	}

	return t.current, false
}
