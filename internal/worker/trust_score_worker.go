package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	infraPostgres "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"go.mongodb.org/mongo-driver/bson"
	libMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	trustScoreBatchMax       = 200
	trustScoreFlushInterval  = 2 * time.Second
	trustScoreMonthlyCronInt = 24 * time.Hour // Vérifie quotidiennement les comptes éligibles aux 30 jours sans infraction
)

// EnqueueTrustScoreEvent publie de manière non-bloquante un événement de réputation dans Redis.
func EnqueueTrustScoreEvent(ctx context.Context, evt moderation_models.TrustScoreEventPayload) error {
	if evt.OccurredAtMs == 0 {
		evt.OccurredAtMs = domain.NowMillis()
	}
	rawBytes, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return redis.WorkerQueue.RPush(ctx, variables.WorkerQueueTrustScore, string(rawBytes))
}

// IsSteganoBanned vérifie en O(1) RAM (avec fallback L3) si l'identifiant stéganographique est banni.
func IsSteganoBanned(ctx context.Context, userID int64) bool {
	if userID <= 0 || redis.SteganoBanlist == nil {
		return false
	}
	member := fmt.Sprintf("%d", userID)
	isMember, err := redis.SteganoBanlist.SIsMember(ctx, "global", member)
	if err == nil && isMember {
		return true
	}
	if infraPostgres.PostgresDB != nil {
		var banned bool
		errPg := infraPostgres.PostgresDB.QueryRowContext(ctx, "SELECT moderation.func_is_stegano_banned($1)", userID).Scan(&banned)
		if errPg == nil && banned {
			_ = redis.SteganoBanlist.SAdd(ctx, "global", member)
			return true
		}
	}
	return false
}

// ComputeTrustScoreDelta retourne le modificateur mathématique associé à un type d'événement.
func ComputeTrustScoreDelta(eventType string) (delta float64, isInfraction bool, isMajorBan bool, known bool) {
	switch eventType {
	case variables.TrustEventQuarantineApproved:
		return variables.TrustBonusQuarantineApproved, false, false, true
	case variables.TrustEventMonthlyClean:
		return variables.TrustBonusMonthlyClean, false, false, true
	case variables.TrustEventJustifiedReport:
		return variables.TrustBonusJustifiedReport, false, false, true
	case variables.TrustEventAbusiveReport:
		return variables.TrustMalusAbusiveReport, true, false, true
	case variables.TrustEventMinorInfraction:
		return variables.TrustMalusMinorInfraction, true, false, true
	case variables.TrustEventMajorInfraction:
		return -1.00, true, true, true
	default:
		return 0.0, false, false, false
	}
}

// ClampTrustScore borne strictement un flottant entre 0.00 et 1.00 (arrondi à 4 décimales contre la dérive IEEE-754).
func ClampTrustScore(score float64) float64 {
	if score <= variables.TrustScoreMin {
		return variables.TrustScoreMin
	}
	if score >= variables.TrustScoreMax {
		return variables.TrustScoreMax
	}
	return math.Round(score*10000) / 10000
}

func startTrustScoreWorker(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Worker de Réputation (Trust Score Batch Consumer)...")
	go func() {
		flushInt := trustScoreFlushInterval
		if variables.TrustScoreFlushInterval > 0 {
			flushInt = variables.TrustScoreFlushInterval
		}
		ticker := time.NewTicker(flushInt)
		defer ticker.Stop()

		cronInt := trustScoreMonthlyCronInt
		if variables.TrustScoreMonthlyCronTick > 0 {
			cronInt = variables.TrustScoreMonthlyCronTick
		}
		monthlyTicker := time.NewTicker(cronInt)
		defer monthlyTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				numan_log.Info(ctx).Msg("Arrêt gracieux du Worker de Réputation.")
				return
			case <-ticker.C:
				drainAndProcessTrustEvents(ctx)
			case <-monthlyTicker.C:
				processMonthlyCleanBonus(ctx)
			}
		}
	}()
}

// ProcessMonthlyCleanBonus attribue le bonus d'ancienneté mensuelle (+0.05) aux comptes sans infraction depuis 30 jours (§C.13).
func ProcessMonthlyCleanBonus(ctx context.Context) {
	processMonthlyCleanBonus(ctx)
}

