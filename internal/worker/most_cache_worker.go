package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/algorithm_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/vmihailenco/msgpack/v5"
	"go.mongodb.org/mongo-driver/bson"
)

// Expression régulière pré-compilée pour l'extraction de hashtags indirects dans les commentaires.
// Capture les lettres, chiffres et underscores après un '#'
var hashtagRegex = regexp.MustCompile(`#([\p{L}\p{N}_]+)`)

// ############################################################################
// # WORKER : MOST CACHE (INTERCEPTEUR ALGORITHMIQUE L1)
// ############################################################################

// updateMostCache intercepte le flux d'événements asynchrones (via le Worker Manager)
// pour alimenter intelligemment les structures de classement en RAM (ZSETs algorithmiques).
func updateMostCache(ctx context.Context, events []redis.AsyncEvent) {
	for _, e := range events {

		// ── ÉTAPE 1 : CRÉATION OU MISE À JOUR D'UN POST ─────────────────────────
		if e.Type == redis.EntityPost && (e.Action == redis.ActionCreate || e.Action == redis.ActionUpdate) {
			jsonBytes, errMarshal := json.Marshal(e.Payload)
			if errMarshal == nil {
				var post post_models.PostPayload
				if errUnmarshal := json.Unmarshal(jsonBytes, &post); errUnmarshal == nil {
					// L'algorithme a besoin du post pour initialiser son score et son vecteur ML
					cache_service.UpdatePostRecommendationScore(ctx, post)
					algorithm_service.StoreContentVector(ctx, post)
				}
			}
			continue
		}

		// ── ÉTAPE 2 : SUPPRESSION D'UN POST (NETTOYAGE DES ZSETS TENDANCES) ────
		if e.Type == redis.EntityPost && e.Action == redis.ActionDelete {
			jsonBytes, errMarshal := json.Marshal(e.Payload)
			if errMarshal == nil {
				var post post_models.PostPayload
				if errUnmarshal := json.Unmarshal(jsonBytes, &post); errUnmarshal == nil {
					pipe := redis.TrendGlobalDaily.Pipeline()
					dateKey := domain.MillisToTime(post.CreatedAt).UTC().Format("20060102")

					// 1. Retrait du classement global mondial
					pipe.ZRem(ctx, redis.TrendGlobalDaily.Key(dateKey), post.ID)

					// 2. Désérialisation et retrait des tags directs
					for _, tag := range post.Hashtags {
						item := lite_models.TagPostItem{PostID: post.ID, IsIndirect: false}
						b, _ := msgpack.Marshal(item)
						pipe.ZRem(ctx, redis.TrendTagDaily.Key(fmt.Sprintf("%s:daily", tag)), string(b))
					}

					// 3. Désérialisation et retrait des tags indirects
					for _, tag := range post.IndirectHashtags {
						item := lite_models.TagPostItem{PostID: post.ID, IsIndirect: true}
						b, _ := msgpack.Marshal(item)
						pipe.ZRem(ctx, redis.TrendTagDaily.Key(fmt.Sprintf("%s:daily", tag)), string(b))
					}

					_, errPipe := pipe.Exec(ctx)
					if errPipe != nil {
						numan_log.Error(ctx).Err(errPipe).Int64("post_id", post.ID).Msg("Most Cache Worker : Échec nettoyage ZSET Trends")
					}
				}
			}
			continue
		}

		// ── ÉTAPE 3 : INTERACTIONS (LIKES, VUES, COMMENTAIRES, TÉLÉMÉTRIE) ──────
		if e.Type == redis.EntityLike || e.Type == redis.EntityView || e.Type == redis.EntityComment || e.Type == redis.EntityTelemetry {
			jsonBytes, errMarshal := json.Marshal(e.Payload)
			if errMarshal != nil {
				continue
			}

			var targetPostID int64
			var callerID int64

			// A. Extraction ciblée de la clé d'entité en fonction du type de l'événement
			if e.Type == redis.EntityLike || e.Type == redis.EntityView {
				var payload struct {
					PostID     int64 `json:"post_id"`
					TargetID   int64 `json:"target_id"`
					TargetType int   `json:"target_type"`
					UserID     int64 `json:"user_id"`
				}
				if json.Unmarshal(jsonBytes, &payload) == nil {
					if payload.TargetType != 0 {
						continue // 🛡️ BOUCLIER : Les interactions sur les commentaires n'impactent pas l'algorithme du Post
					}
					targetPostID = payload.TargetID
					if payload.PostID != 0 {
						targetPostID = payload.PostID // Privilégie PostID si explicite
					}
					callerID = payload.UserID
				}
			} else if e.Type == redis.EntityComment || e.Type == redis.EntityTelemetry {
				var payload struct {
					PostID int64 `json:"post_id"`
					UserID int64 `json:"user_id"` // Utilisateur émetteur de l'événement
				}
				if json.Unmarshal(jsonBytes, &payload) == nil {
					targetPostID = payload.PostID
					callerID = payload.UserID
				}
			}

			// B. Routage Algorithmique
			if targetPostID != 0 {
				p, errFallback := getPostWithFallback(ctx, targetPostID)
				if errFallback == nil && p.Visibility != -1 {

					// Vérification des droits : L'événement n'influence l'algorithme que s'il est légitime
					if callerID != 0 && p.UserID != callerID {
						relationState := cache_service.RelationValue(ctx, p.UserID, callerID)
						if relationState == -1 || (p.Visibility == 1 && relationState < 1) || (p.Visibility == 2 && relationState != 2) || (p.Visibility != 0 && p.Visibility != 1 && p.Visibility != 2) {
							continue // Rejet de l'événement (Accès bloqué ou droits insuffisants ou visibilité non supportée)
						}
					}

					// Mise à jour scalaire et vectorielle
					if e.Type == redis.EntityLike {
						cache_service.EvaluatePostAfterLike(ctx, p)
					} else if e.Type == redis.EntityTelemetry {
						// La télémétrie alimente le leaderboard des vues ET le score global
						cache_service.EvaluatePostAfterView(ctx, p)
						algorithm_service.UpdatePostEngagementVector(ctx, p)
					} else {
						// Pour un commentaire, on met juste à jour le score global
						cache_service.UpdatePostRecommendationScore(ctx, p)
					}

					// Le Vecteur ML doit toujours refléter l'état actuel de la base
					algorithm_service.UpdatePostEngagementVector(ctx, p)
				}
			}
		}

		// ── ÉTAPE 4 : CRÉATION DE COMMENTAIRE (EXTRACTION DE TAGS INDIRECTS) ────
		if e.Type == redis.EntityComment && e.Action == redis.ActionCreate {
			jsonBytes, errMarshal := json.Marshal(e.Payload)
			if errMarshal == nil {
				var commentEvent struct {
					PostID  int64  `json:"post_id"`
					Content string `json:"content"`
				}
				if errUnmarshal := json.Unmarshal(jsonBytes, &commentEvent); errUnmarshal == nil && commentEvent.PostID != 0 && commentEvent.Content != "" {

					// Capture des hashtags dans le texte du commentaire
					matches := hashtagRegex.FindAllStringSubmatch(commentEvent.Content, -1)

					if len(matches) > 0 {
						for _, match := range matches {
							tag := match[1] // Le mot brut sans le '#'

							// 1. Injection L3 (Garantit la source de vérité SQL)
							_ = postgres.FuncAddIndirectTagToPost(ctx, commentEvent.PostID, tag)

							// 2. Injection L2 (Le $addToSet de Mongo empêche les doublons)
							if mongo.Posts != nil {
								_, _ = mongo.Posts.DB.Collection(mongo.Posts.Name).UpdateOne(
									ctx,
									bson.M{"id": commentEvent.PostID},
									bson.M{"$addToSet": bson.M{"indirect_hashtags": tag}},
								)
							}

							// 3. Signalement L1 (Nécessaire pour le Canonicalizer de nuit)
							_ = redis.Tags.SAdd(ctx, "active", tag)
						}

						// 4. Éviction du Post en RAM L1
						// Cette étape force le prochain `getPostWithFallback` (juste en dessous) à
						// recharger le post depuis L2/L3 avec son nouveau lot de tags indirects.
						_ = object_cache_service.DeletePostFromObjectCache(ctx, commentEvent.PostID)
					}

					// Recalcul du score algorithmique du post fraîchement hydraté
					p, errFallback := getPostWithFallback(ctx, commentEvent.PostID)
					if errFallback == nil && p.Visibility != -1 {
						cache_service.UpdatePostRecommendationScore(ctx, p)
					}
				}
			}
		}
	}
}

