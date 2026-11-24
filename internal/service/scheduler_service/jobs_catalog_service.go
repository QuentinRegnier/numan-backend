package scheduler_service

import (
	"context"
	"time"
)

const trickleSleepDuration = 500 * time.Millisecond

// RegisterAllDefaultJobs enregistre l'intégralité du catalogue des 18 domaines de jobs planifiés
// (Catégorie A : 8 jobs hygiène, Catégorie B : 5 jobs BDD/Stockage, Catégorie C : 4 jobs algo/cache + 7 crons migrés).
func RegisterAllDefaultJobs(eng *Engine) {
	if eng == nil {
		return
	}
	registerCategoryAHygieneJobs(eng)
	registerCategoryBDatabaseJobs(eng)
	registerCategoryCAlgoAndMigratedJobs(eng)
}

func pauseIfTrickle(ctx context.Context, trickle bool) error {
	if !trickle {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(trickleSleepDuration):
		return nil
	}
}