func processMonthlyCleanBonus(ctx context.Context) {
	if infraPostgres.PostgresDB == nil || infraRedis.Rdb == nil {
		return
	}
	thirtyDaysAgoMs := domain.NowMillis() - int64(30*24*time.Hour/time.Millisecond)

	rows, err := infraPostgres.PostgresDB.QueryContext(
		ctx,
		`SELECT id FROM auth.users 
		 WHERE (last_infraction_at IS NULL OR last_infraction_at < $1)
		   AND created_at < $1
		   AND banned = false
		   AND trust_score < 1.00
		 LIMIT 100`,
		thirtyDaysAgoMs,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var uID int64
		if errScan := rows.Scan(&uID); errScan == nil && uID > 0 {
			rateKey := fmt.Sprintf("monthly_clean:%d", uID)
			acquired, errLock := redis.WorkerQueue.SetNX(ctx, rateKey, "1", 30*24*time.Hour)
			if errLock == nil && acquired {
				_ = EnqueueTrustScoreEvent(ctx, moderation_models.TrustScoreEventPayload{
					EventID:      domain.NowMillis() + uID,
					UserID:       uID,
					EventType:    variables.TrustTriggerMonthlyClean,
					TargetID:     uID,
					Reason:       "Mois calendaire sans aucune infraction (+0.05)",
					OccurredAtMs: domain.NowMillis(),
				})
			}
		}
	}
}

// DrainAndProcessTrustEvents dépile et traite immédiatement un lot d'événements de réputation (synchrone / testable).
func DrainAndProcessTrustEvents(ctx context.Context) {
	drainAndProcessTrustEvents(ctx)
}

func drainAndProcessTrustEvents(ctx context.Context) {
	if redis.WorkerQueue == nil {
		return
	}
	var batch []moderation_models.TrustScoreEventPayload

	for i := 0; i < trustScoreBatchMax; i++ {
		raw, err := redis.WorkerQueue.LPop(ctx, variables.WorkerQueueTrustScore)
		if err != nil || raw == "" {
			break
		}
		var evt moderation_models.TrustScoreEventPayload
		if errUnmarshal := json.Unmarshal([]byte(raw), &evt); errUnmarshal == nil {
			if len(evt.UserIDs) > 0 {
				// Événement groupé (ex: mod_batch_like) -> expansion par utilisateur
				for _, uID := range evt.UserIDs {
					if uID > 0 {
						batch = append(batch, moderation_models.TrustScoreEventPayload{
							EventID:      evt.EventID,
							UserID:       uID,
							EventType:    evt.EventType,
							TargetID:     evt.TargetID,
							Reason:       evt.Reason,
							OccurredAtMs: evt.OccurredAtMs,
						})
					}
				}
			} else if evt.UserID > 0 {
				batch = append(batch, evt)
			}
		}
	}

	if len(batch) > 0 {
		processTrustScoreBatch(ctx, batch)
	}
}

type aggregatedTrustChange struct {
	UserID                  int64
	CurrentTrust            float64
	NewTrust                float64
	TotalDelta              float64
	QuarantinePostsApproved int
	IsInfraction            bool
	IsMinorInfraction       bool
	IsMajorBan              bool
	BanReason               string
	TargetID                int64
}

