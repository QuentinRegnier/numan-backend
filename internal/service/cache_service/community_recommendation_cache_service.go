package cache_service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	goredis "github.com/go-redis/redis/v8"
	"github.com/vmihailenco/msgpack/v5"
)

// RankedCommunityEntry représente une entrée précalculée du ZSET
// community:reco:ranked:{lang}.
type RankedCommunityEntry struct {
	ConversationID int64
	Score          float64
}

// ============================================================================
// 1. UTILITAIRES DE CLÉS & DATES UTC
// ============================================================================

// CommunityRecoDateBucket formate un instant en bucket journalier UTC (YYYY-MM-DD).
func CommunityRecoDateBucket(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// CommunityRecoTrailingDates retourne les `days` derniers buckets UTC (aujourd'hui inclus).
func CommunityRecoTrailingDates(now time.Time, days int) []string {
	if days <= 0 {
		days = 7
	}
	base := now.UTC()
	dates := make([]string, days)
	for i := 0; i < days; i++ {
		dates[i] = base.AddDate(0, 0, -i).Format("2006-01-02")
	}
	return dates
}

// CommunityRecoAuthorsKey construit l'identifiant composite "{conv_id}:{YYYY-MM-DD}"
// pour la collection HyperLogLog CommunityRecoAuthors.
func CommunityRecoAuthorsKey(conversationID int64, dateBucket string) string {
	return fmt.Sprintf("%d:%s", conversationID, dateBucket)
}

func isCommunityRecoCacheReady() bool {
	return redis.IsReady() &&
		redis.CommunityRecoRanked != nil &&
		redis.CommunityRecoPins != nil &&
		redis.CommunityRecoMsgCount != nil &&
		redis.CommunityRecoAuthors != nil &&
		redis.CommunityRecoGrowth != nil &&
		redis.UserPublicComms != nil
}

// ============================================================================
// 2. TÉLÉMÉTRIE TEMPS RÉEL (HOOKS MESSAGES & ADHÉSIONS)
// ============================================================================

// RecordCommunityMessageActivity incrémente le compteur journalier de messages et
// enregistre l'auteur dans le HyperLogLog journalier en un seul Pipeline Redis.
func RecordCommunityMessageActivity(ctx context.Context, conversationID int64, senderID int64) error {
	return RecordCommunityMessageActivityAt(ctx, conversationID, senderID, domain.NowTime())
}

// RecordCommunityMessageActivityAt enregistre l'activité message pour un instant UTC explicite.
func RecordCommunityMessageActivityAt(ctx context.Context, conversationID int64, senderID int64, at time.Time) error {
	if conversationID <= 0 || senderID <= 0 {
		return nil
	}
	if !isCommunityRecoCacheReady() {
		return nil
	}

	dateBucket := CommunityRecoDateBucket(at)
	ttlSec := int64(variables.CommunityRecoTelemetryTTL / time.Second)
	if ttlSec <= 0 {
		ttlSec = int64((8 * 24 * time.Hour) / time.Second)
	}

	msgCountKey := redis.CommunityRecoMsgCount.Key(dateBucket)
	authorsKey := redis.CommunityRecoAuthors.Key(CommunityRecoAuthorsKey(conversationID, dateBucket))
	convField := strconv.FormatInt(conversationID, 10)
	senderVal := strconv.FormatInt(senderID, 10)

	pipe := redis.CommunityRecoMsgCount.Pipeline()
	pipe.HIncrBy(ctx, msgCountKey, convField, 1)
	pipe.Do(ctx, "EXPIRE", msgCountKey, ttlSec, "NX")
	pipe.PFAdd(ctx, authorsKey, senderVal)
	pipe.Do(ctx, "EXPIRE", authorsKey, ttlSec, "NX")

	if _, err := pipe.Exec(ctx); err != nil {
		// Repli tolérant si Redis < 7 rejette l'option NX de EXPIRE
		fallbackPipe := redis.CommunityRecoMsgCount.Pipeline()
		fallbackPipe.HIncrBy(ctx, msgCountKey, convField, 1)
		fallbackPipe.Expire(ctx, msgCountKey, variables.CommunityRecoTelemetryTTL)
		fallbackPipe.PFAdd(ctx, authorsKey, senderVal)
		fallbackPipe.Expire(ctx, authorsKey, variables.CommunityRecoTelemetryTTL)
		if _, fallbackErr := fallbackPipe.Exec(ctx); fallbackErr != nil {
			numan_log.Warn(ctx).
				Err(fallbackErr).
				Int64("conversation_id", conversationID).
				Int64("sender_id", senderID).
				Msg("Échec d'enregistrement télémétrie message communauté")
			return fallbackErr
		}
	}
	return nil
}

// RecordCommunityMemberJoin incrémente le compteur journalier de croissance
// d'une communauté publique dans community:reco:growth:{YYYY-MM-DD}.
func RecordCommunityMemberJoin(ctx context.Context, conversationID int64) error {
	return RecordCommunityMemberJoinAt(ctx, conversationID, domain.NowTime())
}

// RecordCommunityMemberJoinAt enregistre la croissance pour un instant UTC explicite.
func RecordCommunityMemberJoinAt(ctx context.Context, conversationID int64, at time.Time) error {
	if conversationID <= 0 {
		return nil
	}
	if !isCommunityRecoCacheReady() {
		return nil
	}

	dateBucket := CommunityRecoDateBucket(at)
	ttlSec := int64(variables.CommunityRecoTelemetryTTL / time.Second)
	if ttlSec <= 0 {
		ttlSec = int64((8 * 24 * time.Hour) / time.Second)
	}

	growthKey := redis.CommunityRecoGrowth.Key(dateBucket)
	convField := strconv.FormatInt(conversationID, 10)

	pipe := redis.CommunityRecoGrowth.Pipeline()
	pipe.HIncrBy(ctx, growthKey, convField, 1)
	pipe.Do(ctx, "EXPIRE", growthKey, ttlSec, "NX")

	if _, err := pipe.Exec(ctx); err != nil {
		fallbackPipe := redis.CommunityRecoGrowth.Pipeline()
		fallbackPipe.HIncrBy(ctx, growthKey, convField, 1)
		fallbackPipe.Expire(ctx, growthKey, variables.CommunityRecoTelemetryTTL)
		if _, fallbackErr := fallbackPipe.Exec(ctx); fallbackErr != nil {
			numan_log.Warn(ctx).
				Err(fallbackErr).
				Int64("conversation_id", conversationID).
				Msg("Échec d'enregistrement télémétrie croissance communauté")
			return fallbackErr
		}
	}
	return nil
}

// ============================================================================
// 3. INDEX DES COMMUNAUTÉS PUBLIQUES PAR UTILISATEUR (user:public_comms:{uid})
// ============================================================================

// AddUserPublicCommunity ajoute une communauté publique au SET de l'utilisateur
// et retourne true uniquement si l'adhésion n'y figurait pas déjà (idempotence).
func AddUserPublicCommunity(ctx context.Context, userID int64, conversationID int64) (bool, error) {
	if userID <= 0 || conversationID <= 0 || !isCommunityRecoCacheReady() {
		return false, nil
	}
	added, err := redis.UserPublicComms.SAddCount(ctx, userID, conversationID)
	if err != nil {
		return false, err
	}
	return added > 0, nil
}

// AddUserPublicCommunitiesBatch indexe un lot d'adhésions (userID -> convIDs) en un seul Pipeline.
func AddUserPublicCommunitiesBatch(ctx context.Context, memberships map[int64][]int64) error {
	if len(memberships) == 0 || !isCommunityRecoCacheReady() {
		return nil
	}
	pipe := redis.UserPublicComms.Pipeline()
	for userID, convIDs := range memberships {
		if userID <= 0 || len(convIDs) == 0 {
			continue
		}
		args := make([]any, len(convIDs))
		for i, cid := range convIDs {
			args[i] = cid
		}
		pipe.SAdd(ctx, redis.UserPublicComms.Key(userID), args...)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// RemoveUserPublicCommunity retire une communauté publique du SET d'un utilisateur.
func RemoveUserPublicCommunity(ctx context.Context, userID int64, conversationID int64) error {
	if userID <= 0 || conversationID <= 0 || !isCommunityRecoCacheReady() {
		return nil
	}
	return redis.UserPublicComms.SRem(ctx, userID, conversationID)
}

// GetUserPublicCommunities récupère les identifiants des communautés publiques d'un utilisateur.
func GetUserPublicCommunities(ctx context.Context, userID int64) ([]int64, error) {
	if userID <= 0 || !isCommunityRecoCacheReady() {
		return []int64{}, nil
	}
	raw, err := redis.UserPublicComms.SMembers(ctx, userID)
	if err != nil {
		return nil, err
	}
	return pkg.ParseInt64List(raw), nil
}

// GetDirectFriendIDsCapped récupère les amis directs (degré 1) de l'utilisateur
// depuis SpeedRelationsIndex (in:2 et out:2) en un seul Pipeline Redis, borné à capLimit.
func GetDirectFriendIDsCapped(ctx context.Context, userID int64, capLimit int) ([]int64, error) {
	if userID <= 0 || !redis.IsReady() || redis.SpeedRelationsIndex == nil {
		return []int64{}, nil
	}
	if capLimit <= 0 {
		capLimit = variables.CommunityRecoFriendEvalCap
	}
	if capLimit <= 0 {
		return []int64{}, nil
	}

	inKey := redis.SpeedRelationsIndex.Key(fmt.Sprintf("in:%d:%d", variables.RelationStateFriend, userID))
	outKey := redis.SpeedRelationsIndex.Key(fmt.Sprintf("out:%d:%d", variables.RelationStateFriend, userID))

	pipe := redis.SpeedRelationsIndex.Pipeline()
	inCmd := pipe.ZRevRange(ctx, inKey, 0, int64(capLimit-1))
	outCmd := pipe.ZRevRange(ctx, outKey, 0, int64(capLimit-1))
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, goredis.Nil) {
		return nil, err
	}

	seen := make(map[int64]struct{}, capLimit)
	friends := make([]int64, 0, capLimit)

	appendList := func(raw []string) {
		for _, s := range raw {
			if len(friends) >= capLimit {
				return
			}
			id := pkg.ParseInt64(s)
			if id <= 0 || id == userID {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			friends = append(friends, id)
		}
	}

	if inRes, errIn := inCmd.Result(); errIn == nil {
		appendList(inRes)
	}
	if len(friends) < capLimit {
		if outRes, errOut := outCmd.Result(); errOut == nil {
			appendList(outRes)
		}
	}
	return friends, nil
}

// GetFriendsPublicCommunities agrège en un seul Pipeline Redis (SMEMBERS par ami)
// le nombre d'amis directs membres de chaque communauté publique.
func GetFriendsPublicCommunities(ctx context.Context, friendIDs []int64) (map[int64]int, error) {
	counts := make(map[int64]int)
	if len(friendIDs) == 0 || !isCommunityRecoCacheReady() {
		return counts, nil
	}

	pipe := redis.UserPublicComms.Pipeline()
	cmds := make([]*goredis.StringSliceCmd, len(friendIDs))
	for i, fid := range friendIDs {
		cmds[i] = pipe.SMembers(ctx, redis.UserPublicComms.Key(fid))
	}

	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, goredis.Nil) {
		return nil, err
	}

	for _, cmd := range cmds {
		members, errCmd := cmd.Result()
		if errCmd != nil {
			continue
		}
		// Déduplication par sécurité sur un même ami
		seenForFriend := make(map[int64]struct{}, len(members))
		for _, rawID := range members {
			convID := pkg.ParseInt64(rawID)
			if convID <= 0 {
				continue
			}
			if _, dup := seenForFriend[convID]; dup {
				continue
			}
			seenForFriend[convID] = struct{}{}
			counts[convID]++
		}
	}
	return counts, nil
}

// ============================================================================
// 4. CLASSEMENT GLOBAL PRÉCALCULÉ (community:reco:ranked:{lang})
// ============================================================================

// GetRankedCommunitiesByLang lit l'ensemble du ZSET classé par score décroissant.
func GetRankedCommunitiesByLang(ctx context.Context, lang string) ([]RankedCommunityEntry, error) {
	if !isCommunityRecoCacheReady() {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Le cache de recommandation de communautés est indisponible.",
			errors.New("community reco redis collections not initialized"),
		)
	}
	zs, err := redis.CommunityRecoRanked.ZRevRangeWithScores(ctx, lang, 0, -1)
	if err != nil {
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeCacheError,
			"Impossible de lire le classement des communautés depuis le cache.",
			err,
		)
	}

	out := make([]RankedCommunityEntry, 0, len(zs))
	for _, z := range zs {
		var convID int64
		switch v := z.Member.(type) {
		case string:
			convID = pkg.ParseInt64(v)
		default:
			convID = pkg.ParseInt64(fmt.Sprintf("%v", v))
		}
		if convID <= 0 {
			continue
		}
		out = append(out, RankedCommunityEntry{
			ConversationID: convID,
			Score:          z.Score,
		})
	}
	return out, nil
}

