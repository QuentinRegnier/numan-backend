package postgres

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/report_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/lib/pq"
)

// FuncLoadPendingReportsPaginated charge les tickets de signalement en attente triés par importance décroissante.
func FuncLoadPendingReportsPaginated(ctx context.Context, limit, offset int64) ([]report_models.ReportPayload, error) {
	return FuncLoadPendingReportsFilteredPaginated(ctx, limit, offset, nil, nil)
}

// funcLoadPendingReportsFilteredPaginated charge les tickets en attente avec filtrage optionnel par origine (User/AutoAI/Hybrid) et type de cible.
func FuncLoadPendingReportsFilteredPaginated(ctx context.Context, limit, offset int64, originFilter *int, targetTypeFilter *int) ([]report_models.ReportPayload, error) {
	if postgres.PostgresDB == nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Le service de base de données est momentanément indisponible.",
			errors.New("PostgreSQL connection 'PostgresDB' is not initialized"),
		)
	}
	var originArg, targetTypeArg any
	if originFilter != nil {
		originArg = *originFilter
	}
	if targetTypeFilter != nil {
		targetTypeArg = *targetTypeFilter
	}
	const query = `SELECT id, reporter_id, report_origin, target_type, target_ids, target_owner_id, author_trust_score, ia_score_max, accumulated_report_score, report_count, category, reason, rationale, state, importance, created_at, updated_at FROM moderation.func_load_pending_reports_paginated($1::integer, $2::integer, $3::smallint, $4::smallint)`
	rows, err := postgres.PostgresDB.QueryContext(ctx, query, limit, offset, originArg, targetTypeArg)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible de charger les signalements en attente.",
			err,
		)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
	}()

	reports := make([]report_models.ReportPayload, 0)
	for rows.Next() {
		var r report_models.ReportPayload
		if err := rows.Scan(
			&r.ID, &r.ReporterID, &r.ReportOrigin, &r.TargetType, pq.Array(&r.TargetIDs),
			&r.TargetOwnerID, &r.AuthorTrustScore, &r.IAScoreMax,
			&r.AccumulatedReportScore, &r.ReportCount, &r.Category,
			&r.Reason, &r.Rationale, &r.State, &r.Importance,
			&r.CreatedAt, &r.UpdatedAt,
		); err == nil {
			r.ReportOriginLabel = report_models.ResolveOriginLabel(r.ReportOrigin)
			reports = append(reports, r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeDatabaseError,
			"Impossible d'itérer sur les signalements en attente.",
			err,
		)
	}
	return reports, nil
}
