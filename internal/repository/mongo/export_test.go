package mongo

import (
	"context"

	"github.com/QuentinRegnier/numan-backend/internal/repository/redis"
)

// UpdateForTest exports the private update method for package mongo_test.
func (c *Collection) UpdateForTest(filter, updateDoc map[string]any) error {
	return c.update(filter, updateDoc)
}

// ValidateForTest exports the private validate method for package mongo_test.
func (c *Collection) ValidateForTest(ctx context.Context, doc map[string]any, isUpdate bool) error {
	return c.validate(ctx, doc, isUpdate)
}

// Redis2MongoForTest exports the private redis2Mongo helper for package mongo_test.
func Redis2MongoForTest(ctx context.Context, entity redis.EntityType) (*Collection, error) {
	return redis2Mongo(ctx, entity)
}