// ReplaceRankedCommunitiesByLang remplace atomiquement (MULTI/EXEC) le ZSET de classement pour une langue.
func ReplaceRankedCommunitiesByLang(ctx context.Context, lang string, entries []RankedCommunityEntry) error {
	if !isCommunityRecoCacheReady() {
		return errors.New("community reco redis collections not initialized")
	}
	members := make([]*goredis.Z, 0, len(entries))
	for _, e := range entries {
		if e.ConversationID <= 0 {
			continue
		}
		members = append(members, &goredis.Z{
			Score:  e.Score,
			Member: strconv.FormatInt(e.ConversationID, 10),
		})
	}
	return redis.CommunityRecoRanked.ZSetReplaceAtomic(ctx, lang, members, 0)
}

// UpsertRankedCommunityScore insère ou met à jour le score d'une communauté dans le ZSET de sa langue.
func UpsertRankedCommunityScore(ctx context.Context, lang string, conversationID int64, score float64) error {
	if lang == "" || conversationID <= 0 || !isCommunityRecoCacheReady() {
		return nil
	}
	return redis.CommunityRecoRanked.ZAdd(ctx, lang, score, strconv.FormatInt(conversationID, 10))
}

// GetRankedCommunityScore retourne le score actuel d'une communauté dans le ZSET de sa langue.
func GetRankedCommunityScore(ctx context.Context, lang string, conversationID int64) (float64, bool, error) {
	if lang == "" || conversationID <= 0 || !isCommunityRecoCacheReady() {
		return 0, false, nil
	}
	score, err := redis.CommunityRecoRanked.ZScore(ctx, lang, strconv.FormatInt(conversationID, 10))
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return score, true, nil
}

