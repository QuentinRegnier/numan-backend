package variables

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUserRecoConstantsAndRegistry(t *testing.T) {
	resetAllVariablesForTest()
	defer resetAllVariablesForTest()

	if RedisKeyUserRecoListGeo != "user:reco:list:geo" ||
		RedisKeyUserRecoListAff != "user:reco:list:aff" ||
		RedisKeyUserRecoListTaste != "user:reco:list:taste" ||
		RedisKeyUserRecoListGlobal != "user:reco:list:global" ||
		RedisKeyUsersLang != "users:lang" ||
		RedisKeyCuckooUserRecoSkips != "cuckoo:user_reco:skips" ||
		RedisKeyLockUserRecoCompute != "lock:user_reco:compute" ||
		MongoCollUserRecoSkips != "user_reco_skips" {
		t.Fatal("invariants de clés Redis/Mongo incorrects")
	}

	if UserRecoCuckooCapacity != 3000 || UserRecoCuckooFalsePositive != 0.01 || UserRecoSkipTTLSeconds != 604800 {
		t.Fatal("invariants Cuckoo/TTL incorrects")
	}

	if UserRecoDefaultLimit != 20 || UserRecoMinLimit != 1 || UserRecoMaxLimit != 50 {
		t.Fatal("invariants de pagination incorrects")
	}

	patch := map[string]json.RawMessage{
		"UserRecoMutualFriendWeight": json.RawMessage(`0.5`),
		"UserRecoGeoDecayScaleKm":    json.RawMessage(`75.0`),
		"UserRecoMaxCandidates":      json.RawMessage(`3000`),
		"UserRecoTopK":               json.RawMessage(`150`),
		"UserRecoComputeLockTTL":     json.RawMessage(`"12h"`),
		"UserRecoCronInterval":       json.RawMessage(`"36h"`),
		"UserRecoCronBatchSize":      json.RawMessage(`250`),
	}

	prep, err := ValidateAndPreparePatch(patch)
	if err != nil {
		t.Fatalf("ValidateAndPreparePatch a échoué : %v", err)
	}
	ApplyPreparedPatchInMemory(prep, 42)

	if UserRecoMutualFriendWeight != 0.5 || UserRecoGeoDecayScaleKm != 75.0 || UserRecoMaxCandidates != 3000 || UserRecoCronInterval != 36*time.Hour || UserRecoCronBatchSize != 250 {
		t.Fatal("les variables dynamiques UserReco n'ont pas été correctement mises à jour")
	}

	// Test de rejet sur valeur invalide
	_, errInvalid := ValidateAndPreparePatch(map[string]json.RawMessage{
		"UserRecoGeoDecayScaleKm": json.RawMessage(`0`),
	})
	if errInvalid == nil {
		t.Fatal("attendu rejet pour UserRecoGeoDecayScaleKm <= 0")
	}
}
