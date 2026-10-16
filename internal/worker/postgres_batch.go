package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/message_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/lib/pq"
)

// ============================================================================
// CONSTANTES SPÉCIFIQUES AU BATCH POSTGRESQL
// ============================================================================
const (
	postgresDLQQueue = "postgres_errors" // File de quarantaine (Dead Letter Queue)
)

// ############################################################################
// # WORKER BATCH : POSTGRESQL (COLD STORAGE L3 - SOURCE DE VÉRITÉ)
// ############################################################################

// generateCopyQuery génère dynamiquement une requête COPY IN optimisée pour PostgreSQL
// en prenant en compte le schéma et la table.
func generateCopyQuery(fullTableName string, columns []string) string {
	quotedTableName := ""
	if strings.Contains(fullTableName, ".") {
		parts := strings.SplitN(fullTableName, ".", 2)
		quotedTableName = pq.QuoteIdentifier(parts[0]) + "." + pq.QuoteIdentifier(parts[1])
	} else {
		quotedTableName = pq.QuoteIdentifier(fullTableName)
	}

	quotedColumns := make([]string, len(columns))
	for i, col := range columns {
		quotedColumns[i] = pq.QuoteIdentifier(col)
	}

	return fmt.Sprintf("COPY %s (%s) FROM STDIN", quotedTableName, strings.Join(quotedColumns, ", "))
}

// flushPostgres est le chef d'orchestre de l'écriture relationnelle.
// Il garantit l'intégrité référentielle en ordonnant les requêtes topologiquement.
func flushPostgres(ctx context.Context, events []redis.AsyncEvent) {
	// ── ÉTAPE 1 : GROUPEMENT DES ÉVÉNEMENTS ─────────────────────────────────
	grouped := make(map[redis.EntityType][]redis.AsyncEvent)
	for _, e := range events {
		grouped[e.Type] = append(grouped[e.Type], e)
	}

	// ── ÉTAPE 2 : TRI TOPOLOGIQUE (INTÉGRITÉ RÉFÉRENTIELLE) ─────────────────
	// Empêche les violations de clés étrangères (Ex: insérer un post avant l'utilisateur).
	executionOrder := []redis.EntityType{
		redis.EntityUser,
		redis.EntityUserSettings,
		redis.EntitySession,
		redis.EntityRelation,
		redis.EntityPost,
		redis.EntityComment,
		redis.EntityLike,
		redis.EntityMedia,
		redis.EntityConversation,
		redis.EntityMembers,
		redis.EntityMessage,
	}

	var persistedEvents []redis.AsyncEvent

	// 1. Exécution ordonnée pour les entités liées
	for _, entityType := range executionOrder {
		if evts, exists := grouped[entityType]; exists {
			persisted := processEntityEvents(ctx, entityType, evts)
			persistedEvents = append(persistedEvents, persisted...)
			delete(grouped, entityType)
		}
	}

	// 2. Exécution du reste (Entités sans dépendances critiques)
	for entityType, evts := range grouped {
		persisted := processEntityEvents(ctx, entityType, evts)
		persistedEvents = append(persistedEvents, persisted...)
	}

	// ── ÉTAPE 3 : MISE À JOUR DES COMPTEURS (DÉNORMALISATION SQL) ───────────
	// WB-4: Seuls les événements dont la persistance a réussi modifient les compteurs
	updateCountersPostgres(ctx, persistedEvents)
}

