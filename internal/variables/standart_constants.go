package variables

const (
	// Grades utilisateurs (Énumérations BDD immuables)
	UserGradeUser      = 0
	UserGradeCertified = 1
	UserGradeParter    = 2
	UserGradeModerator = 3
	UserGradeAdmin     = 4

	// Borne mathématique 64 bits
	MaxTimestamp int64 = 1<<63 - 1

	// Base de données MongoDB L2 unifiée
	MongoDatabaseName = "numan_mongo"
)

var (
	FetchBatchLimit              = 50
	ToleranceTimeSeconds         = 300     // 5 minutes (Ratchet)
	JWTExpirationSeconds         = 900     // 15 minutes
	MasterTokenExpirationSeconds = 2592000 // 1 mois en secondes
)