// RemoveRankedCommunity supprime une communauté du ZSET de classement d'une langue.
func RemoveRankedCommunity(ctx context.Context, lang string, conversationID int64) error {
	if lang == "" || conversationID <= 0 || !isCommunityRecoCacheReady() {
		return nil
	}
	return redis.CommunityRecoRanked.ZRem(ctx, lang, strconv.FormatInt(conversationID, 10))
}

// ============================================================================
// 5. ÉPINGLAGES ADMINISTRATIFS (community:reco:pins:{lang})
// ============================================================================

// GetCommunityPinsByLang retourne la map slot (1-based) -> conversation_id pour une langue.
func GetCommunityPinsByLang(ctx context.Context, lang string) (map[int]int64, error) {
	pins := make(map[int]int64)
	if lang == "" || !isCommunityRecoCacheReady() {
		return pins, nil
	}
	raw, err := redis.CommunityRecoPins.HGetAll(ctx, lang).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return pins, nil
		}
		return nil, err
	}
	for slotStr, convStr := range raw {
		slot, errSlot := strconv.Atoi(slotStr)
		convID := pkg.ParseInt64(convStr)
		if errSlot != nil || slot <= 0 || convID <= 0 {
			continue
		}
		pins[slot] = convID
	}
	return pins, nil
}

