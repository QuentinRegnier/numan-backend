package article_models_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetArticlesInput_ValidateAndNormalize(t *testing.T) {
	in := article_models.GetArticlesInput{}
	require.NoError(t, in.ValidateAndNormalize())
	assert.Equal(t, variables.DefaultArticleListLimit, in.Limit)
	assert.Equal(t, 0, in.Offset)

	inTooHigh := article_models.GetArticlesInput{Limit: 999, Offset: 10}
	require.NoError(t, inTooHigh.ValidateAndNormalize())
	assert.Equal(t, variables.MaxArticleListLimit, inTooHigh.Limit)
	assert.Equal(t, 10, inTooHigh.Offset)

	inNegOffset := article_models.GetArticlesInput{Limit: 10, Offset: -1}
	assert.Error(t, inNegOffset.ValidateAndNormalize())

	inNegLimit := article_models.GetArticlesInput{Limit: -5, Offset: 0}
	assert.Error(t, inNegLimit.ValidateAndNormalize())
}
