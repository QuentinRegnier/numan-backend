package worker

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	infraRedis "github.com/QuentinRegnier/numan-backend/internal/infrastructure/redis"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPOIReviewMapper(t *testing.T) {
	mapper := getMapper(redis.EntityPOIReview)
	require.NotNil(t, mapper)
	assert.Equal(t, "content.poi_reviews", mapper.TableName())
	assert.Equal(t, []string{"id", "poi_id", "user_id", "rating", "content", "visibility", "created_at", "updated_at"}, mapper.Columns())

	// Vérifie que EntityPOICounter ne possède pas de mapper relationnel
	assert.Nil(t, getMapper(redis.EntityPOICounter))

	review := map_models.POIReviewPayload{
		ID:         7001,
		POIID:      5001,
		UserID:     1001,
		Rating:     5,
		Content:    "",
		Visibility: 0,
		CreatedAt:  1700000000000,
		UpdatedAt:  1700000000000,
	}

	row, err := mapper.ToRow(review)
	require.NoError(t, err)
	require.Len(t, row, 8)
	assert.Equal(t, int64(7001), row[0])
	assert.Equal(t, int64(5001), row[1])
	assert.Equal(t, int64(1001), row[2])
	assert.Equal(t, int8(5), row[3])
	assert.Equal(t, "", row[4]) // Chaîne vide conservée (modèle unifié)
	assert.Equal(t, 0, row[5])

	q := mapper.BuildUpdateQuery("tmp_poi_reviews")
	assert.Contains(t, q, "UPDATE content.poi_reviews SET")
}

func TestInteractionWorker_POIReviewAggregation(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	// Simule plusieurs avis/mises à jour/suppressions dans la même fenêtre de flush
	batch := []interaction{
		{TargetID: 5001, Type: "poi_review", RatingDelta: 5, Delta: 1},   // User A note 5
		{TargetID: 5001, Type: "poi_review", RatingDelta: 3, Delta: 1},   // User B note 3
		{TargetID: 5001, Type: "poi_review", RatingDelta: -1, Delta: 0},  // User A passe de 5 à 4
		{TargetID: 5001, Type: "poi_review", RatingDelta: -3, Delta: -1}, // User B supprime sa note
		// POI 5002 : création puis suppression dans la même fenêtre -> delta net 0 (aucun événement émis)
		{TargetID: 5002, Type: "poi_review", RatingDelta: 4, Delta: 1},
		{TargetID: 5002, Type: "poi_review", RatingDelta: -4, Delta: -1},
	}

	processCacheUpdates(ctx, batch)

	// Vérifie que seul 1 événement EntityPOICounter pour POI 5001 a été empilé dans Redis
	keys, err := infraRedis.Rdb.Keys(ctx, "request_cache:q:*").Result()
	require.NoError(t, err)

	var counterEvents []redis.AsyncEvent
	for _, k := range keys {
		items, errL := infraRedis.Rdb.LRange(ctx, k, 0, -1).Result()
		require.NoError(t, errL)
		for _, raw := range items {
			var evt redis.AsyncEvent
			dec := json.NewDecoder(strings.NewReader(raw))
			dec.UseNumber()
			require.NoError(t, dec.Decode(&evt))
			if evt.Type == redis.EntityPOICounter {
				counterEvents = append(counterEvents, evt)
			}
		}
	}

	require.Len(t, counterEvents, 1)
	assert.Equal(t, int64(5001), counterEvents[0].ID)
	assert.Equal(t, redis.ActionUpdate, counterEvents[0].Action)
	assert.Equal(t, redis.TargetPostgres, counterEvents[0].Targets)

	payloadBytes, _ := json.Marshal(counterEvents[0].Payload)
	var p struct {
		POIID       int64 `json:"poi_id"`
		RatingSum   int64 `json:"rating_sum"`
		ReviewCount int   `json:"review_count"`
	}
	require.NoError(t, json.Unmarshal(payloadBytes, &p))
	assert.Equal(t, int64(5001), p.POIID)
	assert.Equal(t, int64(4), p.RatingSum) // 5 + 3 - 1 - 3 = 4
	assert.Equal(t, 1, p.ReviewCount)      // 1 + 1 + 0 - 1 = 1
}

