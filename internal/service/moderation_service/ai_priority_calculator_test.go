package moderation_service

import (
	"context"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/infrastructure/test"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAIPriorityCalculator_MathematicalProperties(t *testing.T) {
	_ = test.SetupTestRedis(t)
	_ = pkg.InitSnowflake(1)
	ctx := context.Background()

	t.Run("AggregateMaxIAScore uses MAX across 1 to 4 images", func(t *testing.T) {
		maxScore := AggregateMaxIAScore(0.10, 0.20, 0.85, 0.40)
		assert.Equal(t, 0.85, maxScore)
	})

	t.Run("Single High-Trust reporter (0.95) outweighs 15 Low-Trust reporters (0.20) thanks to exp + log damping", func(t *testing.T) {
		// 15 comptes à bas TrustScore (0.20) signalent le post 1001
		var lowTrustPriority float64
		for i := int64(1); i <= 15; i++ {
			_, lowTrustPriority = RecordReporterAndComputePriority(
				ctx, variables.ReportTargetPost, 1001, 50, 0.50, 0.10, i, 0.20, 0.0,
			)
		}

		// 1 seul compte à très haut TrustScore (0.95) signale le post 1002 (mêmes caractéristiques)
		_, highTrustPriority := RecordReporterAndComputePriority(
			ctx, variables.ReportTargetPost, 1002, 50, 0.50, 0.10, 999, 0.95, 0.0,
		)

		assert.Greater(t, highTrustPriority, lowTrustPriority,
			"Un seul signaleur à haut TrustScore (0.95) doit peser nettement plus lourd que 15 signaleurs à bas TrustScore (0.20)")
	})

	t.Run("Author Trust Shield places impeccable author (0.95) much lower in queue than low-trust author (0.20)", func(t *testing.T) {
		// Mauvais signaleur (0.15) signale un auteur impeccable (0.95)
		_, prioTrustedAuthor := RecordReporterAndComputePriority(
			ctx, variables.ReportTargetPost, 2001, 88, 0.95, 0.10, 500, 0.15, 0.0,
		)
		// Même signaleur (0.15) signale un auteur à faible confiance (0.20)
		_, prioUntrustedAuthor := RecordReporterAndComputePriority(
			ctx, variables.ReportTargetPost, 2002, 89, 0.20, 0.10, 500, 0.15, 0.0,
		)

		assert.Less(t, prioTrustedAuthor, prioUntrustedAuthor*0.35,
			"L'auteur à haut TrustScore (0.95) doit se retrouver au moins 3x plus bas dans la file d'attente")
	})

	t.Run("Private Message report has higher priority than public Post report for same IA and Trust scores", func(t *testing.T) {
		_, postPrio := RecordReporterAndComputePriority(
			ctx, variables.ReportTargetPost, 3001, 60, 0.40, 0.40, 700, 0.80, 0.0,
		)
		_, msgPrio := RecordReporterAndComputePriority(
			ctx, variables.ReportTargetMessage, 3002, 60, 0.40, 0.40, 700, 0.80, 0.0,
		)
		assert.Greater(t, msgPrio, postPrio, "W_sig messagerie (3.0) doit remonter plus vite que W_sig post (1.5)")
	})

	t.Run("Auto-AI report triggers when combined risk exceeds threshold and transitions to Hybrid on user report", func(t *testing.T) {
		// Image P_ia = 0.60 publiée par un nouveau compte (T_auteur = 0.20)
		// R_auto = 0.60 * (1 + 0.50 * 0.80) = 0.84 >= 0.65 -> Signalement automatique émis !
		triggered, shouldQuarantine, autoPrio := EvaluateAndTriggerAutoReport(
			ctx, 42, 0.20, variables.ReportTargetPost, 4001, 0.60,
		)
		require.True(t, triggered)
		assert.True(t, shouldQuarantine)
		assert.Greater(t, autoPrio, 0.0)

		// Même image P_ia = 0.60 envoyée par un auteur VIP (T_auteur = 0.95)
		// R_auto = 0.60 * (1 + 0.50 * 0.05) = 0.615 < 0.65 -> Pas de faux positif !
		triggeredVIP, _, _ := EvaluateAndTriggerAutoReport(
			ctx, 99, 0.95, variables.ReportTargetPost, 4002, 0.60,
		)
		assert.False(t, triggeredVIP)

		// Si un utilisateur humain signale ensuite le post 4001, l'origine passe de AUTO_AI (1) à HYBRID (2)
		agg, newPrio := RecordReporterAndComputePriority(
			ctx, variables.ReportTargetPost, 4001, 42, 0.20, 0.60, 777, 0.85, 0.0,
		)
		assert.Equal(t, variables.ReportOriginHybrid, agg.ReportOrigin)
		assert.Greater(t, newPrio, autoPrio)
	})
}
