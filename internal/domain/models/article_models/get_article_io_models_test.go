package article_models_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetArticleInput_Validate(t *testing.T) {
	in := article_models.GetArticleInput{ArticleID: 42}
	require.NoError(t, in.Validate())
	assert.Equal(t, int64(42), in.ResolvedID())

	inAlias := article_models.GetArticleInput{ID: 99}
	require.NoError(t, inAlias.Validate())
	assert.Equal(t, int64(99), inAlias.ResolvedID())

	inInvalid := article_models.GetArticleInput{}
	assert.Error(t, inInvalid.Validate())

	inNegative := article_models.GetArticleInput{ArticleID: -10}
	assert.Error(t, inNegative.Validate())
}
