package article_models_test

import (
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/article_models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteArticleInput_ResolvedIDs(t *testing.T) {
	in := article_models.DeleteArticleInput{
		ArticleID:  10,
		ArticleIDs: []int64{20, 10, 30},
	}
	ids, err := in.ResolvedIDs()
	require.NoError(t, err)
	assert.Equal(t, []int64{10, 20, 30}, ids)

	byAlias := article_models.DeleteArticleInput{ID: 77}
	idsAlias, errAlias := byAlias.ResolvedIDs()
	require.NoError(t, errAlias)
	assert.Equal(t, []int64{77}, idsAlias)

	empty := article_models.DeleteArticleInput{}
	_, errEmpty := empty.ResolvedIDs()
	assert.Error(t, errEmpty)

	negSingle := article_models.DeleteArticleInput{ArticleID: -1}
	_, errNegSingle := negSingle.ResolvedIDs()
	assert.Error(t, errNegSingle)

	negAlias := article_models.DeleteArticleInput{ID: -2}
	_, errNegAlias := negAlias.ResolvedIDs()
	assert.Error(t, errNegAlias)

	negBatch := article_models.DeleteArticleInput{ArticleIDs: []int64{10, -5}}
	_, errNegBatch := negBatch.ResolvedIDs()
	assert.Error(t, errNegBatch)

	badExp := int64(0)
	invalidConc := article_models.DeleteArticleInput{ArticleID: 10, ExpectedUpdatedAt: &badExp}
	_, errConc := invalidConc.ResolvedIDs()
	assert.Error(t, errConc)

	okExp := int64(1700000001000)
	validConc := article_models.DeleteArticleInput{ArticleID: 10, ExpectedUpdatedAt: &okExp}
	idsConc, errValidConc := validConc.ResolvedIDs()
	require.NoError(t, errValidConc)
	assert.Equal(t, []int64{10}, idsConc)
}