// processEntityEvents achemine le lot d'événements vers la bonne opération SQL (Fast Path).
// Si le lot échoue, il déclenche le mécanisme de résilience par dichotomie (Slow Path).
// Retourne uniquement les événements qui ont été persistés avec succès.
func processEntityEvents(ctx context.Context, entityType redis.EntityType, events []redis.AsyncEvent) []redis.AsyncEvent {
	// WB-9: Exclusion explicite des entités sans table relationnelle (Vues, Télémétrie).
	// Elles n'ont pas de mapper et sont consommées exclusivement par updateCountersPostgres.
	if entityType == redis.EntityView || entityType == redis.EntityTelemetry {
		return events
	}

	var inserts, updates, deletes, others []redis.AsyncEvent

	for _, e := range events {
		switch e.Action {
		case redis.ActionCreate:
			inserts = append(inserts, e)
		case redis.ActionUpdate:
			updates = append(updates, e)
		case redis.ActionDelete:
			deletes = append(deletes, e)
		default:
			others = append(others, e)
		}
	}

	var persisted []redis.AsyncEvent

	if len(inserts) > 0 {
		if err := bulkInsertPostgres(ctx, entityType, inserts); err != nil {
			numan_log.Warn(ctx).Interface("entity", entityType).Msg("Fast Path Insert échoué. Déclenchement Dichotomie...")
			persisted = append(persisted, slowPathDichotomy(ctx, entityType, redis.ActionCreate, inserts)...)
		} else {
			persisted = append(persisted, inserts...)
		}
	}
	if len(updates) > 0 {
		if err := bulkUpdatePostgres(ctx, entityType, updates); err != nil {
			numan_log.Warn(ctx).Interface("entity", entityType).Msg("Fast Path Update échoué. Déclenchement Dichotomie...")
			persisted = append(persisted, slowPathDichotomy(ctx, entityType, redis.ActionUpdate, updates)...)
		} else {
			persisted = append(persisted, updates...)
		}
	}
	if len(deletes) > 0 {
		if err := bulkDeletePostgres(ctx, entityType, deletes); err != nil {
			numan_log.Warn(ctx).Interface("entity", entityType).Msg("Fast Path Delete échoué. Déclenchement Dichotomie...")
			persisted = append(persisted, slowPathDichotomy(ctx, entityType, redis.ActionDelete, deletes)...)
		} else {
			persisted = append(persisted, deletes...)
		}
	}

	// Préserver les actions spéciales (ex: ActionBuild pour les réactions de messages)
	persisted = append(persisted, others...)

	return persisted
}

// ============================================================================
// RÉSILIENCE : DICHOTOMIE & DEAD LETTER QUEUE (DLQ)
// ============================================================================

// slowPathDichotomy divise récursivement un lot en échec pour isoler la requête corrompue
// (ex: Doublon de clé unique, Violation de Foreign Key) sans bloquer les autres requêtes valides.
// Retourne la liste des événements dont la persistance a réussi.
func slowPathDichotomy(ctx context.Context, entity redis.EntityType, action redis.ActionType, events []redis.AsyncEvent) []redis.AsyncEvent {
	if len(events) == 0 {
		return nil
	}

	var err error
	switch action {
	case redis.ActionCreate:
		err = bulkInsertPostgres(ctx, entity, events)
	case redis.ActionUpdate:
		err = bulkUpdatePostgres(ctx, entity, events)
	case redis.ActionDelete:
		err = bulkDeletePostgres(ctx, entity, events)
	}

	// Si ça passe, on a sauvé ce sous-lot, on arrête l'exploration.
	if err == nil {
		return events
	}

	// Si on échoue et qu'il n'y a qu'UN SEUL élément, c'est le coupable (Poison Pill) !
	if len(events) == 1 {
		sendToDLQ(ctx, entity, action, events[0], err)
		return nil
	}

	// Sinon on coupe en deux et on relance l'arbre de recherche.
	mid := len(events) / 2
	left := slowPathDichotomy(ctx, entity, action, events[:mid])
	right := slowPathDichotomy(ctx, entity, action, events[mid:])
	return append(left, right...)
}

// sendToDLQ place l'événement impossible à exécuter en quarantaine.
func sendToDLQ(ctx context.Context, entity redis.EntityType, action redis.ActionType, event redis.AsyncEvent, dbErr error) {
	dlqPayload := map[string]any{
		"numan_error": dbErr.Error(),
		"time":        time.Now().Format(time.RFC3339),
		"entity":      entity,
		"action":      action,
		"event":       event,
	}

	bytes, err := json.Marshal(dlqPayload)
	if err == nil {
		_ = redis.DLQ.LPush(ctx, postgresDLQQueue, bytes)
		numan_log.Error(ctx).
			Err(dbErr).
			Interface("entity", entity).
			Interface("action", action).
			Int64("event_id", event.ID).
			Msg("Postgres Worker : Événement empoisonné isolé et mis en quarantaine (DLQ)")
	}
}

// ============================================================================
// 1. BULK INSERT (COPY IN)
// ============================================================================

