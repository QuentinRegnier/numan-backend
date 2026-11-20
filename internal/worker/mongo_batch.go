package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/relation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/saved_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/telemetry_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/user_recommendation_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"go.mongodb.org/mongo-driver/bson"
	libMongo "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ############################################################################
// # WORKER BATCH : MONGODB (WARM STORAGE L2)
// ############################################################################

// flushMongo groupe les événements par entité, applique le TTL glissant et
// exécute les opérations d'écriture en masse (BulkWrite) sur MongoDB.
func flushMongo(ctx context.Context, events []redis.AsyncEvent) {
	// ── ÉTAPE 1 : GROUPEMENT DES ÉVÉNEMENTS PAR TYPE D'ENTITÉ ───────────────
	grouped := make(map[redis.EntityType][]redis.AsyncEvent)
	for _, e := range events {
		grouped[e.Type] = append(grouped[e.Type], e)
	}

	// Référence temporelle universelle pour rafraîchir le Sliding TTL (Éviction L2)
	now := domain.NowTime()

	// ── ÉTAPE 2 : TRAITEMENT DE CHAQUE COLLECTION BDD ───────────────────────
	for entity, evts := range grouped {
		if entity == redis.EntityUserRecoSkip {
			skips := make([]user_recommendation_models.UserRecoSkipPayload, 0, len(evts))
			for _, e := range evts {
				if e.Action != redis.ActionCreate && e.Action != redis.ActionUpdate {
					continue
				}
				var skip user_recommendation_models.UserRecoSkipPayload
				jsonBytes, err := json.Marshal(e.Payload)
				if err != nil {
					continue
				}
				if err := json.Unmarshal(jsonBytes, &skip); err != nil {
					continue
				}
				skips = append(skips, skip)
			}
			if len(skips) > 0 {
				if err := mongo.BulkUpsertUserRecoSkips(ctx, skips); err != nil {
					numan_log.Error(ctx).Err(err).Msg("Worker Mongo : Échec de la persistance groupée des skips de recommandation")
				}
			}
			continue
		}

		var c *mongo.Collection

		switch entity {
		case redis.EntityUser:
			c = mongo.Users
		case redis.EntityUserSettings:
			c = mongo.UserSettings
		case redis.EntitySession:
			c = mongo.Sessions
		case redis.EntityRelation:
			c = mongo.Relations
		case redis.EntityPost:
			c = mongo.Posts
		case redis.EntityComment:
			c = mongo.Comments
		case redis.EntityLike:
			c = mongo.Likes
		case redis.EntityMedia:
			c = mongo.Media
		case redis.EntityConversation:
			c = mongo.Conversations
		case redis.EntityMembers:
			c = mongo.Members
		case redis.EntityMessage:
			c = mongo.Messages
		case redis.EntityMessageReaction:
			c = mongo.MessageReactions
		case redis.EntitySaved:
			c = mongo.Saved
		case redis.EntityNotification:
			c = mongo.Notifications
		case redis.EntityPasskey:
			c = mongo.Passkeys
		case redis.EntityArticle:
			c = mongo.Articles
		default:
			numan_log.Error(ctx).Interface("entity", entity).Msg("Worker Mongo : Entité non reconnue ou sans collection définie")
			continue
		}

		if c == nil || c.DB == nil {
			numan_log.Error(ctx).Interface("entity", entity).Msg("Worker Mongo : La collection cible est nil")
			continue
		}

		coll := c.DB.Collection(c.Name)
		var models []libMongo.WriteModel

		if entity == redis.EntityArticle {
			models = buildArticleMongoWriteModels(evts, now)
			if len(models) > 0 {
				opts := options.BulkWrite().SetOrdered(variables.MongoBulkWriteOrdered)
				if _, err := coll.BulkWrite(ctx, models, opts); err != nil {
					numan_log.Error(ctx).Err(err).Str("collection", c.Name).Msg("Worker Mongo : Échec de l'opération BulkWrite Articles")
				}
			}
			continue
		}

		// ── ÉTAPE 3 : CONSTRUCTION DES REQUÊTES (WRITE MODELS) ──────────────
		for _, e := range evts {
			switch e.Action {

			// --- INSERTION ---
			case redis.ActionCreate:
				var doc bson.M
				dataBytes, _ := bson.Marshal(e.Payload)
				_ = bson.Unmarshal(dataBytes, &doc)

				// Injection automatique du rafraîchissement TTL
				doc["last_use"] = now

				if entity == redis.EntityMessageReaction {
					// UPSERT spécifique pour les réactions (Clé composite)
					models = append(models, libMongo.NewUpdateOneModel().
						SetFilter(bson.M{
							"message_id": doc["message_id"],
							"user_id":    doc["user_id"],
						}).
						SetUpdate(bson.M{"$set": doc}).
						SetUpsert(true))
				} else {
					models = append(models, libMongo.NewInsertOneModel().SetDocument(doc))
				}

			// --- MISE À JOUR ---
			case redis.ActionUpdate:
				if entity == redis.EntityRelation {
					// Relation Sociale (Clé composite)
					var rel relation_models.RelationPayload
					jsonBytes, _ := json.Marshal(e.Payload)
					_ = json.Unmarshal(jsonBytes, &rel)

					models = append(models, libMongo.NewUpdateOneModel().
						SetFilter(bson.M{"primary_id": rel.PrimaryID, "secondary_id": rel.SecondaryID}).
						SetUpdate(bson.M{"$set": bson.M{
							"state":      rel.State,
							"updated_at": rel.UpdatedAt,
							"last_use":   now,
						}}))
				} else {
					// Entité Standard (Par ID Snowflake)
					var doc bson.M
					dataBytes, _ := bson.Marshal(e.Payload)
					_ = bson.Unmarshal(dataBytes, &doc)
					doc["last_use"] = now

					models = append(models, libMongo.NewUpdateOneModel().
						SetFilter(bson.M{"id": e.ID}).
						SetUpdate(bson.M{"$set": doc}))
				}

			// --- SUPPRESSION ---
			case redis.ActionDelete:
				if entity == redis.EntityPost {
					var post post_models.PostPayload
					jsonBytes, _ := json.Marshal(e.Payload)
					_ = json.Unmarshal(jsonBytes, &post)

					// A. Soft Delete du Post
					models = append(models, libMongo.NewUpdateOneModel().
						SetFilter(bson.M{"id": e.ID}).
						SetUpdate(bson.M{"$set": bson.M{"visibility": -1, "last_use": now}}))

					// B. Cascades Asynchrones NoSQL (Hard Deletes des enfants)
					if mongo.Comments != nil {
						_, _ = mongo.Comments.DB.Collection(mongo.Comments.Name).DeleteMany(ctx, bson.M{"post_id": e.ID})
					}
					if mongo.Likes != nil {
						_, _ = mongo.Likes.DB.Collection(mongo.Likes.Name).DeleteMany(ctx, bson.M{"target_id": e.ID, "target_type": 0})
					}
					if mongo.Saved != nil {
						_, _ = mongo.Saved.DB.Collection(mongo.Saved.Name).DeleteMany(ctx, bson.M{"post_id": e.ID})
					}
					if mongo.Media != nil && len(post.MediaIDs) > 0 {
						models = append(models, libMongo.NewDeleteManyModel().
							SetFilter(bson.M{"id": bson.M{"$in": post.MediaIDs}}))
					}

				} else if entity == redis.EntityMessage {
					// Soft Delete du Message
					models = append(models, libMongo.NewUpdateOneModel().
						SetFilter(bson.M{"id": e.ID}).
						SetUpdate(bson.M{"$set": bson.M{"visibility": false, "last_use": now}}))

					// Cascade Asynchrone (Réactions)
					if mongo.MessageReactions != nil {
						_, _ = mongo.MessageReactions.DB.Collection(mongo.MessageReactions.Name).DeleteMany(ctx, bson.M{"message_id": e.ID})
					}

				} else if entity == redis.EntityComment {
					// Soft Delete du Commentaire
					models = append(models, libMongo.NewUpdateOneModel().
						SetFilter(bson.M{"id": e.ID}).
						SetUpdate(bson.M{"$set": bson.M{"visibility": -1, "last_use": now}}))

				} else if entity == redis.EntityRelation {
					// Hard Delete (Relation)
					var rel relation_models.RelationPayload
					jsonBytes, _ := json.Marshal(e.Payload)
					_ = json.Unmarshal(jsonBytes, &rel)

					models = append(models, libMongo.NewDeleteOneModel().SetFilter(bson.M{"primary_id": rel.PrimaryID, "secondary_id": rel.SecondaryID}))

				} else if entity == redis.EntityMessageReaction {
					// Hard Delete (Réaction)
					var doc bson.M
					dataBytes, _ := bson.Marshal(e.Payload)
					_ = bson.Unmarshal(dataBytes, &doc)

					models = append(models, libMongo.NewDeleteOneModel().
						SetFilter(bson.M{
							"message_id": doc["message_id"],
							"user_id":    doc["user_id"],
						}))

				} else if entity == redis.EntitySaved {
					// Hard Delete (Favoris)
					var sav saved_models.SavedPayload
					jsonBytes, _ := json.Marshal(e.Payload)
					_ = json.Unmarshal(jsonBytes, &sav)

					models = append(models, libMongo.NewDeleteOneModel().
						SetFilter(bson.M{"user_id": sav.UserID, "post_id": sav.PostID}))

				} else {
					// Hard Delete Standard
					models = append(models, libMongo.NewDeleteOneModel().
						SetFilter(bson.M{"id": e.ID}))
				}
			}
		}

		// ── ÉTAPE 4 : EXÉCUTION DU BULK ─────────────────────────────────────────
		if len(models) > 0 {
			opts := options.BulkWrite().SetOrdered(variables.MongoBulkWriteOrdered)
			_, err := coll.BulkWrite(ctx, models, opts)
			if err != nil {
				numan_log.Error(ctx).Err(err).Str("collection", c.Name).Msg("Worker Mongo : Échec de l'opération BulkWrite")
			}
		}
	}

	// ── ÉTAPE 5 : EXÉCUTION DES INCÉRMENTATIONS DE COMPTEURS ────────────────
	updateCountersMongo(ctx, events)
}

