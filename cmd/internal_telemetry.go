package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/admin_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg/metrics"
	"github.com/QuentinRegnier/numan-backend/internal/service/infra_telemetry_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/scheduler_service"
)

// buildInternalMetricsMux construit le routeur interne (:9090, jamais exposé par Traefik Ingress)
// consommé par Prometheus et par numan-cli via le proxy Kubernetes API (client-go over WireGuard).
func buildInternalMetricsMux(defaultDatacenter string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/internal/cli-telemetry", handleInternalCLITelemetry(defaultDatacenter))
	mux.HandleFunc("/internal/scheduler/pause", handleInternalSchedulerPause)
	mux.HandleFunc("/internal/scheduler/resume", handleInternalSchedulerResume)
	mux.HandleFunc("/internal/scheduler/jobs/trigger", handleInternalSchedulerTrigger)
	return mux
}

func handleInternalCLITelemetry(defaultDatacenter string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeInternalJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		dc := defaultDatacenter
		if r.Method == http.MethodPost && r.Body != nil {
			defer r.Body.Close()
			var input admin_models.GetTelemetryOverviewInput
			if body, err := io.ReadAll(io.LimitReader(r.Body, 4096)); err == nil && len(strings.TrimSpace(string(body))) > 0 {
				if errJ := json.Unmarshal(body, &input); errJ != nil {
					writeInternalJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
					return
				}
				if errV := input.Validate(); errV != nil {
					writeInternalJSON(w, http.StatusBadRequest, map[string]string{"error": errV.Error()})
					return
				}
				if input.Datacenter != "" {
					dc = input.Datacenter
				}
			}
		}
		snap, err := infra_telemetry_service.GetOrComputeTelemetrySnapshot(r.Context(), dc)
		if err != nil {
			numan_log.Warn(r.Context()).Err(err).Str("datacenter", dc).Msg("Échec /internal/cli-telemetry")
			writeInternalJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to collect telemetry snapshot"})
			return
		}
		writeInternalJSON(w, http.StatusOK, snap)
	}
}

func handleInternalSchedulerPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeInternalJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload admin_models.PauseSchedulerInput
	if r.Body != nil {
		defer r.Body.Close()
		body, err := io.ReadAll(io.LimitReader(r.Body, 8192))
		if err == nil && len(strings.TrimSpace(string(body))) > 0 {
			if errJ := json.Unmarshal(body, &payload); errJ != nil {
				writeInternalJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
				return
			}
		}
	}
	if err := payload.Validate(); err != nil {
		writeInternalJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ttlMin := payload.TTLMinutes
	if ttlMin <= 0 {
		ttlMin = 30
	}
	reason := payload.Reason
	if reason == "" {
		reason = "cli_maintenance_pause"
	}

	if err := scheduler_service.PauseGlobal(r.Context(), time.Duration(ttlMin)*time.Minute, reason); err != nil {
		writeInternalJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to pause scheduler"})
		return
	}
	numan_log.Info(r.Context()).Int("ttl_minutes", ttlMin).Str("reason", reason).Msg("Internal CLI: scheduler paused")
	writeInternalJSON(w, http.StatusOK, admin_models.SchedulerActionOutput{
		Status:     "paused",
		TTLMinutes: ttlMin,
		Reason:     reason,
	})
}

func handleInternalSchedulerResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeInternalJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if err := scheduler_service.ResumeGlobal(r.Context()); err != nil {
		writeInternalJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to resume scheduler"})
		return
	}
	numan_log.Info(r.Context()).Msg("Internal CLI: scheduler resumed")
	writeInternalJSON(w, http.StatusOK, admin_models.SchedulerActionOutput{
		Status: "resumed",
	})
}

func handleInternalSchedulerTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeInternalJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload admin_models.TriggerSchedulerJobInput
	if r.Body != nil {
		defer r.Body.Close()
		body, err := io.ReadAll(io.LimitReader(r.Body, 8192))
		if err != nil || len(strings.TrimSpace(string(body))) == 0 {
			writeInternalJSON(w, http.StatusBadRequest, map[string]string{"error": "json payload required"})
			return
		}
		if errJ := json.Unmarshal(body, &payload); errJ != nil {
			writeInternalJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
			return
		}
	}
	if err := payload.Validate(); err != nil {
		writeInternalJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	st, queued, err := scheduler_service.TriggerJobGlobal(r.Context(), payload.JobName, payload.Force)
	if err != nil {
		code := http.StatusInternalServerError
		if appErr, ok := err.(*numan_error.AppError); ok {
			code = appErr.HTTPStatus
		}
		writeInternalJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	status := "executed"
	httpCode := http.StatusOK
	if queued {
		status = "queued_remote_trigger"
		httpCode = http.StatusAccepted
	}
	numan_log.Info(r.Context()).Str("job", payload.JobName).Bool("force", payload.Force).Bool("queued", queued).Msg("Internal CLI: scheduler job triggered")
	writeInternalJSON(w, httpCode, admin_models.SchedulerActionOutput{
		Status: status,
		Job:    st,
		Queued: queued,
	})
}

func writeInternalJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
