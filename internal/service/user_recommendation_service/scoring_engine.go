package user_recommendation_service

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/QuentinRegnier/numan-backend/internal/service/cache_service"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

// ############################################################################
// # MOTEUR DE SCORING MATHÉMATIQUE — RECOMMANDATION D'UTILISATEURS
// ############################################################################

const earthRadiusKm = 6371.0

// knownCityCoordinates fournit un référentiel déterministe de coordonnées GPS
// lorsque le champ auth.users.location contient un nom de ville plutôt qu'un couple "lat,lon".
var knownCityCoordinates = map[string][2]float64{
	"paris":         {48.8566, 2.3522},
	"lyon":          {45.7640, 4.8357},
	"marseille":     {43.2965, 5.3698},
	"toulouse":      {43.6047, 1.4442},
	"nice":          {43.7102, 7.2620},
	"nantes":        {47.2184, -1.5536},
	"montpellier":   {43.6108, 3.8767},
	"strasbourg":    {48.5734, 7.7521},
	"bordeaux":      {44.8378, -0.5792},
	"lille":         {50.6292, 3.0573},
	"rennes":        {48.1173, -1.6778},
	"grenoble":      {45.1885, 5.7245},
	"london":        {51.5074, -0.1278},
	"berlin":        {52.5200, 13.4050},
	"madrid":        {40.4168, -3.7038},
	"rome":          {41.9028, 12.4964},
	"brussels":      {50.8503, 4.3517},
	"bruxelles":     {50.8503, 4.3517},
	"geneva":        {46.2044, 6.1432},
	"geneve":        {46.2044, 6.1432},
	"zurich":        {47.3769, 8.5417},
	"amsterdam":     {52.3676, 4.9041},
	"new york":      {40.7128, -74.0060},
	"san francisco": {37.7749, -122.4194},
	"los angeles":   {34.0522, -118.2437},
	"montreal":      {45.5017, -73.5673},
	"tokyo":         {35.6762, 139.6503},
}

// ============================================================================
// 2.1. MULTIPLICATEUR COMMUNAUTAIRE (TF-IDF)
// ============================================================================

// ComputeCommunityIDF calcule le poids IDF d'une communauté : IDF(c) = log(N / |c|).
// Protégé contre les divisions par zéro, les tailles négatives et le cas où |c| >= N.
func ComputeCommunityIDF(totalActiveUsers int64, communityMemberCount int64) float64 {
	if totalActiveUsers <= 0 || communityMemberCount <= 0 {
		return 0.0
	}
	if communityMemberCount >= totalActiveUsers {
		return 0.0
	}
	idf := math.Log(float64(totalActiveUsers) / float64(communityMemberCount))
	if math.IsNaN(idf) || math.IsInf(idf, 0) || idf < 0 {
		return 0.0
	}
	return idf
}

// ComputeCommunityMultiplier calcule : CommunityMultiplier = 1.0 + sum(IDF(c))
// pour l'ensemble des communautés partagées entre l'utilisateur cible et le candidat.
func ComputeCommunityMultiplier(totalActiveUsers int64, sharedCommunitySizes []int64) float64 {
	multiplier := 1.0
	if len(sharedCommunitySizes) == 0 || totalActiveUsers <= 0 {
		return multiplier
	}
	for _, size := range sharedCommunitySizes {
		multiplier += ComputeCommunityIDF(totalActiveUsers, size)
	}
	return multiplier
}

// ============================================================================
// 2.2. TASTE SCORE (SIMILARITÉ VECTORIELLE D'INTÉRÊTS)
// ============================================================================

// ComputeTasteScore calcule : ScoreTaste = DotProduct(V_target, V_candidate) * CommunityMultiplier.
func ComputeTasteScore(
	targetVector *[VectorDimension]float32,
	candidateVector *[VectorDimension]float32,
	communityMultiplier float64,
) float64 {
	if targetVector == nil || candidateVector == nil {
		return 0.0
	}
	if communityMultiplier <= 0 {
		communityMultiplier = 1.0
	}
	dot := DotProduct224(targetVector, candidateVector)
	score := dot * communityMultiplier
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0.0
	}
	return score
}

// ============================================================================
// 2.3. SOCIAL AFFINITY SCORE (AMIS EN COMMUN)
// ============================================================================

// ComputeAffinityScore calcule : ScoreAff = (1.0 + MutualFriendsCount * ω) * CommunityMultiplier.
func ComputeAffinityScore(mutualFriendsCount int, omega float64, communityMultiplier float64) float64 {
	if mutualFriendsCount < 0 {
		mutualFriendsCount = 0
	}
	if omega < 0 || math.IsNaN(omega) || math.IsInf(omega, 0) {
		omega = 0
	}
	if communityMultiplier <= 0 || math.IsNaN(communityMultiplier) || math.IsInf(communityMultiplier, 0) {
		communityMultiplier = 1.0
	}
	return (1.0 + float64(mutualFriendsCount)*omega) * communityMultiplier
}

// ============================================================================
// 2.4. GEOLOCATION SCORE (PROXIMITÉ SPATIALE & RESPECT DE LA VIE PRIVÉE)
// ============================================================================

