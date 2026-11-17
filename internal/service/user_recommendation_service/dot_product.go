package user_recommendation_service

import (
	"fmt"
	"math"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # CALCUL VECTORIEL : PRODUIT SCALAIRE [224]float32 (SIMD + FALLBACK PORTABLE)
// ############################################################################

// VectorDimension définit la dimension stricte des vecteurs d'intérêt utilisateur générés par le client (Edge Computing).
const VectorDimension = variables.VectorDimTotal // 224

// useSIMDDotProduct permet d'activer le noyau vectorisé 8-voies (avec repli automatique vers le scalaire portable).
var useSIMDDotProduct = true

// DotProduct224 calcule le produit scalaire entre deux vecteurs d'intérêt utilisateur de dimension [224]float32.
// Utilise l'implémentation vectorisée 8-voies (SIMD/FMA friendly) lorsque supportée, sinon le fallback Go portable.
func DotProduct224(target *[VectorDimension]float32, candidate *[VectorDimension]float32) float64 {
	if target == nil || candidate == nil {
		return 0.0
	}
	var result float64
	if useSIMDDotProduct {
		result = DotProductSIMD224(target, candidate)
	} else {
		result = DotProductPortable224(target, candidate)
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0.0
	}
	return result
}

// DotProductSIMD224 exécute un produit scalaire déroulé sur 8 accumulateurs indépendants (28 blocs x 8 float32 = 224).
// Cette structure sans dépendance de boucle intermédiaire permet au compilateur Go (ARM64 NEON / AMD64 AVX/SSE)
// de générer des instructions vectorielles SIMD tout en restant 100% sûre mémoire.
func DotProductSIMD224(a *[VectorDimension]float32, b *[VectorDimension]float32) float64 {
	if a == nil || b == nil {
		return 0.0
	}

	var s0, s1, s2, s3, s4, s5, s6, s7 float32

	// 224 est un multiple exact de 8 (28 * 8 = 224)
	for i := 0; i < VectorDimension; i += 8 {
		s0 += a[i+0] * b[i+0]
		s1 += a[i+1] * b[i+1]
		s2 += a[i+2] * b[i+2]
		s3 += a[i+3] * b[i+3]
		s4 += a[i+4] * b[i+4]
		s5 += a[i+5] * b[i+5]
		s6 += a[i+6] * b[i+6]
		s7 += a[i+7] * b[i+7]
	}

	total := float64(s0+s1) + float64(s2+s3) + float64(s4+s5) + float64(s6+s7)
	if math.IsNaN(total) || math.IsInf(total, 0) {
		return 0.0
	}
	return total
}

// DotProductPortable224 est l'implémentation de référence portable en Go pur (fallback scalaire).
func DotProductPortable224(a *[VectorDimension]float32, b *[VectorDimension]float32) float64 {
	if a == nil || b == nil {
		return 0.0
	}
	var sum float64
	for i := 0; i < VectorDimension; i++ {
		sum += float64(a[i]) * float64(b[i])
	}
	if math.IsNaN(sum) || math.IsInf(sum, 0) {
		return 0.0
	}
	return sum
}

// DotProductSlice valide que les deux tranches ont exactement 224 dimensions avant de calculer leur produit scalaire.
// Conformément à la règle métier critique, le serveur ne génère ni ne complète jamais de dimensions manquantes.
func DotProductSlice(target []float32, candidate []float32) (float64, error) {
	if len(target) != VectorDimension || len(candidate) != VectorDimension {
		return 0.0, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Dimensions du vecteur d'intérêt invalides (attendu : 224 float32).",
			fmt.Errorf("expected vector dimension %d, got target=%d candidate=%d", VectorDimension, len(target), len(candidate)),
		)
	}

	var a, b [VectorDimension]float32
	copy(a[:], target)
	copy(b[:], candidate)
	return DotProduct224(&a, &b), nil
}

// IsNonZeroVector224 vérifie si un vecteur [224]float32 contient au moins une composante finie non nulle.
func IsNonZeroVector224(v []float32) (*[VectorDimension]float32, bool) {
	if len(v) != VectorDimension {
		return nil, false
	}
	var arr [VectorDimension]float32
	hasNonZero := false
	for i := 0; i < VectorDimension; i++ {
		val := v[i]
		if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return nil, false
		}
		arr[i] = val
		if val != 0 {
			hasNonZero = true
		}
	}
	return &arr, hasNonZero
}