func bulkInsertPostgres(ctx context.Context, entity redis.EntityType, events []redis.AsyncEvent) error {
	// ── CAS PARTICULIER : UPSERT DES RÉACTIONS ──────────────────────────────
	if entity == redis.EntityMessageReaction {
		return handleMessageReactionUpsert(ctx, events)
	}

	mapper := getMapper(entity)
	if mapper == nil {
		err := errors.New("Aucun mapper Postgres défini pour l'entité")
		numan_log.Error(ctx).Err(err).Interface("entity", entity).Msg("Échec BulkInsert")
		return numan_error.NewInternal()
	}

	tx, errTx := postgres.PostgresDB.BeginTx(ctx, nil)
	if errTx != nil {
		numan_log.Error(ctx).Err(errTx).Msg("Échec démarrage transaction BulkInsert")
		return numan_error.NewInternal()
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	copyQuery := generateCopyQuery(mapper.TableName(), mapper.Columns())

	stmt, errStmt := tx.Prepare(copyQuery)
	if errStmt != nil {
		numan_log.Error(ctx).Err(errStmt).Msg("Échec préparation statement COPY IN")
		return numan_error.NewInternal()
	}
	defer func(stmt *sql.Stmt) {
		if errClose := stmt.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Erreur fermeture statement CopyIn")
		}
	}(stmt)

	for _, e := range events {
		row, errRow := mapper.ToRow(e.Payload)
		if errRow != nil {
			numan_log.Error(ctx).Err(errRow).Int64("id", e.ID).Msg("Échec mapping ligne SQL")
			return numan_error.NewInternal()
		}
		if _, errExec := stmt.Exec(row...); errExec != nil {
			numan_log.Error(ctx).Err(errExec).Msg("Échec exécution COPY IN")
			return numan_error.NewInternal()
		}
	}

	if _, errFlush := stmt.Exec(); errFlush != nil {
		numan_log.Error(ctx).Err(errFlush).Msg("Échec Flush COPY IN")
		return numan_error.NewInternal()
	}

	if errCommit := tx.Commit(); errCommit != nil {
		numan_log.Error(ctx).Err(errCommit).Msg("Échec Commit COPY IN")
		return numan_error.NewInternal()
	}

	committed = true
	return nil
}

// handleMessageReactionUpsert gère l'insertion d'une réaction avec résolution de conflit.
func handleMessageReactionUpsert(ctx context.Context, events []redis.AsyncEvent) error {
	tx, errTx := postgres.PostgresDB.BeginTx(ctx, nil)
	if errTx != nil {
		numan_log.Error(ctx).Err(errTx).Msg("Échec démarrage transaction Upsert")
		return numan_error.NewInternal()
	}

	stmt, errStmt := tx.Prepare(`
		INSERT INTO messaging.message_reactions (id, message_id, user_id, reaction, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (message_id, user_id) 
		DO UPDATE SET reaction = EXCLUDED.reaction, created_at = EXCLUDED.created_at
	`)
	if errStmt != nil {
		_ = tx.Rollback()
		numan_log.Error(ctx).Err(errStmt).Msg("Échec préparation statement Upsert")
		return numan_error.NewInternal()
	}

	defer func(stmt *sql.Stmt) {
		if errClose := stmt.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Erreur fermeture statement Upsert")
		}
	}(stmt)

	for _, e := range events {
		jsonBytes, _ := json.Marshal(e.Payload)
		var r message_models.MessageReactionPayload
		_ = json.Unmarshal(jsonBytes, &r)

		if _, errExec := stmt.Exec(r.ID, r.MessageID, r.UserID, r.Reaction, r.CreatedAt); errExec != nil {
			_ = tx.Rollback()
			numan_log.Error(ctx).Err(errExec).Msg("Échec exécution de la ligne Upsert")
			return numan_error.NewInternal()
		}
	}

	if errCommit := tx.Commit(); errCommit != nil {
		numan_log.Error(ctx).Err(errCommit).Msg("Échec Commit Upsert")
		return numan_error.NewInternal()
	}
	return nil
}

// ============================================================================
// 2. BULK UPDATE (Table Temporaire + COPY IN + UPDATE FROM)
// ============================================================================