// HaversineDistanceKm calcule la distance orthodromique (en kilomètres) entre deux coordonnées GPS.
func HaversineDistanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)
	rLat1 := lat1 * (math.Pi / 180.0)
	rLat2 := lat2 * (math.Pi / 180.0)

	sinDLat := math.Sin(dLat / 2.0)
	sinDLon := math.Sin(dLon / 2.0)

	a := sinDLat*sinDLat + math.Cos(rLat1)*math.Cos(rLat2)*sinDLon*sinDLon
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}
	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))
	return earthRadiusKm * c
}

// ComputeGeoDecayFromDistanceKm applique une fonction de décroissance exponentielle déterministe :
// f_geo(distance) = exp(-distanceKm / scaleKm) dans ]0, 1], monotone décroissante.
func ComputeGeoDecayFromDistanceKm(distanceKm float64, scaleKm float64) float64 {
	if distanceKm < 0 || math.IsNaN(distanceKm) {
		return 0.0
	}
	if math.IsInf(distanceKm, 1) {
		return 0.0
	}
	if scaleKm <= 0 || math.IsNaN(scaleKm) || math.IsInf(scaleKm, 0) {
		scaleKm = 50.0
	}
	decay := math.Exp(-distanceKm / scaleKm)
	if math.IsNaN(decay) || math.IsInf(decay, 0) {
		return 0.0
	}
	return decay
}

// ParseLocationCoordinates extrait des coordonnées GPS (latitude, longitude) depuis une chaîne
// au format "lat,lon" (ex: "48.8566,2.3522") ou depuis le référentiel des villes connues.
func ParseLocationCoordinates(location string) (float64, float64, bool) {
	cleaned := strings.TrimSpace(location)
	if cleaned == "" {
		return 0, 0, false
	}

	parts := strings.Split(cleaned, ",")
	if len(parts) == 2 {
		lat, errLat := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		lon, errLon := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if errLat == nil && errLon == nil &&
			!math.IsNaN(lat) && !math.IsNaN(lon) &&
			lat >= -90.0 && lat <= 90.0 &&
			lon >= -180.0 && lon <= 180.0 {
			return lat, lon, true
		}
	}

	normalized := strings.ToLower(cleaned)
	if coords, found := knownCityCoordinates[normalized]; found {
		return coords[0], coords[1], true
	}
	// Gestion du format "Ville, Pays" (ex: "Paris, France")
	if len(parts) >= 1 {
		cityOnly := strings.ToLower(strings.TrimSpace(parts[0]))
		if coords, found := knownCityCoordinates[cityOnly]; found {
			return coords[0], coords[1], true
		}
	}

	return 0, 0, false
}

// ComputeGeoProximityFactor évalue f_geo(distance) en respectant les règles de confidentialité (ShowLocation).
// Retourne (f_geo, true) si les deux utilisateurs exposent une localisation exploitable, sinon (0, false).
func ComputeGeoProximityFactor(
	targetLocation string,
	targetShowLocation bool,
	candidateLocation string,
	candidateShowLocation bool,
	scaleKm float64,
) (float64, bool) {
	if !targetShowLocation || !candidateShowLocation {
		return 0.0, false
	}
	tClean := strings.TrimSpace(targetLocation)
	cClean := strings.TrimSpace(candidateLocation)
	if tClean == "" || cClean == "" {
		return 0.0, false
	}

	lat1, lon1, ok1 := ParseLocationCoordinates(tClean)
	lat2, lon2, ok2 := ParseLocationCoordinates(cClean)
	if ok1 && ok2 {
		distKm := HaversineDistanceKm(lat1, lon1, lat2, lon2)
		return ComputeGeoDecayFromDistanceKm(distKm, scaleKm), true
	}

	// Fallback déterministe par correspondance de ville / région textuelle normalisée
	tNorm := strings.ToLower(tClean)
	cNorm := strings.ToLower(cClean)
	if tNorm == cNorm {
		return 1.0, true
	}

	tParts := strings.Split(tNorm, ",")
	cParts := strings.Split(cNorm, ",")
	if len(tParts) > 1 && len(cParts) > 1 {
		tRegion := strings.TrimSpace(tParts[len(tParts)-1])
		cRegion := strings.TrimSpace(cParts[len(cParts)-1])
		if tRegion != "" && tRegion == cRegion {
			return 0.5, true
		}
	}

	return 0.1, true
}

// ComputeGeoScore calcule : ScoreGeo = f_geo(distance) * CommunityMultiplier.
func ComputeGeoScore(proximityFactor float64, communityMultiplier float64) float64 {
	if proximityFactor <= 0 || math.IsNaN(proximityFactor) || math.IsInf(proximityFactor, 0) {
		return 0.0
	}
	if communityMultiplier <= 0 || math.IsNaN(communityMultiplier) || math.IsInf(communityMultiplier, 0) {
		communityMultiplier = 1.0
	}
	return proximityFactor * communityMultiplier
}

// ============================================================================
// PHASE 3 — AGRÉGATION, TRI TOP-K & INTERLEAVING ROUND-ROBIN
// ============================================================================

