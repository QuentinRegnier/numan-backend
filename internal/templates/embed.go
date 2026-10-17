package templates

import "embed"

// EmailFS monte l'intégralité du dossier "emails" en RAM lors de la compilation.
// Cela garantit des lectures instantanées (I/O) en production et supprime le besoin de volumes K8s.
//
//go:embed emails/*.html
var EmailFS embed.FS