// SetCommunityPinInCache enregistre un slot épinglé dans community:reco:pins:{lang}.
func SetCommunityPinInCache(ctx context.Context, lang string, slot int, conversationID int64) error {
	if lang == "" || slot <= 0 || conversationID <= 0 || !isCommunityRecoCacheReady() {
		return nil
	}
	return redis.CommunityRecoPins.HSet(ctx, lang, strconv.Itoa(slot), strconv.FormatInt(conversationID, 10))
}

// DeleteCommunityPinInCache supprime un slot épinglé dans community:reco:pins:{lang}.
func DeleteCommunityPinInCache(ctx context.Context, lang string, slot int) error {
	if lang == "" || slot <= 0 || !isCommunityRecoCacheReady() {
		return nil
	}
	return redis.CommunityRecoPins.HDel(ctx, lang, strconv.Itoa(slot))
}

// ReplaceCommunityPinsByLang remplace atomiquement (MULTI/EXEC) le HASH des pins d'une langue.
func ReplaceCommunityPinsByLang(ctx context.Context, lang string, pins map[int]int64) error {
	if lang == "" || !isCommunityRecoCacheReady() {
		return nil
	}
	fields := make(map[string]any, len(pins))
	for slot, convID := range pins {
		if slot <= 0 || convID <= 0 {
			continue
		}
		fields[strconv.Itoa(slot)] = strconv.FormatInt(convID, 10)
	}
	return redis.CommunityRecoPins.HashReplaceAtomic(ctx, lang, fields, 0)
}