// updateCountersMongo consolide en mémoire les compteurs (Deltas) avant d'appliquer
// les modifications (via $inc) en un seul lot BulkWrite par collection.
func updateCountersMongo(ctx context.Context, events []redis.AsyncEvent) {
	postLikeDeltas := make(map[int64]int)
	commentLikeDeltas := make(map[int64]int)
	commentDeltas := make(map[int64]int)
	viewDeltas := make(map[int64]int)

	// ISOLATION DES MÉTRIQUES DE MODÉRATION
	postReportDeltas := make(map[int64]int)
	commentReportDeltas := make(map[int64]int)

	messageReactionDeltas := make(map[int64]map[string]int)

	telemetryDwellSum := make(map[int64]float64)
	telemetryDwellSq := make(map[int64]float64)
	telemetryClicks := make(map[int64]int)

	for _, e := range events {
		delta := 1
		if e.Action == redis.ActionDelete {
			delta = -1
		}

		jsonBytes, err := json.Marshal(e.Payload)
		if err != nil {
			numan_log.Error(ctx).Err(err).Msg("Worker Mongo : Erreur de désérialisation du payload des compteurs")
			continue
		}

		if e.Type == redis.EntityLike {
			var p struct {
				TargetType int   `json:"target_type"`
				TargetID   int64 `json:"target_id"`
			}
			if err := json.Unmarshal(jsonBytes, &p); err == nil && p.TargetID != 0 {
				if p.TargetType == 0 {
					postLikeDeltas[p.TargetID] += delta
				} else if p.TargetType == 1 {
					commentLikeDeltas[p.TargetID] += delta
				}
			}
		} else if e.Type == redis.EntityComment {
			var p struct {
				PostID int64 `json:"post_id"`
			}
			if err := json.Unmarshal(jsonBytes, &p); err == nil && p.PostID != 0 {
				commentDeltas[p.PostID] += delta
			}
		} else if e.Type == redis.EntityView {
			var p struct {
				TargetID int64 `json:"target_id"`
				Count    int   `json:"count"`
			}
			if err := json.Unmarshal(jsonBytes, &p); err == nil && p.TargetID != 0 {
				if p.Count != 0 {
					delta = p.Count
				}
				viewDeltas[p.TargetID] += delta
			}
		} else if e.Type == redis.EntityReport {
			var r struct {
				TargetType int     `json:"target_type"`
				TargetIDs  []int64 `json:"target_ids"`
			}
			if err := json.Unmarshal(jsonBytes, &r); err == nil {
				for _, id := range r.TargetIDs {
					// ROUTAGE SÉMANTIQUE STRICT
					switch r.TargetType {
					case variables.ReportTargetPost:
						postReportDeltas[id] += delta
					case variables.ReportTargetComment:
						commentReportDeltas[id] += delta
					case variables.ReportTargetMessage, variables.ReportTargetConversation, variables.ReportTargetUser:
						// Évolution future : Prêt pour incrémenter les métriques sur les autres entités
					default:
						numan_log.Warn(ctx).Int("target_type", r.TargetType).Msg("Worker Mongo : Type de signalement non géré pour les compteurs")
					}
				}
			}
		} else if e.Type == redis.EntityTelemetry {
			var t telemetry_models.TelemetryEvent
			if err := json.Unmarshal(jsonBytes, &t); err == nil && t.Validate() == nil {
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

	var postModels []libMongo.WriteModel
	var commentModels []libMongo.WriteModel
	var messageModels []libMongo.WriteModel

	now := domain.NowTime()

	// Application des deltas aggrégés
	for id, delta := range postLikeDeltas {
		if delta != 0 {
			postModels = append(postModels, libMongo.NewUpdateOneModel().SetFilter(bson.M{"id": id}).SetUpdate(bson.M{"$inc": bson.M{"like_count": delta}, "$set": bson.M{"last_use": now}}))
		}
	}
	for id, delta := range commentLikeDeltas {
		if delta != 0 {
			commentModels = append(commentModels, libMongo.NewUpdateOneModel().SetFilter(bson.M{"id": id}).SetUpdate(bson.M{"$inc": bson.M{"like_count": delta, "score": delta}, "$set": bson.M{"last_use": now}}))
		}
	}
	for id, delta := range commentDeltas {
		if delta != 0 {
			postModels = append(postModels, libMongo.NewUpdateOneModel().SetFilter(bson.M{"id": id}).SetUpdate(bson.M{"$inc": bson.M{"comment_count": delta}, "$set": bson.M{"last_use": now}}))
		}
	}
	for id, delta := range viewDeltas {
		if delta != 0 {
			postModels = append(postModels, libMongo.NewUpdateOneModel().SetFilter(bson.M{"id": id}).SetUpdate(bson.M{"$inc": bson.M{"view_count": delta}, "$set": bson.M{"last_use": now}}))
		}
	}

	// APPLICATION SÉPARÉE DES COMPTEURS DE SIGNALEMENT (L2)
	for id, delta := range postReportDeltas {
		if delta != 0 {
			postModels = append(postModels, libMongo.NewUpdateOneModel().SetFilter(bson.M{"id": id}).SetUpdate(bson.M{"$inc": bson.M{"report_count": delta}, "$set": bson.M{"last_use": now}}))
		}
	}
	for id, delta := range commentReportDeltas {
		if delta != 0 {
			commentModels = append(commentModels, libMongo.NewUpdateOneModel().SetFilter(bson.M{"id": id}).SetUpdate(bson.M{"$inc": bson.M{"report_count": delta}, "$set": bson.M{"last_use": now}}))
		}
	}

	for id, sum := range telemetryDwellSum {
		postModels = append(postModels, libMongo.NewUpdateOneModel().SetFilter(bson.M{"id": id}).SetUpdate(bson.M{
			"$inc": bson.M{
				"telemetry_dwell_sum": sum,
				"telemetry_dwell_sq":  telemetryDwellSq[id],
				"telemetry_clicks":    telemetryClicks[id],
			},
			"$set": bson.M{"last_use": now},
		}))
	}
	for msgID, deltas := range messageReactionDeltas {
		incMap := bson.M{}
		for emoji, d := range deltas {
			if d != 0 {
				incMap["attachments.reaction_counts."+emoji] = d
			}
		}
		if len(incMap) > 0 {
			messageModels = append(messageModels, libMongo.NewUpdateOneModel().SetFilter(bson.M{"id": msgID}).SetUpdate(bson.M{"$inc": incMap, "$set": bson.M{"last_use": now}}))
		}
	}

	// Exécution des BulkWrite (Moteur L2)
	opts := options.BulkWrite().SetOrdered(variables.MongoBulkWriteOrdered)
	if len(postModels) > 0 && mongo.Posts != nil {
		if _, err := mongo.Posts.DB.Collection(mongo.Posts.Name).BulkWrite(ctx, postModels, opts); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Worker Mongo : Échec BulkWrite Counters (Posts)")
		}
	}
	if len(commentModels) > 0 && mongo.Comments != nil {
		if _, err := mongo.Comments.DB.Collection(mongo.Comments.Name).BulkWrite(ctx, commentModels, opts); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Worker Mongo : Échec BulkWrite Counters (Comments)")
		}
	}
	if len(messageModels) > 0 && mongo.Messages != nil {
		if _, err := mongo.Messages.DB.Collection(mongo.Messages.Name).BulkWrite(ctx, messageModels, opts); err != nil {
			numan_log.Error(ctx).Err(err).Msg("Worker Mongo : Échec BulkWrite Counters (Messages)")
		}
	}
}

// shouldReplaceArticleState détermine si le candidat (candidate, candidateDelete) doit supplanter l'état courant
// (existing, existingDelete) selon l'ordre monotone de updated_at et la règle d'irréversibilité du soft-delete.
func shouldReplaceArticleState(existing article_models.ArticlePayload, existingDelete bool, candidate article_models.ArticlePayload, candidateDelete bool) bool {
	// Un soft-delete ne peut jamais être écrasé par une création ou mise à jour active ultérieure/hors-ordre
	if existingDelete || existing.Visibility < 0 {
		if !candidateDelete && candidate.Visibility >= 0 {
			return false
		}
	}
	// Une suppression (soft-delete) l'emporte toujours sur un état actif de timestamp inférieur ou égal, ou même supérieur
	if candidateDelete || candidate.Visibility < 0 {
		return true
	}
	return candidate.UpdatedAt >= existing.UpdatedAt
}

// buildArticleMongoWriteModels consolide un lot d'événements d'articles par ID (idempotence + ordre monotone updated_at)
// et génère les opérations BulkWrite MongoDB L2.
func buildArticleMongoWriteModels(evts []redis.AsyncEvent, now time.Time) []libMongo.WriteModel {
	type consolidatedArticle struct {
		payload  article_models.ArticlePayload
		isDelete bool
	}

	dedup := make(map[int64]consolidatedArticle, len(evts))
	order := make([]int64, 0, len(evts))

	for _, e := range evts {
		var art article_models.ArticlePayload
		if e.Payload != nil {
			if jsonBytes, err := json.Marshal(e.Payload); err == nil {
				_ = json.Unmarshal(jsonBytes, &art)
			}
		}
		if art.ID <= 0 {
			art.ID = e.ID
		}
		if art.ID <= 0 {
			continue
		}
		isDel := e.Action == redis.ActionDelete || art.Visibility < 0
		if isDel {
			art.Visibility = variables.ArticleVisibilitySoftDeleted
			if art.UpdatedAt <= 0 {
				art.UpdatedAt = e.Timestamp
			}
		}
		art.Normalize()

		cur, exists := dedup[art.ID]
		if !exists {
			order = append(order, art.ID)
			dedup[art.ID] = consolidatedArticle{payload: art, isDelete: isDel}
			continue
		}
		if shouldReplaceArticleState(cur.payload, cur.isDelete, art, isDel) {
			dedup[art.ID] = consolidatedArticle{payload: art, isDelete: isDel}
		}
	}

	models := make([]libMongo.WriteModel, 0, len(order))
	for _, id := range order {
		c := dedup[id]
		if c.isDelete {
			models = append(models, libMongo.NewUpdateOneModel().
				SetFilter(bson.M{"id": id}).
				SetUpdate(bson.M{"$set": bson.M{
					"visibility": variables.ArticleVisibilitySoftDeleted,
					"updated_at": c.payload.UpdatedAt,
					"last_use":   now,
				}}).
				SetUpsert(true))
			continue
		}

		var doc bson.M
		dataBytes, err := bson.Marshal(c.payload)
		if err != nil {
			continue
		}
		if err := bson.Unmarshal(dataBytes, &doc); err != nil {
			continue
		}
		doc["last_use"] = now

		models = append(models, libMongo.NewUpdateOneModel().
			SetFilter(bson.M{"id": id}).
			SetUpdate(bson.M{"$set": doc}).
			SetUpsert(true))
	}
	return models
}
