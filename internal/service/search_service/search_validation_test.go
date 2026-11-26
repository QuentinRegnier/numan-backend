package search_service_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/search_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	repoRedis "github.com/QuentinRegnier/numan-backend/internal/repository/redis"
	"github.com/QuentinRegnier/numan-backend/internal/service/search_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}

func TestSearchServices_ValidationAndPagination(t *testing.T) {
	_ = test.SetupTestRedis(t)
	ctx := context.Background()

	// Invalid payloads rejected before Redis queries
	_, err := search_service.AutocompleteTags(ctx, search_models.AutocompleteTagInput{Query: "   ", Offset: 0, Limit: 10})
	assertBadRequest(t, err)
	_, err = search_service.AutocompleteTags(ctx, search_models.AutocompleteTagInput{Query: "go", Offset: -1, Limit: 10})
	assertBadRequest(t, err)
	_, err = search_service.AutocompleteTags(ctx, search_models.AutocompleteTagInput{Query: "go", Offset: 0, Limit: 51})
	assertBadRequest(t, err)

	_, err = search_service.AutocompleteText(ctx, 1, search_models.AutocompleteTextInput{Query: "\t", Offset: 0, Limit: 10})
	assertBadRequest(t, err)

	_, err = search_service.SearchPosts(ctx, 1, search_models.SearchPostInput{Query: "   ", Filter: "", Offset: 0, Limit: 10})
	assertBadRequest(t, err)
	_, err = search_service.SearchPosts(ctx, 1, search_models.SearchPostInput{Query: "go", Filter: "bad", Offset: 0, Limit: 10})
	assertBadRequest(t, err)

	keys, errKeys := repoRedis.ScanKeys(ctx, "*", 200)
	require.NoError(t, errKeys)
	assert.Empty(t, keys)

	// Valid paginated queries (previously broken by pkg.BatchVerif returning 403)
	tagOut, errTag := search_service.AutocompleteTags(ctx, search_models.AutocompleteTagInput{Query: "golang", Offset: 0, Limit: 15})
	require.NoError(t, errTag)
	assert.NotNil(t, tagOut.Tags)

	textOut, errText := search_service.AutocompleteText(ctx, 1, search_models.AutocompleteTextInput{Query: "numan", Offset: 10, Limit: 20})
	require.NoError(t, errText)
	assert.NotNil(t, textOut.Users.Users)
	assert.NotNil(t, textOut.Communities.Communities)

	// Valid filter-only post discovery with empty Query and Offset=0
	postOut, errPost := search_service.SearchPosts(ctx, 1, search_models.SearchPostInput{
		Query:  "",
		Filter: variables.SearchFilterLikes,
		Offset: 0,
		Limit:  10,
	})
	require.NoError(t, errPost)
	assert.NotNil(t, postOut.Posts)
}