// RemoveCommunityPinByConversationID supprime toute entrée pointant vers conversationID dans le HASH pins d'une langue.
func RemoveCommunityPinByConversationID(ctx context.Context, lang string, conversationID int64) error {
	if lang == "" || conversationID <= 0 || !isCommunityRecoCacheReady() {
		return nil
	}
	pins, err := GetCommunityPinsByLang(ctx, lang)
	if err != nil {
		return err
	}
	var slotsToDelete []string
	for slot, cid := range pins {
		if cid == conversationID {
			slotsToDelete = append(slotsToDelete, strconv.Itoa(slot))
		}
	}
	if len(slotsToDelete) == 0 {
		return nil
	}
	return redis.CommunityRecoPins.HDel(ctx, lang, slotsToDelete...)
}

// ============================================================================
// 6. LECTURES BATCH TÉLÉMÉTRIE & HYDRATATION (WORKER & READ PATH)
// ============================================================================

// GetCommunity7DayMessageCounts somme en un seul Pipeline Redis (HMGET sur chaque date)
// le nombre de messages reçus par chaque communauté sur la fenêtre de dates fournie.
func GetCommunity7DayMessageCounts(ctx context.Context, conversationIDs []int64, dates []string) (map[int64]int64, error) {
	return sumDailyHashCounts(ctx, redis.CommunityRecoMsgCount, conversationIDs, dates)
}

// GetCommunity7DayGrowthCounts somme en un seul Pipeline Redis (HMGET sur chaque date)
// le nombre de nouvelles adhésions pour chaque communauté sur la fenêtre de dates fournie.
func GetCommunity7DayGrowthCounts(ctx context.Context, conversationIDs []int64, dates []string) (map[int64]int64, error) {
	return sumDailyHashCounts(ctx, redis.CommunityRecoGrowth, conversationIDs, dates)
}

func sumDailyHashCounts(ctx context.Context, col *redis.Collection, conversationIDs []int64, dates []string) (map[int64]int64, error) {
	totals := make(map[int64]int64, len(conversationIDs))
	if len(conversationIDs) == 0 || len(dates) == 0 || !redis.IsReady() || col == nil {
		return totals, nil
	}

	fields := make([]string, len(conversationIDs))
	for i, cid := range conversationIDs {
		fields[i] = strconv.FormatInt(cid, 10)
	}

	pipe := col.Pipeline()
	cmds := make([]*goredis.SliceCmd, len(dates))
	for i, dateBucket := range dates {
		cmds[i] = pipe.HMGet(ctx, col.Key(dateBucket), fields...)
	}

	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, goredis.Nil) {
		return nil, err
	}

	for _, cmd := range cmds {
		vals, errCmd := cmd.Result()
		if errCmd != nil {
			continue
		}
		for idx, raw := range vals {
			if raw == nil || idx >= len(conversationIDs) {
				continue
			}
			var parsed int64
			switch v := raw.(type) {
			case string:
				parsed = pkg.ParseInt64(v)
			case []byte:
				parsed = pkg.ParseInt64(string(v))
			case int64:
				parsed = v
			}
			if parsed > 0 {
				totals[conversationIDs[idx]] += parsed
			}
		}
	}
	return totals, nil
}

// GetCommunity7DayUniqueAuthors calcule la cardinalité union sur les 7 buckets HyperLogLog
// pour chaque communauté via un Pipeline Redis de commandes PFCOUNT multi-clés.
func GetCommunity7DayUniqueAuthors(ctx context.Context, conversationIDs []int64, dates []string) (map[int64]int64, error) {
	result := make(map[int64]int64, len(conversationIDs))
	if len(conversationIDs) == 0 || len(dates) == 0 || !isCommunityRecoCacheReady() {
		return result, nil
	}

	const chunkSize = 250
	for start := 0; start < len(conversationIDs); start += chunkSize {
		end := start + chunkSize
		if end > len(conversationIDs) {
			end = len(conversationIDs)
		}
		batch := conversationIDs[start:end]

		pipe := redis.CommunityRecoAuthors.Pipeline()
		cmds := make([]*goredis.IntCmd, len(batch))
		for i, cid := range batch {
			keys := make([]string, len(dates))
			for j, dateBucket := range dates {
				keys[j] = redis.CommunityRecoAuthors.Key(CommunityRecoAuthorsKey(cid, dateBucket))
			}
			cmds[i] = pipe.PFCount(ctx, keys...)
		}

		_, err := pipe.Exec(ctx)
		if err != nil && !errors.Is(err, goredis.Nil) {
			return nil, err
		}

		for i, cmd := range cmds {
			count, errCmd := cmd.Result()
			if errCmd == nil && count > 0 {
				result[batch[i]] = count
			}
		}
	}
	return result, nil
}