func TestPostgresBatch_POIReviewUpsertDeleteAndCounterFlush(t *testing.T) {
	ctx := context.Background()

	type execCall struct {
		query string
		args  []driver.Value
	}
	var mu sync.Mutex
	var calls []execCall

	test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		vals := make([]driver.Value, len(args))
		for i, a := range args {
			vals[i] = a.Value
		}
		calls = append(calls, execCall{query: query, args: vals})
		return driver.RowsAffected(1), nil
	})

	events := []redis.AsyncEvent{
		// Deux événements upsert pour le même couple (5001, 1001) -> dédupliqué Last-Write-Wins
		{
			ID:     8001,
			Type:   redis.EntityPOIReview,
			Action: redis.ActionCreate,
			Payload: map_models.POIReviewPayload{
				ID: 8001, POIID: 5001, UserID: 1001, Rating: 4, Content: "Bien", Visibility: 0, CreatedAt: 100, UpdatedAt: 100,
			},
			Targets: redis.TargetPostgres,
		},
		{
			ID:     8001,
			Type:   redis.EntityPOIReview,
			Action: redis.ActionCreate,
			Payload: map_models.POIReviewPayload{
				ID: 8001, POIID: 5001, UserID: 1001, Rating: 5, Content: "Finalement génial", Visibility: 0, CreatedAt: 100, UpdatedAt: 200,
			},
			Targets: redis.TargetPostgres,
		},
		// Suppression logique (Soft Delete) avec json.Number (tel que décodé par PopSmartBatchBlocking)
		{
			ID:     8002,
			Type:   redis.EntityPOIReview,
			Action: redis.ActionDelete,
			Payload: map[string]any{
				"poi_id":     json.Number("5001"),
				"user_id":    json.Number("1002"),
				"visibility": json.Number("-1"),
			},
			Targets: redis.TargetPostgres,
		},
		// Deux événements EntityPOICounter pour le même POI 5001 -> doivent être fusionnés en 1 seul appel SQL
		{
			ID:     5001,
			Type:   redis.EntityPOICounter,
			Action: redis.ActionUpdate,
			Payload: map[string]any{
				"poi_id":       json.Number("5001"),
				"rating_sum":   json.Number("5"),
				"review_count": json.Number("1"),
			},
			Targets: redis.TargetPostgres,
		},
		{
			ID:     5001,
			Type:   redis.EntityPOICounter,
			Action: redis.ActionUpdate,
			Payload: map[string]any{
				"poi_id":       json.Number("5001"),
				"rating_sum":   json.Number("-2"),
				"review_count": json.Number("-1"),
			},
			Targets: redis.TargetPostgres,
		},
	}

	flushPostgres(ctx, events)

	mu.Lock()
	defer mu.Unlock()

	var upsertCalls, deleteCalls, counterCalls []execCall
	for _, c := range calls {
		switch {
		case strings.Contains(c.query, "content.func_upsert_poi_review"):
			upsertCalls = append(upsertCalls, c)
		case strings.Contains(c.query, "content.func_soft_delete_poi_review"):
			deleteCalls = append(deleteCalls, c)
		case strings.Contains(c.query, "content.func_increment_poi_counters"):
			counterCalls = append(counterCalls, c)
		}
	}

	// 1. Upsert dédupliqué à 1 seul appel avec rating=5 et visibility=0
	require.Len(t, upsertCalls, 1)
	assert.Equal(t, int64(5), upsertCalls[0].args[3])
	assert.Equal(t, "Finalement génial", upsertCalls[0].args[4])
	assert.Equal(t, int64(0), upsertCalls[0].args[5])

	// 2. Soft Delete appelé pour (5001, 1002)
	require.Len(t, deleteCalls, 1)
	assert.Equal(t, int64(5001), deleteCalls[0].args[0])
	assert.Equal(t, int64(1002), deleteCalls[0].args[1])

	// 3. Compteurs POI agrégés et exécutés UNE SEULE FOIS par POI dans le batch
	require.Len(t, counterCalls, 1)
	assert.Equal(t, int64(5001), counterCalls[0].args[0])
	assert.Equal(t, int64(3), counterCalls[0].args[1]) // 5 + (-2) = 3
	assert.Equal(t, int64(0), counterCalls[0].args[2]) // 1 + (-1) = 0
}