func bulkUpdatePostgres(ctx context.Context, entity redis.EntityType, events []redis.AsyncEvent) error {
	mapper := getMapper(entity)
	if mapper == nil {
		err := errors.New("Aucun mapper Postgres défini")
		numan_log.Error(ctx).Err(err).Interface("entity", entity).Msg("Échec BulkUpdate")
		return numan_error.NewInternal()
	}

	// ── DÉDUPLICATION RAM (LAST-WRITE-WINS) ─────────────────────────────────
	// Indispensable : Si un événement a muté deux fois dans la même fenêtre de Flush,
	// injecter deux fois la même Primary Key dans la table temporaire ferait crasher
	// la commande SQL "UPDATE ... FROM".
	dedupMap := make(map[int64]redis.AsyncEvent)
	for _, e := range events {
		dedupMap[e.ID] = e
	}

	dedupEvents := make([]redis.AsyncEvent, 0, len(dedupMap))
	for _, e := range dedupMap {
		dedupEvents = append(dedupEvents, e)
	}

	tx, errTx := postgres.PostgresDB.BeginTx(ctx, nil)
	if errTx != nil {
		numan_log.Error(ctx).Err(errTx).Msg("Échec démarrage transaction BulkUpdate")
		return numan_error.NewInternal()
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Génération d'une table temporaire propre à la session
	safeTableName := strings.ReplaceAll(mapper.TableName(), ".", "_")
	tempTable := fmt.Sprintf("tmp_%s_%d", safeTableName, time.Now().UnixNano())

	queryCreateTable := fmt.Sprintf("CREATE TEMP TABLE %s (LIKE %s INCLUDING DEFAULTS) ON COMMIT DROP", tempTable, mapper.TableName())
	if _, errExec := tx.ExecContext(ctx, queryCreateTable); errExec != nil {
		numan_log.Error(ctx).Err(errExec).Msg("Échec création table temporaire BulkUpdate")
		return numan_error.NewInternal()
	}

	// Remplissage de la table temporaire
	stmt, errStmt := tx.Prepare(pq.CopyIn(tempTable, mapper.Columns()...))
	if errStmt != nil {
		numan_log.Error(ctx).Err(errStmt).Msg("Échec préparation CopyIn Temp Table")
		return numan_error.NewInternal()
	}
	defer func(stmt *sql.Stmt) {
		if errClose := stmt.Close(); errClose != nil {
			numan_log.Error(ctx).Err(errClose).Msg("Erreur fermeture statement CopyIn Temp")
		}
	}(stmt)

	for _, e := range dedupEvents {
		row, errRow := mapper.ToRow(e.Payload)
		if errRow != nil {
			numan_log.Error(ctx).Err(errRow).Int64("id", e.ID).Msg("Échec mapping de la ligne Temp Table")
			return numan_error.NewInternal()
		}
		if _, errExec := stmt.Exec(row...); errExec != nil {
			numan_log.Error(ctx).Err(errExec).Msg("Échec exécution ligne CopyIn Temp Table")
			return numan_error.NewInternal()
		}
	}

	if _, errFlush := stmt.Exec(); errFlush != nil {
		numan_log.Error(ctx).Err(errFlush).Msg("Échec Flush Temp Table")
		return numan_error.NewInternal()
	}

	// Application des changements (Merge) de la table temporaire vers la table réelle
	queryUpdate := mapper.BuildUpdateQuery(tempTable)
	if _, errMerge := tx.ExecContext(ctx, queryUpdate); errMerge != nil {
		numan_log.Error(ctx).Err(errMerge).Msg("Échec requête MERGE UPDATE")
		return numan_error.NewInternal()
	}

	if errCommit := tx.Commit(); errCommit != nil {
		numan_log.Error(ctx).Err(errCommit).Msg("Échec Commit BulkUpdate")
		return numan_error.NewInternal()
	}

	committed = true
	return nil
}

// ============================================================================
// 3. BULK DELETE (CASCADES LOGIQUES ET PHYSIQUES)
// ============================================================================

func bulkDeletePostgres(ctx context.Context, entity redis.EntityType, events []redis.AsyncEvent) error {
	mapper := getMapper(entity)
	if mapper == nil {
		err := errors.New("Aucun mapper Postgres défini")
		numan_log.Error(ctx).Err(err).Interface("entity", entity).Msg("Échec BulkDelete")
		return numan_error.NewInternal()
	}

	// ── CAS SPÉCIAL : LES LIKES (Clé Composite) ─────────────────────────────
	if entity == redis.EntityLike {
		tx, errTx := postgres.PostgresDB.BeginTx(ctx, nil)
		if errTx != nil {
			numan_log.Error(ctx).Err(errTx).Msg("Échec démarrage transaction Like Delete")
			return numan_error.NewInternal()
		}

		stmt, errStmt := tx.Prepare("DELETE FROM content.likes WHERE target_type = $1 AND target_id = $2 AND user_id = $3")
		if errStmt != nil {
			_ = tx.Rollback()
			numan_log.Error(ctx).Err(errStmt).Msg("Échec préparation statement Like Delete")
			return numan_error.NewInternal()
		}
		defer func(stmt *sql.Stmt) {
			if errClose := stmt.Close(); errClose != nil {
				numan_log.Error(ctx).Err(errClose).Msg("Erreur fermeture statement Delete Likes")
			}
		}(stmt)

		for _, e := range events {
			jsonBytes, _ := json.Marshal(e.Payload)
			var l map[string]interface{}
			_ = json.Unmarshal(jsonBytes, &l)

			targetType := int(l["target_type"].(float64))
			targetID := int64(l["target_id"].(float64))
			userID := int64(l["user_id"].(float64))

			if _, errExec := stmt.Exec(targetType, targetID, userID); errExec != nil {
				_ = tx.Rollback()
				numan_log.Error(ctx).Err(errExec).Msg("Échec exécution Like Delete")
				return numan_error.NewInternal()
			}
		}

		if errCommit := tx.Commit(); errCommit != nil {
			numan_log.Error(ctx).Err(errCommit).Msg("Échec Commit Like Delete")
			return numan_error.NewInternal()
		}
		return nil
	}

	// ── CAS SPÉCIAL : LES RELATIONS (Clé Composite) ─────────────────────────
	if entity == redis.EntityRelation {
		tx, errTx := postgres.PostgresDB.BeginTx(ctx, nil)
		if errTx != nil {
			numan_log.Error(ctx).Err(errTx).Msg("Échec démarrage transaction Relation Delete")
			return numan_error.NewInternal()
		}

		stmt, errStmt := tx.Prepare("DELETE FROM auth.relations WHERE primary_id = $1 AND secondary_id = $2")
		if errStmt != nil {
			_ = tx.Rollback()
			numan_log.Error(ctx).Err(errStmt).Msg("Échec préparation statement Relation Delete")
			return numan_error.NewInternal()
		}
		defer func(stmt *sql.Stmt) {
			if errClose := stmt.Close(); errClose != nil {
				numan_log.Error(ctx).Err(errClose).Msg("Erreur fermeture statement Delete Relations")
			}
		}(stmt)

		for _, e := range events {
			jsonBytes, _ := json.Marshal(e.Payload)
			var r map[string]interface{}
			_ = json.Unmarshal(jsonBytes, &r)

			primaryID := int64(r["primary_id"].(float64))
			secondaryID := int64(r["secondary_id"].(float64))
			if _, errExec := stmt.Exec(primaryID, secondaryID); errExec != nil {
				_ = tx.Rollback()
				numan_log.Error(ctx).Err(errExec).Msg("Échec exécution Relation Delete")
				return numan_error.NewInternal()
			}
		}

		if errCommit := tx.Commit(); errCommit != nil {
			numan_log.Error(ctx).Err(errCommit).Msg("Échec Commit Relation Delete")
			return numan_error.NewInternal()
		}
		return nil
	}

	// ── CAS SPÉCIAL : RÉACTIONS AUX MESSAGES (Clé Composite) ────────────────
	if entity == redis.EntityMessageReaction {
		tx, errTx := postgres.PostgresDB.BeginTx(ctx, nil)
		if errTx != nil {
			numan_log.Error(ctx).Err(errTx).Msg("Échec démarrage transaction Msg Reaction Delete")
			return numan_error.NewInternal()
		}

		stmt, errStmt := tx.Prepare("DELETE FROM messaging.message_reactions WHERE message_id = $1 AND user_id = $2")
		if errStmt != nil {
			_ = tx.Rollback()
			numan_log.Error(ctx).Err(errStmt).Msg("Échec préparation statement Msg Reaction Delete")
			return numan_error.NewInternal()
		}
		defer func(stmt *sql.Stmt) {
			if errClose := stmt.Close(); errClose != nil {
				numan_log.Error(ctx).Err(errClose).Msg("Erreur fermeture statement Delete Message Reactions")
			}
		}(stmt)

		for _, e := range events {
			jsonBytes, _ := json.Marshal(e.Payload)
			var r message_models.MessageReactionPayload
			_ = json.Unmarshal(jsonBytes, &r)

			if _, errExec := stmt.Exec(r.MessageID, r.UserID); errExec != nil {
				_ = tx.Rollback()
				numan_log.Error(ctx).Err(errExec).Msg("Échec exécution Msg Reaction Delete")
				return numan_error.NewInternal()
			}
		}

		if errCommit := tx.Commit(); errCommit != nil {
			numan_log.Error(ctx).Err(errCommit).Msg("Échec Commit Msg Reaction Delete")
			return numan_error.NewInternal()
		}
		return nil
	}

	// ── CAS SPÉCIAL : FAVORIS (Clé Composite) ───────────────────────────────
	if entity == redis.EntitySaved {
		tx, errTx := postgres.PostgresDB.BeginTx(ctx, nil)
		if errTx != nil {
			numan_log.Error(ctx).Err(errTx).Msg("Échec démarrage transaction Saved Delete")
			return numan_error.NewInternal()
		}

		stmt, errStmt := tx.Prepare("DELETE FROM content.saved WHERE user_id = $1 AND post_id = $2")
		if errStmt != nil {
			_ = tx.Rollback()
			numan_log.Error(ctx).Err(errStmt).Msg("Échec préparation statement Saved Delete")
			return numan_error.NewInternal()
		}
		defer func(stmt *sql.Stmt) {
			if errClose := stmt.Close(); errClose != nil {
				numan_log.Error(ctx).Err(errClose).Msg("Erreur fermeture statement Delete Saved")
			}
		}(stmt)

		for _, e := range events {
			jsonBytes, _ := json.Marshal(e.Payload)
			var s map[string]interface{}
			_ = json.Unmarshal(jsonBytes, &s)

			userID := int64(s["user_id"].(float64))
			postID := int64(s["post_id"].(float64))
			if _, errExec := stmt.Exec(userID, postID); errExec != nil {
				_ = tx.Rollback()
				numan_log.Error(ctx).Err(errExec).Msg("Échec exécution Saved Delete")
				return numan_error.NewInternal()
			}
		}

		if errCommit := tx.Commit(); errCommit != nil {
			numan_log.Error(ctx).Err(errCommit).Msg("Échec Commit Saved Delete")
			return numan_error.NewInternal()
		}
		return nil
	}

	// ── COMPORTEMENT STANDARD (Par IDs) ─────────────────────────────────────
	ids := make([]int64, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}

	var query string

	if entity == redis.EntityPost {
		// A. Soft Delete des posts (visibility = -1)
		query = fmt.Sprintf("UPDATE %s SET visibility = -1 WHERE id = ANY($1)", mapper.TableName())
		if _, errExec := postgres.PostgresDB.ExecContext(ctx, query, pq.Array(ids)); errExec != nil {
			numan_log.Error(ctx).Err(errExec).Msg("Échec Soft Delete Posts")
			return numan_error.NewInternal()
		}

		// B. Cascade Logicielle (Soft & Hard Delete)
		_, _ = postgres.PostgresDB.ExecContext(ctx, "UPDATE content.comments SET visibility = -1, updated_at = NOW() WHERE post_id = ANY($1)", pq.Array(ids))
		_, _ = postgres.PostgresDB.ExecContext(ctx, "DELETE FROM content.likes WHERE target_type = 0 AND target_id = ANY($1)", pq.Array(ids))
		_, _ = postgres.PostgresDB.ExecContext(ctx, "DELETE FROM content.saved WHERE post_id = ANY($1)", pq.Array(ids))
		_, _ = postgres.PostgresDB.ExecContext(ctx, "UPDATE content.media SET visibility = false, updated_at = util.now_ms() WHERE id IN (SELECT unnest(media_ids) FROM content.posts WHERE id = ANY($1))", pq.Array(ids))
		return nil

	} else if entity == redis.EntityMessage {
		// Soft Delete du Message (visibility = false)
		query = fmt.Sprintf("UPDATE %s SET visibility = false, updated_at = util.now_ms() WHERE id = ANY($1)", mapper.TableName())
		if _, errExec := postgres.PostgresDB.ExecContext(ctx, query, pq.Array(ids)); errExec != nil {
			numan_log.Error(ctx).Err(errExec).Msg("Échec Soft Delete Message")
			return numan_error.NewInternal()
		}

		// Cascade : Purge des réactions associées pour soulager la base
		_, _ = postgres.PostgresDB.ExecContext(ctx, "DELETE FROM messaging.message_reactions WHERE message_id = ANY($1)", pq.Array(ids))
		return nil

	} else if entity == redis.EntityComment {
		// Soft Delete du Commentaire (visibility = -1)
		query = fmt.Sprintf("UPDATE %s SET visibility = -1 WHERE id = ANY($1)", mapper.TableName())

	} else {
		// Hard Delete pour le reste (Utilisateurs, Sessions, etc.)
		query = fmt.Sprintf("DELETE FROM %s WHERE id = ANY($1)", mapper.TableName())
	}

	if _, errExec := postgres.PostgresDB.ExecContext(ctx, query, pq.Array(ids)); errExec != nil {
		numan_log.Error(ctx).Err(errExec).Msg("Échec de la requête de Suppression/Mise à jour Finale")
		return numan_error.NewInternal()
	}

	return nil
}

// updateCountersPostgres regroupe les événements (Deltas) en mémoire avant d'appeler
// les procédures stockées optimisées dans PostgreSQL, minimisant ainsi les conflits transactionnels.
func updateCountersPostgres(ctx context.Context, events []redis.AsyncEvent) {
	likeDeltas := make(map[int64]int)
	commentDeltas := make(map[int64]int)
	viewDeltas := make(map[int64]int)
	commentLikeDeltas := make(map[int64]int)
	reportDeltas := make(map[int64]int)
	telemetryDwellSum := make(map[int64]float64)
	telemetryDwellSq := make(map[int64]float64)
	telemetryClicks := make(map[int64]int)
	messageReactionDeltas := make(map[int64]map[string]int)

	// A. Agréggation des Métriques
	for _, e := range events {
		delta := 0
		if e.Action == redis.ActionCreate {
			delta = 1
		} else if e.Action == redis.ActionDelete {
			delta = -1
		}

		jsonBytes, err := json.Marshal(e.Payload)
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Worker Postgres : Échec sérialisation pour compteurs")
			continue
		}

		if e.Type == redis.EntityLike {
			if delta == 0 {
				continue
			}
			var p struct {
				TargetType int   `json:"target_type"`
				TargetID   int64 `json:"target_id"`
			}
			if err := json.Unmarshal(jsonBytes, &p); err == nil {
				if p.TargetType == 0 && p.TargetID != 0 {
					likeDeltas[p.TargetID] += delta
				} else if p.TargetType == 1 && p.TargetID != 0 {
					commentLikeDeltas[p.TargetID] += delta
				}
			}
		} else if e.Type == redis.EntityComment {
			if delta == 0 {
				continue
			}
			var p struct {
				PostID int64 `json:"post_id"`
			}
			if err := json.Unmarshal(jsonBytes, &p); err == nil && p.PostID != 0 {
				commentDeltas[p.PostID] += delta
			}
		} else if e.Type == redis.EntityView {
			if e.Action != redis.ActionCreate {
				continue
			}
			var p struct {
				TargetID int64 `json:"target_id"`
				Count    int   `json:"count"`
			}
			if err := json.Unmarshal(jsonBytes, &p); err == nil && p.TargetID != 0 {
				vDelta := 1
				if p.Count != 0 {
					vDelta = p.Count
				}
				if vDelta != 0 {
					viewDeltas[p.TargetID] += vDelta
				}
			}
		} else if e.Type == redis.EntityReport {
			if delta == 0 {
				continue
			}
			var r struct {
				TargetType int     `json:"target_type"`
				TargetIDs  []int64 `json:"target_ids"`
			}
			if err := json.Unmarshal(jsonBytes, &r); err == nil && r.TargetType == 1 {
				for _, id := range r.TargetIDs {
					reportDeltas[id] += delta
				}
			}
		} else if e.Type == redis.EntityTelemetry {
			if e.Action != redis.ActionCreate {
				continue
			}
			var t struct {
				PostID       int64 `json:"post_id"`
				DwellTimeMs  int   `json:"dwell_time_ms"`
				IsClicked    bool  `json:"is_clicked"`
				DeepScroll   bool  `json:"deep_scroll"`
				ProfileVisit bool  `json:"profile_visit"`
			}
			if err := json.Unmarshal(jsonBytes, &t); err == nil && t.PostID != 0 {
				viewDeltas[t.PostID] += 1 // Une télémétrie correspond toujours à une vue

				dwellVal := float64(t.DwellTimeMs)
				telemetryDwellSum[t.PostID] += dwellVal
				telemetryDwellSq[t.PostID] += dwellVal * dwellVal

				clicks := 0
				if t.IsClicked {
					clicks++
				}
				if t.DeepScroll {
					clicks++
				}
				if t.ProfileVisit {
					clicks++
				}

				telemetryClicks[t.PostID] += clicks
			}
		} else if e.Type == redis.EntityMessage && e.Action == redis.ActionBuild {
			var p struct {
				MessageID int64          `json:"message_id"`
				Deltas    map[string]int `json:"deltas"`
			}
			if err := json.Unmarshal(jsonBytes, &p); err == nil && p.MessageID != 0 {
				if messageReactionDeltas[p.MessageID] == nil {
					messageReactionDeltas[p.MessageID] = make(map[string]int)
				}
				for emoji, d := range p.Deltas {
					messageReactionDeltas[p.MessageID][emoji] += d
				}
			}
		}
	}

	// B. Exécution vers Procédures Stockées PostgreSQL
	for id, delta := range likeDeltas {
		if delta == 0 {
			continue
		}
		if _, err := postgres.PostgresDB.ExecContext(ctx, "SELECT content.func_increment_post_like($1, $2)", id, delta); err != nil {
			numan_log.Error(ctx).Err(err).Int64("post_id", id).Msg("Échec exécution fonction func_increment_post_like")
		}
	}
	for id, delta := range commentDeltas {
		if delta == 0 {
			continue
		}
		if _, err := postgres.PostgresDB.ExecContext(ctx, "SELECT content.func_increment_post_comment($1, $2)", id, delta); err != nil {
			numan_log.Error(ctx).Err(err).Int64("post_id", id).Msg("Échec exécution fonction func_increment_post_comment")
		}
	}
	for id, delta := range viewDeltas {
		if delta == 0 {
			continue
		}
		if _, err := postgres.PostgresDB.ExecContext(ctx, "SELECT content.func_increment_post_view($1, $2)", id, delta); err != nil {
			numan_log.Error(ctx).Err(err).Int64("post_id", id).Msg("Échec exécution fonction func_increment_post_view")
		}
	}
	for id, delta := range commentLikeDeltas {
		if delta == 0 {
			continue
		}
		if _, err := postgres.PostgresDB.ExecContext(ctx, "SELECT content.func_increment_comment_metrics($1, $2)", id, delta); err != nil {
			numan_log.Error(ctx).Err(err).Int64("comment_id", id).Msg("Échec exécution fonction func_increment_comment_metrics")
		}
	}
	for id, delta := range reportDeltas {
		if delta == 0 {
			continue
		}
		if _, err := postgres.PostgresDB.ExecContext(ctx, "SELECT content.func_increment_post_report($1, $2)", id, delta); err != nil {
			numan_log.Error(ctx).Err(err).Int64("post_id", id).Msg("Échec exécution fonction func_increment_post_report")
		}
	}
	for id, sum := range telemetryDwellSum {
		if _, err := postgres.PostgresDB.ExecContext(ctx, "SELECT content.func_increment_post_telemetry($1, $2, $3, $4)", id, sum, telemetryDwellSq[id], telemetryClicks[id]); err != nil {
			numan_log.Error(ctx).Err(err).Int64("post_id", id).Msg("Échec exécution fonction func_increment_post_telemetry")
		}
	}
	for msgID, deltas := range messageReactionDeltas {
		validDeltas := make(map[string]int)
		for emoji, d := range deltas {
			if d != 0 {
				validDeltas[emoji] = d
			}
		}

		if len(validDeltas) > 0 {
			deltasJSON, err := json.Marshal(validDeltas)
			if err != nil {
				continue
			}
			if _, errExec := postgres.PostgresDB.ExecContext(ctx, "SELECT messaging.func_apply_reaction_delta($1, $2::jsonb)", msgID, string(deltasJSON)); errExec != nil {
				numan_log.Error(ctx).Err(errExec).Int64("message_id", msgID).Msg("Échec exécution fonction func_apply_reaction_delta")
			}
		}
	}
}
