package variables

import "time"

var (
	// Ratios de mixage du Feed (Modifiables à chaud)
	SocialRatio = 0.3 // 30% d'abonnements/amis
	TagRatio    = 0.5 // 50% d'affinités thématiques (Graphe 1-Hop)
	GlobalRatio = 0.2 // 20% de sérendipité mondiale pure

	FeedReloadDelay = 30 * time.Minute // Temps avant d'autoriser une vraie nouvelle génération
	FeedPageSize    = 50               // Nombre de posts renvoyés par scroll

	// Paramètres du Worker Feed Cache
	FeedWarmupCronInterval       = 5 * time.Minute // Fréquence d'analyse des flux expirés
	FeedWarmupBatchSize    int64 = 500             // Nombre maximum d'utilisateurs réhydratés par cycle
	FanOutVIPThreshold     int64 = 50000           // Limite d'abonnés déclenchant le coupe-circuit "Justin Bieber"
	FanOutRetentionLimit         = -501            // Purge ZSET (ZREMRANGEBYRANK) pour conserver les 500 derniers posts

	// Garde-fous anti-saturation de la Mailbox & diversité par auteur (Modifiables à chaud)
	FanOutMaxPostsPerAuthorPerDay = 5 // Nombre max de posts d'un même auteur distribués en Fan-Out sur 24h glissantes
	MailboxMaxPostsPerAuthor      = 3 // Nombre max de posts d'un même auteur extraits de la Mailbox par panier
	FeedMaxPostsPerAuthor         = 3 // Nombre max de posts d'un même auteur par lot final généré (Caissière / MMR)
)
