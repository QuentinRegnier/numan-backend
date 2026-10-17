package cache_service

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/lite_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

func TestSpeedCacheCommunities(t *testing.T) {
	srv := setupTestRedis(t)
	ctx := context.Background()

	t.Run("stores communities, updates member counts, and searches by prefix preserving lexicographical order", func(t *testing.T) {
		c1 := lite_models.CommunityLiteRequest{ID: 10, Name: "GamingFR", Description: "FR gamers", MemberCount: 5}
		c2 := lite_models.CommunityLiteRequest{ID: 20, Name: "GamingPro", Description: "Pro gamers", MemberCount: 10}
		c3 := lite_models.CommunityLiteRequest{ID: 30, Name: "Cooking", Description: "Chefs", MemberCount: 3}

		for _, c := range []lite_models.CommunityLiteRequest{c2, c1, c3} {
			if err := StoreCommunityLiteInSpeedCache(ctx, c); err != nil {
				t.Fatalf("StoreCommunityLiteInSpeedCache(%d) error: %v", c.ID, err)
			}
		}

		// Search "GAM"
		results, err := SearchCommunitiesByPrefix(ctx, "GAM", 0, 10)
		if err != nil {
			t.Fatalf("SearchCommunitiesByPrefix(\"GAM\") error: %v", err)
		}
		if len(results) != 2 || results[0].ID != 10 || results[1].ID != 20 {
			t.Fatalf("SearchCommunitiesByPrefix(\"GAM\") = %+v, want IDs [10, 20]", results)
		}

		// Update member count: delta 0 (no-op), positive delta (+3), negative delta clamping at 0 (-100)
		updateCommunityMemberCountInSpeedCache(ctx, 10, 0)
		updateCommunityMemberCountInSpeedCache(ctx, 10, 3)
		updateCommunityMemberCountInSpeedCache(ctx, 30, -100)
		// Non-existent community should be silently ignored
		updateCommunityMemberCountInSpeedCache(ctx, 9999, 5)

		var updated10, updated30 lite_models.CommunityLiteRequest
		if err := repoRedis.SpeedCommunity.GetObject(ctx, 10, &updated10); err != nil {
			t.Fatalf("GetObject(10) error: %v", err)
		}
		if updated10.MemberCount != 8 {
			t.Errorf("community 10 MemberCount = %d, want 8", updated10.MemberCount)
		}

		if err := repoRedis.SpeedCommunity.GetObject(ctx, 30, &updated30); err != nil {
			t.Fatalf("GetObject(30) error: %v", err)
		}
		if updated30.MemberCount != 0 {
			t.Errorf("community 30 MemberCount after clamping = %d, want 0", updated30.MemberCount)
		}
	})

	t.Run("SearchCommunitiesByPrefix returns empty slice when no matches found", func(t *testing.T) {
		got, err := SearchCommunitiesByPrefix(ctx, "zzz", 0, 10)
		if err != nil {
			t.Fatalf("SearchCommunitiesByPrefix(\"zzz\") error: %v", err)
		}
		if !reflect.DeepEqual(got, []lite_models.CommunityLiteRequest{}) {
			t.Errorf("SearchCommunitiesByPrefix(\"zzz\") = %v, want empty slice", got)
		}
	})

	t.Run("error handling on Redis failures", func(t *testing.T) {
		c := lite_models.CommunityLiteRequest{ID: 55, Name: "TestComm"}

		srv.setFailCmd("ZADD", errors.New("zadd failure"))
		err := StoreCommunityLiteInSpeedCache(ctx, c)
		srv.setFailCmd("ZADD", nil)
		if err == nil {
			t.Fatal("StoreCommunityLiteInSpeedCache() error = nil on ZADD failure")
		}

		srv.setFailCmd("SET", errors.New("set failure"))
		err = StoreCommunityLiteInSpeedCache(ctx, c)
		srv.setFailCmd("SET", nil)
		if err == nil {
			t.Fatal("StoreCommunityLiteInSpeedCache() error = nil on SET failure")
		}

		srv.setFailCmd("ZRANGEBYLEX", errors.New("lex failure"))
		_, err = SearchCommunitiesByPrefix(ctx, "test", 0, 10)
		srv.setFailCmd("ZRANGEBYLEX", nil)
		if err == nil {
			t.Fatal("SearchCommunitiesByPrefix() error = nil on ZRANGEBYLEX failure")
		}

		srv.setFailCmd("MGET", errors.New("mget failure"))
		_, err = SearchCommunitiesByPrefix(ctx, "test", 0, 10)
		srv.setFailCmd("MGET", nil)
		var appErr *numan_error.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusInternalServerError {
			t.Errorf("SearchCommunitiesByPrefix() on MGET failure error = %v, want 500 AppError", err)
		}
	})
}