// GetConvMetaBatch récupère un lot de ConvMeta (ConvLiteRequest) depuis Redis via MGET.
func GetConvMetaBatch(ctx context.Context, conversationIDs []int64) (map[int64]lite_models.ConvLiteRequest, []int64, error) {
	found := make(map[int64]lite_models.ConvLiteRequest, len(conversationIDs))
	if len(conversationIDs) == 0 {
		return found, nil, nil
	}
	if !redis.IsReady() || redis.ConvMeta == nil {
		return found, conversationIDs, nil
	}

	res, err := redis.ConvMeta.GetMany(ctx, conversationIDs)
	if err != nil {
		return nil, conversationIDs, err
	}

	missing := append([]int64(nil), res.MissingIDs...)
	for id, raw := range res.Found {
		var meta lite_models.ConvLiteRequest
		if errUnmarshal := msgpack.Unmarshal(raw, &meta); errUnmarshal != nil {
			missing = append(missing, id)
			continue
		}
		found[id] = meta
	}
	return found, missing, nil
}

// GetCommunityMemberCountsBatch récupère le nombre de membres des communautés en batch
// depuis SpeedCommunity (avec repli pipelined SCARD sur ConvParticipants si absent).
func GetCommunityMemberCountsBatch(ctx context.Context, conversationIDs []int64) (map[int64]int, error) {
	counts := make(map[int64]int, len(conversationIDs))
	if len(conversationIDs) == 0 || !redis.IsReady() {
		return counts, nil
	}

	var missing []int64
	if redis.SpeedCommunity != nil {
		res, err := redis.SpeedCommunity.GetMany(ctx, conversationIDs)
		if err == nil {
			for id, raw := range res.Found {
				var comm lite_models.CommunityLiteRequest
				if msgpack.Unmarshal(raw, &comm) == nil && comm.MemberCount >= 0 {
					counts[id] = comm.MemberCount
				} else {
					missing = append(missing, id)
				}
			}
			missing = append(missing, res.MissingIDs...)
		} else {
			missing = conversationIDs
		}
	} else {
		missing = conversationIDs
	}

	if len(missing) > 0 && redis.ConvParticipants != nil {
		pipe := redis.ConvParticipants.Pipeline()
		cmds := make([]*goredis.IntCmd, len(missing))
		for i, cid := range missing {
			cmds[i] = pipe.SCard(ctx, redis.ConvParticipants.Key(cid))
		}
		if _, err := pipe.Exec(ctx); err == nil || errors.Is(err, goredis.Nil) {
			for i, cmd := range cmds {
				if card, errCard := cmd.Result(); errCard == nil && card > 0 {
					counts[missing[i]] = int(card)
				}
			}
		}
	}
	return counts, nil
}

// GetUsersLiteBatch récupère un lot d'objets UserLiteRequest (dont TrustScore) depuis Redis L1 en O(1) RTT.
func GetUsersLiteBatch(ctx context.Context, userIDs []int64) (map[int64]lite_models.UserLiteRequest, error) {
	found := make(map[int64]lite_models.UserLiteRequest, len(userIDs))
	if len(userIDs) == 0 || !redis.IsReady() || redis.UsersLite == nil {
		return found, nil
	}

	const chunkSize = 500
	for start := 0; start < len(userIDs); start += chunkSize {
		end := start + chunkSize
		if end > len(userIDs) {
			end = len(userIDs)
		}
		res, err := redis.UsersLite.GetMany(ctx, userIDs[start:end])
		if err != nil {
			return nil, err
		}
		for uid, raw := range res.Found {
			var u lite_models.UserLiteRequest
			if msgpack.Unmarshal(raw, &u) == nil {
				found[uid] = u
			}
		}
	}
	return found, nil
}
