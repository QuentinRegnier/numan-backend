package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/post_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/mongo"
	"github.com/QuentinRegnier/numan-backend/internal/repository/postgres"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service"
	"github.com/QuentinRegnier/numan-backend/internal/service/algorithm_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service/object_cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
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

		// ÉTAPE 1 : CRÉATION OU MISE À JOUR D'UN POST
		if e.Type == redis.EntityPost && (e.Action == redis.ActionCreate || e.Action == redis.ActionUpdate) {
			jsonBytes, errMarshal := json.Marshal(e.Payload)
			if errMarshal == nil {
				var post post_models.PostPayload
				if errUnmarshal := json.Unmarshal(jsonBytes, &post); errUnmarshal == nil {
					// P0 BOUCLIER D'ISOLATION DE VISIBILITÉ :
					// Seuls les posts strictement publics (Visibility == 0 / PostVisibilityAll)
					// peuvent résider dans les classements et tendances publics du Magasin.
					if post.Visibility != variables.PostVisibilityAll {
						evictPostFromPublicTrends(ctx, post)
						if post.Visibility >= 0 && post.Visibility <= 2 {
							algorithm_service.StoreContentVector(ctx, post)
						}
						continue
					}
					// L'algorithme a besoin du post pour initialiser son score et son vecteur ML
					cache_service.UpdatePostRecommendationScore(ctx, post)
					algorithm_service.StoreContentVector(ctx, post)
				}
			}
			continue
		}

		// ÉTAPE 2 : SUPPRESSION D'UN POST (NETTOYAGE INTÉGRAL DES ZSETS TENDANCES)
		if e.Type == redis.EntityPost && e.Action == redis.ActionDelete {
			jsonBytes, errMarshal := json.Marshal(e.Payload)
			if errMarshal == nil {
				var post post_models.PostPayload
				if errUnmarshal := json.Unmarshal(jsonBytes, &post); errUnmarshal == nil {
					evictPostFromPublicTrends(ctx, post)
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
			var commentContent string

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
					PostID  int64  `json:"post_id"`
					UserID  int64  `json:"user_id"` // Utilisateur émetteur de l'événement
					Content string `json:"content"`
				}
				if json.Unmarshal(jsonBytes, &payload) == nil {
					targetPostID = payload.PostID
					callerID = payload.UserID
					commentContent = payload.Content
				}
			}

			// B. Routage Algorithmique
			if targetPostID != 0 {
				p, errFallback := getPostWithFallback(ctx, targetPostID)
				if errFallback == nil && p.Visibility != variables.PostVisibilityDeleted {

					// Vérification des droits : L'événement n'influence l'algorithme que s'il est légitime
					if callerID != 0 && p.UserID != callerID {
						relationState := cache_service.RelationValue(ctx, p.UserID, callerID)
						if relationState == variables.RelationStateBlocked ||
							(p.Visibility == variables.PostVisibilitySubcriber && relationState < 1) ||
							(p.Visibility == variables.PostVisibilityFriend && relationState != 2) ||
							(p.Visibility != variables.PostVisibilityAll && p.Visibility != variables.PostVisibilitySubcriber && p.Visibility != variables.PostVisibilityFriend) {
							continue // Rejet de l'événement (Accès bloqué ou droits insuffisants ou visibilité non supportée)
						}
					}

					// C. Extraction et validation par consensus des tags indirects (Création de commentaire)
					if e.Type == redis.EntityComment && e.Action == redis.ActionCreate && commentContent != "" {
						matches := hashtagRegex.FindAllStringSubmatch(commentContent, -1)
						if len(matches) > 0 {
							promotedAny := false
							for _, match := range matches {
								rawTag := match[1]
								normTag := service.NormalizeHashtag(ctx, rawTag)
								if normTag == "" {
									continue
								}
								// Anti-abus : nécessite un consensus de variables.IndirectTagConsensusMinUsers commentateurs distincts
								if shouldPromoteIndirectTag(ctx, p.ID, normTag, callerID) {
									canonSlug, ok := service.GetTagFromKeywordWithLang(ctx, normTag, p.Lang, callerID)
									if !ok || canonSlug == "" {
										canonSlug = normTag
									}

									// 1. Injection L3 (Source de vérité SQL)
									_ = postgres.FuncAddIndirectTagToPost(ctx, p.ID, canonSlug)

									// 2. Injection L2 (MongoDB $addToSet)
									if mongo.Posts != nil && mongo.Posts.DB != nil {
										_, _ = mongo.Posts.DB.Collection(mongo.Posts.Name).UpdateOne(
											ctx,
											bson.M{"id": p.ID},
											bson.M{"$addToSet": bson.M{"indirect_hashtags": canonSlug}},
										)
									}

									if !containsString(p.IndirectHashtags, canonSlug) {
										p.IndirectHashtags = append(p.IndirectHashtags, canonSlug)
									}
									promotedAny = true
								}
							}
							if promotedAny {
								_ = object_cache_service.DeletePostFromObjectCache(ctx, p.ID)
								if refreshedPost, errRefresh := getPostWithFallback(ctx, p.ID); errRefresh == nil {
									p = refreshedPost
								}
							}
						}
					}

					// D. Mise à jour scalaire et vectorielle unique (sans double appel)
					if e.Type == redis.EntityLike {
						cache_service.EvaluatePostAfterLike(ctx, p)
					} else if e.Type == redis.EntityTelemetry || e.Type == redis.EntityView {
						// La télémétrie / vue alimente le leaderboard des vues ET le score global
						cache_service.EvaluatePostAfterView(ctx, p)
					} else {
						// Pour un commentaire, on met à jour le score global une seule fois
						cache_service.UpdatePostRecommendationScore(ctx, p)
					}

					// Le Vecteur ML doit toujours refléter l'état actuel de la base (appelé une seule fois par événement)
					algorithm_service.UpdatePostEngagementVector(ctx, p)
				}
			}
		}
	}
}

// shouldPromoteIndirectTag vérifie si le hashtag indirect proposé dans un commentaire
// a atteint le seuil de consensus multi-utilisateurs (variables.IndirectTagConsensusMinUsers).
func shouldPromoteIndirectTag(ctx context.Context, postID int64, normTag string, commenterID int64) bool {
	minUsers := variables.IndirectTagConsensusMinUsers
	if minUsers <= 1 || !redis.IndirectTagConsensus.IsReady() {
		return true
	}
	memberID := commenterID
	if memberID <= 0 {
		// Si l'identifiant du commentateur n'est pas fourni dans l'événement, on autorise par rétrocompatibilité
		return true
	}
	subKey := fmt.Sprintf("%d:%s", postID, normTag)
	_ = redis.IndirectTagConsensus.SAdd(ctx, subKey, memberID)
	_ = redis.IndirectTagConsensus.RefreshTTL(ctx, subKey)
	count, err := redis.IndirectTagConsensus.SCard(ctx, subKey)
	if err != nil {
		return false
	}
	return int(count) >= minUsers
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
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

func evictPostFromPublicTrends(ctx context.Context, post post_models.PostPayload) {
	cache_service.EvictPostFromPublicTrends(ctx, post)
}
