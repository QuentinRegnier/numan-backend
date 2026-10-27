package variables

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

func resetAllVariablesForTest() {
	_ = ApplyRemoteState(map[string]string{}, 0)
}

func TestRegistry_DefaultValuesAndPartialPatch(t *testing.T) {
	resetAllVariablesForTest()
	defer resetAllVariablesForTest()

	if FeedPageSize != 50 {
		t.Fatalf("attendu FeedPageSize=50 par défaut, obtenu %d", FeedPageSize)
	}

	// Modification partielle de 2 variables uniquement (en PascalCase et UPPER_SNAKE_CASE)
	patch := map[string]json.RawMessage{
		"FeedPageSize":             json.RawMessage(`120`),
		"MONGO_BULK_WRITE_ORDERED": json.RawMessage(`true`),
		"FeedReloadDelay":          json.RawMessage(`"45m"`),
	}

	prepared, err := ValidateAndPreparePatch(patch)
	if err != nil {
		t.Fatalf("ValidateAndPreparePatch a échoué : %v", err)
	}

	ApplyPreparedPatchInMemory(prepared, 1)

	if FeedPageSize != 120 {
		t.Errorf("attendu FeedPageSize=120, obtenu %d", FeedPageSize)
	}
	if !MongoBulkWriteOrdered {
		t.Errorf("attendu MongoBulkWriteOrdered=true, obtenu false")
	}
	if FeedReloadDelay != 45*time.Minute {
		t.Errorf("attendu FeedReloadDelay=45m, obtenu %v", FeedReloadDelay)
	}
	// Vérifie que les autres variables ont conservé leur valeur par défaut
	if MaxPinConversation != 3 {
		t.Errorf("attendu MaxPinConversation=3 intact, obtenu %d", MaxPinConversation)
	}
}

func TestRegistry_RejectUnknownAndImmutableVariables(t *testing.T) {
	resetAllVariablesForTest()
	defer resetAllVariablesForTest()

	// 1. Clé totalement inconnue
	_, errUnknown := ValidateAndPreparePatch(map[string]json.RawMessage{
		"VARIABLE_INEXISTANTE": json.RawMessage(`42`),
	})
	if errUnknown == nil {
		t.Fatal("attendu une erreur pour une variable inconnue, obtenu nil")
	}

	// 2. Constante immuable de schéma/dimension (VectorDimTotal ne doit pas être modifiable)
	_, errImmutable := ValidateAndPreparePatch(map[string]json.RawMessage{
		"VectorDimTotal": json.RawMessage(`512`),
	})
	if errImmutable == nil {
		t.Fatal("attendu une erreur pour une constante structurelle immuable, obtenu nil")
	}
}

func TestRegistry_StrictTypeValidationAndAtomicity(t *testing.T) {
	resetAllVariablesForTest()
	defer resetAllVariablesForTest()

	invalidCases := []struct {
		name  string
		patch map[string]json.RawMessage
	}{
		{
			name: "String au lieu de int",
			patch: map[string]json.RawMessage{
				"FeedPageSize": json.RawMessage(`"100"`),
			},
		},
		{
			name: "Float décimal au lieu de int",
			patch: map[string]json.RawMessage{
				"FeedPageSize": json.RawMessage(`50.5`),
			},
		},
		{
			name: "String au lieu de bool",
			patch: map[string]json.RawMessage{
				"MongoBulkWriteOrdered": json.RawMessage(`"true"`),
			},
		},
		{
			name: "Valeur null",
			patch: map[string]json.RawMessage{
				"FeedPageSize": json.RawMessage(`null`),
			},
		},
		{
			name: "Durée négative ou nulle (protection anti-panic Ticker)",
			patch: map[string]json.RawMessage{
				"FeedReloadDelay": json.RawMessage(`"0s"`),
			},
		},
		{
			name: "Ratio hors bornes [0, 1]",
			patch: map[string]json.RawMessage{
				"SocialRatio": json.RawMessage(`1.5`),
			},
		},
		{
			name: "Batch mixte : 1 valide + 1 invalide (doit rejeter tout le lot)",
			patch: map[string]json.RawMessage{
				"FeedPageSize":    json.RawMessage(`80`),
				"MaxMediaPerPost": json.RawMessage(`"invalide"`),
			},
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateAndPreparePatch(tc.patch)
			if err == nil {
				t.Errorf("le cas '%s' aurait dû être rejeté", tc.name)
			}
			// Vérifie que FeedPageSize n'a pas été modifié par le batch mixte
			if FeedPageSize != 50 {
				t.Errorf("violation d'atomicité : FeedPageSize a muté à %d", FeedPageSize)
			}
		})
	}
}