func processTrustScoreBatch(ctx context.Context, events []moderation_models.TrustScoreEventPayload) {
	aggregated := make(map[int64]*aggregatedTrustChange)

	for _, evt := range events {
		if evt.UserID <= 0 {
			continue
		}
		agg, exists := aggregated[evt.UserID]
		if !exists {
			currentScore := variables.TrustScoreInitial
			approvedCount := 0
			var liteUser lite_models.UserLiteRequest
			if redis.UsersLite != nil {
				if errL1 := redis.UsersLite.GetObject(ctx, evt.UserID, &liteUser); errL1 == nil && liteUser.ID != 0 {
					currentScore = liteUser.TrustScore
					approvedCount = liteUser.QuarantinePostsApproved
				}
			}
			agg = &aggregatedTrustChange{
				UserID:                  evt.UserID,
				CurrentTrust:            currentScore,
				NewTrust:                currentScore,
				QuarantinePostsApproved: approvedCount,
			}
			aggregated[evt.UserID] = agg
		}

		newTrust, delta, newApproved, spec, ok := moderation_service.ComputeAsymmetricELO(agg.NewTrust, evt.EventType, agg.QuarantinePostsApproved)
		if !ok {
			// Fallback compatibilité
			d, isInf, isMajor, known := ComputeTrustScoreDelta(evt.EventType)
			if !known {
				continue
			}
			newTrust = ClampTrustScore(agg.NewTrust + d)
			delta = newTrust - agg.NewTrust
			spec.IsMajorBan = isMajor
			spec.IsPositive = !isInf
			spec.IsMinorStrike = isInf && !isMajor
		}

		agg.NewTrust = newTrust
		agg.TotalDelta += delta
		agg.QuarantinePostsApproved = newApproved

		if !spec.IsPositive {
			agg.IsInfraction = true
		}
		if spec.IsMinorStrike {
			agg.IsMinorInfraction = true
			agg.TargetID = evt.TargetID
		}
		if spec.IsMajorBan {
			agg.IsMajorBan = true
			agg.BanReason = evt.Reason
			agg.TargetID = evt.TargetID
		} else if evt.Reason != "" && agg.BanReason == "" {
			agg.BanReason = evt.Reason
		}
	}

	if len(aggregated) == 0 {
		return
	}

	var mongoUpdates []mongo.UserTrustScoreUpdate
	var mongoModels []libMongo.WriteModel
	now := time.Now().UTC()
	nowMs := domain.TimeToMillis(now)

	for userID, change := range aggregated {
		var newScore float64 = change.NewTrust
		var isBanned bool = change.IsMajorBan

		// 1. Mise à jour atomique dans PostgreSQL (L3 - Source de vérité)
		if infraPostgres.PostgresDB != nil {
			errPg := infraPostgres.PostgresDB.QueryRowContext(
				ctx,
				"SELECT new_score, is_banned FROM auth.func_apply_trust_score_delta($1, $2, $3, $4, $5)",
				userID, change.TotalDelta, change.IsInfraction, change.IsMajorBan, change.BanReason,
			).Scan(&newScore, &isBanned)
			if errPg != nil {
				numan_log.Error(ctx).Err(errPg).Int64("user_id", userID).Msg("TrustScore Worker : Échec mise à jour PostgreSQL (L3)")
				continue
			}
		} else {
			continue
		}

		newScore = ClampTrustScore(newScore)

		// 2. Mise à jour du Speed Cache Redis (L1 - UserLite)
		var liteUser lite_models.UserLiteRequest
		if redis.UsersLite != nil {
			if errL1 := redis.UsersLite.GetObject(ctx, userID, &liteUser); errL1 == nil && liteUser.ID != 0 {
				liteUser.TrustScore = newScore
				liteUser.QuarantinePostsApproved = change.QuarantinePostsApproved
				_ = redis.UsersLite.SetObject(ctx, userID, liteUser)
			}
		}

		// 3. Synchronisation avec le profil de Télémétrie L1 (ConfidenceScore)
		if redis.TelemetryProfiles != nil {
			var telLite lite_models.TelemetryProfileLite
			if errTel := redis.TelemetryProfiles.GetObject(ctx, userID, &telLite); errTel == nil {
				telLite.ConfidenceScore = newScore
				telLite.TimestampMs = nowMs
				_ = redis.TelemetryProfiles.SetObject(ctx, userID, telLite)
			}
		}

		// 4. Si minor_infraction confirmée, masquer le contenu cible
		if change.IsMinorInfraction && change.TargetID > 0 {
			if post, errPost := object_cache_service.GetPostFromObjectCache(ctx, change.TargetID); errPost == nil {
				post.Visibility = variables.PostVisibilitySoftDeleted
				_ = object_cache_service.SetPostInObjectCache(ctx, post)
			}
		}

		// 5. Préparation des mises à jour MongoDB (L2)
		lastInf := int64(0)
		if change.IsInfraction {
			lastInf = nowMs
		}
		mongoUpdates = append(mongoUpdates, mongo.UserTrustScoreUpdate{
			UserID:                  userID,
			TrustScore:              newScore,
			QuarantinePostsApproved: change.QuarantinePostsApproved,
			LastInfractionAt:        lastInf,
		})

		setFields := bson.M{
			"trust_score":               newScore,
			"banned":                    isBanned,
			"quarantine_posts_approved": change.QuarantinePostsApproved,
			"updated_at":                nowMs,
			"last_use":                  now,
		}
		if change.IsInfraction {
			setFields["last_infraction_at"] = nowMs
		}
		if isBanned && change.BanReason != "" {
			setFields["ban_reason"] = change.BanReason
		}
		mongoModels = append(mongoModels, libMongo.NewUpdateOneModel().
			SetFilter(bson.M{"id": userID}).
			SetUpdate(bson.M{"$set": setFields}))

		// 6. Actions critiques en cas de Hard Ban (Score == 0.00) ou d'Infraction Majeure
		if isBanned || change.IsMajorBan {
			enforceHardBanAndSteganoRevocation(ctx, userID, change)
		}
	}

	// 7. Exécution du BulkWrite MongoDB (L2)
	if len(mongoUpdates) > 0 {
		if errMongo := mongo.BulkUpdateUserTrustScores(ctx, mongoUpdates); errMongo != nil {
			numan_log.Error(ctx).Err(errMongo).Msg("TrustScore Worker : Échec BulkWrite MongoDB (L2)")
		}
	}
}

