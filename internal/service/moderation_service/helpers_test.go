package moderation_service_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/moderation_models"
	"github.com/QuentinRegnier/numan-backend/internal/service/moderation_service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToPublicTicketView_NonAdmin_PeerReviewMasking(t *testing.T) {
	now := time.Now().UnixMilli()
	record := moderation_models.ModerationTicketRecord{
		ReportID:          12345,
		TargetType:        0,
		TargetID:          999,
		TargetOwnerID:     888,
		Category:          10,
		ReportOrigin:      0,
		State:             3, // ReportStatePendingPeerReview
		Reason:            "Contenu haineux",
		PriorityScore:     12.5,
		IsInfiniteUrgent:  false,
		ClaimedByModID:    4001,
		ProposerModID:     4001,
		ProposerPseudonym: "mod_anon_a1b2c3d4e5f6",
		BlameMotivation:   "Infraction avérée aux CGU",
		AuditBlameRecords: []moderation_models.BlameRecord{
			{ReportID: 100, ProposerModID: 4001, ValidatorModID: 4002, Motivation: "Blame 1", CreatedAt: now},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	// 1. Vue pour modérateur non-administrateur (includeAdminAuditDetails = false)
	view := moderation_service.ToPublicTicketView(&record, false)

	// Vérifications sur la struct Go
	assert.Equal(t, int64(0), view.ClaimedByModID, "ClaimedByModID doit être forcé à 0 pour un non-admin en peer-review")
	assert.Nil(t, view.AuditBlameRecords, "AuditBlameRecords ne doit jamais être exposé aux non-admins")
	assert.Empty(t, view.ProposerPseudonym, "ProposerPseudonym doit être vide pour un non-admin")

	// Sérialisation JSON pour vérifier la réponse envoyée au client
	jsonBytes, err := json.Marshal(view)
	require.NoError(t, err)
	jsonStr := string(jsonBytes)

	// Assertions strictes sur le JSON
	assert.NotContains(t, jsonStr, "4001", "L'identifiant réel du modérateur proposant (4001) ne doit jamais être présent dans le JSON")
	assert.NotContains(t, jsonStr, "proposer_mod_id", "Le champ interne proposer_mod_id ne doit pas exister dans le JSON")
	assert.NotContains(t, jsonStr, "validator_mod_id", "Le champ validator_mod_id ne doit pas exister dans le JSON")
	assert.NotContains(t, jsonStr, "claimed_by_mod_id", "claimed_by_mod_id doit être omis du JSON via omitempty lorsqu'il vaut 0")
	assert.NotContains(t, jsonStr, "audit_blame_records", "audit_blame_records ne doit pas exister pour les non-admins")
	assert.NotContains(t, jsonStr, "proposer_pseudonym", "proposer_pseudonym doit être omis du JSON via omitempty")
	assert.NotContains(t, jsonStr, "mod_anon_", "aucun pseudonyme ne doit être divulgué")
}

func TestToPublicTicketView_Admin_AuditDetailsIncluded(t *testing.T) {
	now := time.Now().UnixMilli()
	record := moderation_models.ModerationTicketRecord{
		ReportID:          12345,
		TargetType:        0,
		TargetID:          999,
		TargetOwnerID:     888,
		Category:          10,
		ReportOrigin:      3, // System
		State:             4, // Escalated
		Reason:            "Audit 3 blâmes",
		ClaimedByModID:    5001,
		ProposerPseudonym: "Mod-Admin",
		AuditBlameRecords: []moderation_models.BlameRecord{
			{ReportID: 101, ProposerModID: 3001, ValidatorModID: 3002, Motivation: "Blame 1", CreatedAt: now},
			{ReportID: 102, ProposerModID: 3003, ValidatorModID: 3004, Motivation: "Blame 2", CreatedAt: now},
			{ReportID: 103, ProposerModID: 3001, ValidatorModID: 3005, Motivation: "Blame 3", CreatedAt: now},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Vue pour Administrateur (includeAdminAuditDetails = true)
	view := moderation_service.ToPublicTicketView(&record, true)

	assert.Equal(t, int64(5001), view.ClaimedByModID, "L'administrateur peut voir ClaimedByModID")
	require.Len(t, view.AuditBlameRecords, 3, "L'administrateur doit recevoir l'historique complet des 3 blâmes")
	assert.Equal(t, int64(3001), view.AuditBlameRecords[0].ProposerModID)
	assert.Equal(t, int64(3002), view.AuditBlameRecords[0].ValidatorModID)

	jsonBytes, err := json.Marshal(view)
	require.NoError(t, err)
	assert.Contains(t, string(jsonBytes), "audit_blame_records")
}
