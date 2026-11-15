package worker

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/map_models"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPOIMapper_And_BulkDeletePostgres(t *testing.T) {
	mapper := getMapper(redis.EntityPOI)
	require.NotNil(t, mapper)
	assert.Equal(t, "content.pois", mapper.TableName())
	assert.Len(t, mapper.Columns(), 17)

	poi := map_models.POIPayload{
		ID:             9001,
		Type:           2,
		Title:          "Jardin du Luxembourg",
		Description:    "Parc public du Sénat",
		Latitude:       48.846222,
		Longitude:      2.337160,
		Address:        "75006 Paris",
		Phone:          "",
		Website:        "https://www.senat.fr",
		Rating:         4.8,
		CommentCount:   150,
		MediaIDs:       []int64{401, 402},
		GoogleMapsLink: "https://maps.google.com/?cid=luxembourg",
		AppleMapsLink:  "",
		Visibility:     0,
		CreatedAt:      1700000000000,
		UpdatedAt:      1700000000000,
	}

	row, err := mapper.ToRow(poi)
	require.NoError(t, err)
	require.Len(t, row, 17)
	assert.Equal(t, int64(9001), row[0])
	assert.Equal(t, int16(2), row[1])
	assert.Equal(t, "Jardin du Luxembourg", row[2])
	assert.Nil(t, row[7]) // Empty phone -> SQL NULL
	assert.Equal(t, "https://www.senat.fr", row[8])
	assert.Equal(t, "https://maps.google.com/?cid=luxembourg", row[12])
	assert.Nil(t, row[13]) // Empty apple_maps_link -> SQL NULL
	assert.Equal(t, 0, row[14])

	updateQuery := mapper.BuildUpdateQuery("tmp_content_pois")
	assert.Contains(t, updateQuery, "UPDATE content.pois SET")
	assert.Contains(t, updateQuery, "WHERE content.pois.id = tmp_content_pois.id")

	// Verify bulkDeletePostgres invokes content.func_bulk_soft_delete_pois
	var executedQuery string
	test.SetupMockDB(t, nil, func(query string, args []driver.NamedValue) (driver.Result, error) {
		executedQuery = query
		return driver.RowsAffected(2), nil
	})

	errDel := bulkDeletePostgres(context.Background(), redis.EntityPOI, []redis.AsyncEvent{
		{ID: 9001, Type: redis.EntityPOI, Action: redis.ActionDelete},
		{ID: 9002, Type: redis.EntityPOI, Action: redis.ActionDelete},
	})
	require.NoError(t, errDel)
	assert.Contains(t, executedQuery, "content.func_bulk_soft_delete_pois")
}