func TestRegistry_ResetToDefaultAndMultiServerSync(t *testing.T) {
	resetAllVariablesForTest()
	defer resetAllVariablesForTest()

	// Simulation d'un état Redis provenant d'un autre serveur (Version 5)
	remoteOverrides := map[string]string{
		"FeedPageSize":  `200`,
		"TDDWeightLike": `2.5`,
	}
	if err := ApplyRemoteState(remoteOverrides, 5); err != nil {
		t.Fatalf("ApplyRemoteState a échoué : %v", err)
	}

	if FeedPageSize != 200 || TDDWeightLike != 2.5 || GetVersion() != 5 {
		t.Fatalf("état après synchronisation incorrect : FeedPageSize=%d, TDDWeightLike=%v, Version=%d", FeedPageSize, TDDWeightLike, GetVersion())
	}

	// Si l'admin remet FeedPageSize à sa valeur par défaut (50), ToDelete doit contenir "FeedPageSize"
	prepared, err := ValidateAndPreparePatch(map[string]json.RawMessage{
		"FeedPageSize": json.RawMessage(`50`),
	})
	if err != nil {
		t.Fatalf("ValidateAndPreparePatch a échoué : %v", err)
	}
	if len(prepared.ToDelete) != 1 || prepared.ToDelete[0] != "FeedPageSize" {
		t.Fatalf("attendu FeedPageSize dans ToDelete, obtenu %+v", prepared.ToDelete)
	}

	// Convergence lorsque l'override est retiré de Redis (Version 6)
	_ = ApplyRemoteState(map[string]string{
		"TDDWeightLike": `2.5`,
	}, 6)

	_, details, ver := GetSnapshot()
	if ver != 6 || FeedPageSize != 50 || details["FeedPageSize"].IsCustom {
		t.Errorf("FeedPageSize aurait dû redevenir non-custom (50), obtenu val=%d, isCustom=%v", FeedPageSize, details["FeedPageSize"].IsCustom)
	}
}

func TestRegistry_ConcurrentReadsAndWrites(t *testing.T) {
	resetAllVariablesForTest()
	defer resetAllVariablesForTest()

	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Add(2)
		version := int64(i)
		pageSize := 50 + i

		go func(v int64, sz int) {
			defer wg.Done()
			patch := map[string]json.RawMessage{
				"FeedPageSize": json.RawMessage(fmt.Sprintf("%d", sz)),
			}
			if prep, err := ValidateAndPreparePatch(patch); err == nil {
				ApplyPreparedPatchInMemory(prep, v)
			}
		}(version, pageSize)

		go func() {
			defer wg.Done()
			vals, _, _ := GetSnapshot()
			if len(vals) == 0 {
				t.Error("snapshot vide")
			}
		}()
	}
	wg.Wait()
}

func TestRegistry_TrustScoreVariablesValidation(t *testing.T) {
	resetAllVariablesForTest()
	defer resetAllVariablesForTest()

	if TrustScoreInitial != 0.20 || TrustThresholdStandard != 0.40 || TrustThresholdVIP != 0.80 {
		t.Fatalf("valeurs initiales TrustScore inattendues: init=%v, std=%v, vip=%v", TrustScoreInitial, TrustThresholdStandard, TrustThresholdVIP)
	}

	// Patch valide
	prep, err := ValidateAndPreparePatch(map[string]json.RawMessage{
		"TrustThresholdStandard": json.RawMessage(`0.45`),
	})
	if err != nil {
		t.Fatalf("attendu patch valide, erreur: %v", err)
	}
	ApplyPreparedPatchInMemory(prep, 10)
	if TrustThresholdStandard != 0.45 {
		t.Fatalf("patch TrustScore mal appliqué")
	}

	// Patch valide sur les poids IA, signalement, gouvernance et ELO
	prepAI, errAI := ValidateAndPreparePatch(map[string]json.RawMessage{
		"ModerationWeightIA":               json.RawMessage(`2.5`),
		"ModerationWeightSignalMsg":        json.RawMessage(`3.5`),
		"AIAutoReportThreshold":            json.RawMessage(`0.60`),
		"AIAutoReportMessageThreshold":     json.RawMessage(`0.68`),
		"ModerationAdminDelegationEnabled": json.RawMessage(`true`),
		"SuperModeratorTrustThreshold":     json.RawMessage(`0.92`),
		"BlameQuotaForAudit":               json.RawMessage(`4`),
		"ModerationYouthBonusMax":          json.RawMessage(`6.0`),
		"TrustScoreInitialVIP":             json.RawMessage(`0.88`),
	})
	if errAI != nil {
		t.Fatalf("attendu patch IA/ gouvernance valide, erreur: %v", errAI)
	}
	ApplyPreparedPatchInMemory(prepAI, 11)
	if ModerationWeightIA != 2.5 || ModerationWeightSignalMsg != 3.5 || AIAutoReportThreshold != 0.60 || !ModerationAdminDelegationEnabled || SuperModeratorTrustThreshold != 0.92 || BlameQuotaForAudit != 4 || ModerationYouthBonusMax != 6.0 || TrustScoreInitialVIP != 0.88 {
		t.Fatalf("patch IA / gouvernance mal appliqué")
	}

	// Patch invalide (hors bornes [0.0, 1.0])
	_, errInvalid := ValidateAndPreparePatch(map[string]json.RawMessage{
		"TrustScoreInitial": json.RawMessage(`1.50`),
	})
	if errInvalid == nil {
		t.Fatal("attendu un rejet pour TrustScoreInitial > 1.0")
	}
}