// ScoredCandidate regroupe les trois scores calculés pour un utilisateur candidat en Phase 2.
type ScoredCandidate struct {
	UserID              int64
	GeoScore            float64
	HasGeo              bool
	AffScore            float64
	TasteScore          float64
	HasTaste            bool
	CommunityMultiplier float64
}

// sortEntriesDescending trie une liste d'entrées par Score décroissant puis par UserID croissant (déterminisme strict).
func sortEntriesDescending(entries []cache_service.UserRecoScoredEntry, topK int) []cache_service.UserRecoScoredEntry {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score == entries[j].Score {
			return entries[i].UserID < entries[j].UserID
		}
		return entries[i].Score > entries[j].Score
	})
	if topK > 0 && len(entries) > topK {
		entries = entries[:topK]
	}
	return entries
}

// BuildCategoryRankings extrait et trie les Top-K (150) candidats pour chacune des 3 catégories : Geo, Affinity, Taste.
func BuildCategoryRankings(
	candidates []ScoredCandidate,
	topK int,
) (
	geoList []cache_service.UserRecoScoredEntry,
	affList []cache_service.UserRecoScoredEntry,
	tasteList []cache_service.UserRecoScoredEntry,
) {
	if topK <= 0 {
		topK = variables.UserRecoTopK
	}
	geoList = make([]cache_service.UserRecoScoredEntry, 0, len(candidates))
	affList = make([]cache_service.UserRecoScoredEntry, 0, len(candidates))
	tasteList = make([]cache_service.UserRecoScoredEntry, 0, len(candidates))

	for _, c := range candidates {
		if c.UserID <= 0 {
			continue
		}
		if c.HasGeo && c.GeoScore > 0 {
			geoList = append(geoList, cache_service.UserRecoScoredEntry{
				UserID: c.UserID,
				Score:  c.GeoScore,
			})
		}
		if c.AffScore > 0 {
			affList = append(affList, cache_service.UserRecoScoredEntry{
				UserID: c.UserID,
				Score:  c.AffScore,
			})
		}
		if c.HasTaste {
			tasteList = append(tasteList, cache_service.UserRecoScoredEntry{
				UserID: c.UserID,
				Score:  c.TasteScore,
			})
		}
	}

	geoList = sortEntriesDescending(geoList, topK)
	affList = sortEntriesDescending(affList, topK)
	tasteList = sortEntriesDescending(tasteList, topK)
	return geoList, affList, tasteList
}

// InterleaveGlobalRecommendations construit la liste globale (user:reco:list:global:{user_id})
// par entrelacement Round-Robin strict :
// Geo #1 -> Affinity #1 -> Taste #1 -> Geo #2 -> Affinity #2 -> Taste #2 ...
// Déduplique les candidats, préserve l'ordre déterministe et plafonne à maxGlobal (150) utilisateurs uniques.
func InterleaveGlobalRecommendations(
	geoList []cache_service.UserRecoScoredEntry,
	affList []cache_service.UserRecoScoredEntry,
	tasteList []cache_service.UserRecoScoredEntry,
	maxGlobal int,
) []cache_service.UserRecoScoredEntry {
	if maxGlobal <= 0 {
		maxGlobal = variables.UserRecoTopK
	}

	maxLen := len(geoList)
	if len(affList) > maxLen {
		maxLen = len(affList)
	}
	if len(tasteList) > maxLen {
		maxLen = len(tasteList)
	}

	seen := make(map[int64]struct{}, maxGlobal)
	orderedIDs := make([]int64, 0, maxGlobal)

	for i := 0; i < maxLen && len(orderedIDs) < maxGlobal; i++ {
		if i < len(geoList) {
			uID := geoList[i].UserID
			if uID > 0 {
				if _, exists := seen[uID]; !exists {
					seen[uID] = struct{}{}
					orderedIDs = append(orderedIDs, uID)
					if len(orderedIDs) >= maxGlobal {
						break
					}
				}
			}
		}
		if i < len(affList) {
			uID := affList[i].UserID
			if uID > 0 {
				if _, exists := seen[uID]; !exists {
					seen[uID] = struct{}{}
					orderedIDs = append(orderedIDs, uID)
					if len(orderedIDs) >= maxGlobal {
						break
					}
				}
			}
		}
		if i < len(tasteList) {
			uID := tasteList[i].UserID
			if uID > 0 {
				if _, exists := seen[uID]; !exists {
					seen[uID] = struct{}{}
					orderedIDs = append(orderedIDs, uID)
					if len(orderedIDs) >= maxGlobal {
						break
					}
				}
			}
		}
	}

	globalList := make([]cache_service.UserRecoScoredEntry, len(orderedIDs))
	total := len(orderedIDs)
	for rankIdx, uID := range orderedIDs {
		// Score décroissant strict par rang pour que ZREVRANGE restitue exactement l'ordre Round-Robin
		globalList[rankIdx] = cache_service.UserRecoScoredEntry{
			UserID: uID,
			Score:  float64(total - rankIdx),
		}
	}

	return globalList
}