// ============================================================================
// UTILITAIRES : HYDRATATION CASCADÉE L1 -> L2 -> L3
// ============================================================================

// getPostWithFallback récupère un post avec auto-guérison du cache L1/L2.
func getPostWithFallback(ctx context.Context, postID int64) (post_models.PostPayload, error) {
	var p post_models.PostPayload

	// TENTATIVE L1 (RAM Object Cache)
	if postL1, errL1 := object_cache_service.GetPostFromObjectCache(ctx, postID); errL1 == nil {
		return postL1, nil
	}

	// TENTATIVE L2 (MongoDB Warm Storage)
	filter := map[string]any{"id": postID}
	docs, errL2 := mongo.Posts.GetPaginated(filter, nil, 0, 1)
	if errL2 == nil && len(docs) > 0 {
		if errStruct := pkg.ToStruct(docs[0], &p); errStruct == nil {
			// Auto-guérison : Remonte la donnée L2 vers le L1
			_ = object_cache_service.SetPostInObjectCache(ctx, p)
			return p, nil
		}
	}

	// FALLBACK L3 (PostgreSQL Cold Storage)
	posts, errL3 := postgres.FuncLoadPosts(ctx, []int64{postID}, 1, 0)
	if errL3 == nil && len(posts) > 0 {
		p = posts[0]
		// Auto-guérison : Remonte la donnée L3 vers le L1
		_ = object_cache_service.SetPostInObjectCache(ctx, p)

		// Auto-guérison L3 -> L2 (Asynchrone via Queue Redis)
		go func(payload post_models.PostPayload) {
			bgCtx := context.Background()
			_ = redis.EnqueueDB(bgCtx, payload.ID, payload.UserID, redis.EntityPost, redis.ActionUpdate, payload, redis.TargetMongo)
		}(p)

		return p, nil
	}

	// Échec total : le post n'existe dans aucune couche. Utilisation du code d'erreur standard.
	return post_models.PostPayload{}, numan_error.NewNotFound(numan_error.CodeNotFound, "Publication introuvable.", nil)
}
