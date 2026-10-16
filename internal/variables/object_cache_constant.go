package variables

import "time"

// Configuration temporelle et limites OBJECT Cache (Modifiables à chaud)
var (
	StandardTTL        = 7 * 24 * time.Hour // TTL STANDARD : 7 Jours
	MaxZsetPostComment = 100
)
