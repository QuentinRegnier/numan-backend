package user_recommendation_service_test

import (
	"math"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/service/user_recommendation_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDotProduct224_AccuracyAndEdgeCases(t *testing.T) {
	t.Run("nil_vectors_return_zero", func(t *testing.T) {
		var v [user_recommendation_service.VectorDimension]float32
		assert.Equal(t, 0.0, user_recommendation_service.DotProduct224(nil, &v))
		assert.Equal(t, 0.0, user_recommendation_service.DotProduct224(&v, nil))
		assert.Equal(t, 0.0, user_recommendation_service.DotProductSIMD224(nil, &v))
		assert.Equal(t, 0.0, user_recommendation_service.DotProductPortable224(nil, &v))
	})

	t.Run("zero_vectors_return_zero", func(t *testing.T) {
		var a, b [user_recommendation_service.VectorDimension]float32
		assert.Equal(t, 0.0, user_recommendation_service.DotProduct224(&a, &b))
		assert.Equal(t, 0.0, user_recommendation_service.DotProductSIMD224(&a, &b))
		assert.Equal(t, 0.0, user_recommendation_service.DotProductPortable224(&a, &b))
	})

	t.Run("simd_and_portable_match_within_floating_point_tolerance", func(t *testing.T) {
		var a, b [user_recommendation_service.VectorDimension]float32
		for i := 0; i < user_recommendation_service.VectorDimension; i++ {
			a[i] = float32(i%17-8) * 0.125
			b[i] = float32((i*3)%19-9) * 0.25
		}

		simdRes := user_recommendation_service.DotProductSIMD224(&a, &b)
		portableRes := user_recommendation_service.DotProductPortable224(&a, &b)
		dispatchRes := user_recommendation_service.DotProduct224(&a, &b)

		assert.InDelta(t, portableRes, simdRes, 1e-4)
		assert.InDelta(t, portableRes, dispatchRes, 1e-4)
	})

	t.Run("negative_values_and_orthogonal_vectors", func(t *testing.T) {
		var a, b [user_recommendation_service.VectorDimension]float32
		for i := 0; i < user_recommendation_service.VectorDimension; i++ {
			a[i] = 1.0
			b[i] = -0.5
		}
		// 224 * (1.0 * -0.5) = -112.0
		assert.InDelta(t, -112.0, user_recommendation_service.DotProduct224(&a, &b), 1e-5)
	})

	t.Run("nan_and_inf_guarded", func(t *testing.T) {
		var a, b [user_recommendation_service.VectorDimension]float32
		a[0] = float32(math.NaN())
		b[0] = 1.0
		assert.Equal(t, 0.0, user_recommendation_service.DotProduct224(&a, &b))
		assert.Equal(t, 0.0, user_recommendation_service.DotProductPortable224(&a, &b))
	})
}

func TestDotProductSlice_DimensionValidation(t *testing.T) {
	validA := make([]float32, user_recommendation_service.VectorDimension)
	validB := make([]float32, user_recommendation_service.VectorDimension)
	for i := range validA {
		validA[i] = 0.5
		validB[i] = 2.0
	}

	score, err := user_recommendation_service.DotProductSlice(validA, validB)
	require.NoError(t, err)
	assert.InDelta(t, 224.0, score, 1e-5)

	// Invalid dimensions must be rejected (never inferred or padded)
	_, errShort := user_recommendation_service.DotProductSlice(validA[:100], validB)
	assert.Error(t, errShort)

	_, errLong := user_recommendation_service.DotProductSlice(validA, append(validB, 1.0))
	assert.Error(t, errLong)

	// IsNonZeroVector224 checks
	arr, nonZero := user_recommendation_service.IsNonZeroVector224(validA)
	require.NotNil(t, arr)
	assert.True(t, nonZero)

	zeroSlice := make([]float32, user_recommendation_service.VectorDimension)
	arrZero, nonZeroFalse := user_recommendation_service.IsNonZeroVector224(zeroSlice)
	require.NotNil(t, arrZero)
	assert.False(t, nonZeroFalse)

	_, okWrongDim := user_recommendation_service.IsNonZeroVector224([]float32{1, 2, 3})
	assert.False(t, okWrongDim)
}

func BenchmarkDotProductSIMD224(b *testing.B) {
	var v1, v2 [user_recommendation_service.VectorDimension]float32
	for i := 0; i < user_recommendation_service.VectorDimension; i++ {
		v1[i] = float32(i) * 0.01
		v2[i] = float32(224-i) * 0.01
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = user_recommendation_service.DotProductSIMD224(&v1, &v2)
	}
}

func BenchmarkDotProductPortable224(b *testing.B) {
	var v1, v2 [user_recommendation_service.VectorDimension]float32
	for i := 0; i < user_recommendation_service.VectorDimension; i++ {
		v1[i] = float32(i) * 0.01
		v2[i] = float32(224-i) * 0.01
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = user_recommendation_service.DotProductPortable224(&v1, &v2)
	}
}