func enforceHardBanAndSteganoRevocation(ctx context.Context, userID int64, change *aggregatedTrustChange) {
	// A. Bannissement de l'identifiant stéganographique (S3 / Watermark)
	if change.IsMajorBan {
		steganoID := fmt.Sprintf("A:%d", userID)
		if redis.SteganoBanlist != nil {
			_ = redis.SteganoBanlist.SAdd(ctx, "global", fmt.Sprintf("%d", userID))
		}
		if infraPostgres.PostgresDB != nil {
			reason := change.BanReason
			if reason == "" {
				reason = "Infraction majeure confirmée (bannissement stéganographique)"
			}
			_, _ = infraPostgres.PostgresDB.ExecContext(
				ctx,
				"SELECT moderation.func_register_stegano_ban($1, $2, $3, $4)",
				userID, steganoID, reason, change.TargetID,
			)
		}
	}

	// B. Révocation immédiate de toutes les sessions de l'utilisateur (Tombstone L1 + Purge L2/L3)
	sessions, errSess := postgres.FuncLoadAllUserSessions(ctx, userID)
	if errSess == nil {
		for _, sess := range sessions {
			idxKey := fmt.Sprintf("%d:%s", userID, sess.FirebaseInstallationID)
			// Utilisation de la méthode DDD correcte (SetPrimitive) et d'un TTL pour ne pas fuir la RAM
			_ = redis.SessionBlacklist.SetPrimitive(ctx, idxKey, "revoked")
			_ = redis.Expire(ctx, idxKey, 24*time.Hour)
			_ = redis.EnqueueDB(ctx, sess.ID, userID, redis.EntitySession, redis.ActionDelete, sess, redis.TargetAll)
		}
	}

	numan_log.Info(ctx).
		Int64("user_id", userID).
		Bool("major_infraction", change.IsMajorBan).
		Msg("TrustScore Worker : Hard Ban exécuté, sessions révoquées et identifiant stéganographique banni.")
}

func startMonthlyTrustScoreCron(ctx context.Context) {
	numan_log.Info(ctx).Msg("Démarrage du Cron Mensuel de Réputation (+0.02 / 30j sans infraction)...")
	go func() {
		cronInt := trustScoreMonthlyCronInt
		if variables.TrustScoreMonthlyCronTick > 0 {
			cronInt = variables.TrustScoreMonthlyCronTick
		}
		ticker := time.NewTicker(cronInt)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processMonthlyTrustBonus(ctx)
			}
		}
	}()
}

func processMonthlyTrustBonus(ctx context.Context) {
	if infraPostgres.PostgresDB == nil {
		return
	}
	rows, err := infraPostgres.PostgresDB.QueryContext(ctx, "SELECT user_id, new_trust_score FROM auth.func_increment_monthly_trust_scores()")
	if err != nil {
		numan_log.Error(ctx).Err(err).Msg("TrustScore Cron : Échec de l'incrémentation mensuelle sur PostgreSQL")
		return
	}
	defer func() {
		_ = rows.Close()
	}()

	var count int
	now := time.Now().UTC()
	nowMs := domain.TimeToMillis(now)
	var mongoModels []libMongo.WriteModel

	for rows.Next() {
		var uid int64
		var newScore float64
		if errScan := rows.Scan(&uid, &newScore); errScan != nil {
			continue
		}
		newScore = ClampTrustScore(newScore)
		count++

		var liteUser lite_models.UserLiteRequest
		if errL1 := redis.UsersLite.GetObject(ctx, uid, &liteUser); errL1 == nil && liteUser.ID != 0 {
			liteUser.TrustScore = newScore
			_ = redis.UsersLite.SetObject(ctx, uid, liteUser)
		}

		mongoModels = append(mongoModels, libMongo.NewUpdateOneModel().
			SetFilter(bson.M{"id": uid}).
			SetUpdate(bson.M{"$set": bson.M{"trust_score": newScore, "updated_at": nowMs, "last_use": now}}))
	}

	if len(mongoModels) > 0 && mongo.Users != nil && mongo.Users.DB != nil {
		_, _ = mongo.Users.DB.Collection(mongo.Users.Name).BulkWrite(ctx, mongoModels, options.BulkWrite().SetOrdered(false))
	}
	if count > 0 {
		numan_log.Info(ctx).Int("rewarded_users", count).Msg("TrustScore Cron : Bonus d'ancienneté (+0.02) appliqué.")
	}
}
