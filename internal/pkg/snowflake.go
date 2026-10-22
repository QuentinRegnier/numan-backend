package pkg

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/variables"
)

var generator *node // La variable globale privée (Singleton)

// InitSnowflake : Tu l'appelles une fois au démarrage dans le main
func InitSnowflake(nodeID int64) error {
	var err error
	generator, err = newNode(nodeID)
	return err
}

// GenerateID : LA fameuse fonction simple que tu voulais
func GenerateID() int64 {
	return generator.generate()
}

// node est la structure qui génère les IDs
type node struct {
	mu        sync.Mutex
	timestamp int64
	nodeID    int64
	step      int64
}

// newNode crée une nouvelle instance de générateur Snowflake.
// nodeID : Un identifiant unique pour ce serveur (entre 0 et 1023).
func newNode(nodeID int64) (*node, error) {
	if nodeID < 0 || nodeID > variables.NodeMax {
		// Encapsulation de l'erreur d'initialisation système sans exposer les variables internes au client
		return nil, numan_error.NewAppError(
			http.StatusInternalServerError,
			numan_error.CodeInternalError,
			"Impossible d'initialiser le générateur d'identifiants.",
			fmt.Errorf("nodeID %d invalide (doit être compris entre 0 et %d)", nodeID, variables.NodeMax),
		)
	}

	return &node{
		timestamp: 0,
		nodeID:    nodeID,
		step:      0,
	}, nil
}

// generate crée et retourne un nouvel ID unique (int64).
func (n *node) generate() int64 {
	n.mu.Lock()         // Verrouille pour éviter que deux goroutines accèdent en même temps
	defer n.mu.Unlock() // Déverrouille à la fin de la fonction

	now := time.Now().UnixMilli() // Temps actuel en millisecondes

	if now < n.timestamp {
		// Protection contre le changement d'heure système (NTP drift)
		// On attend que l'horloge rattrape le dernier timestamp
		for now <= n.timestamp {
			now = time.Now().UnixMilli()
		}
	}

	if n.timestamp == now {
		// Si on est dans la même milliseconde, on incrémente la séquence
		n.step = (n.step + 1) & variables.StepMax

		// Si la séquence déborde (plus de 4096 IDs dans la même ms), on attend la ms suivante
		if n.step == 0 {
			for now <= n.timestamp {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		// Nouvelle milliseconde : on remet la séquence à zéro
		n.step = 0
	}

	n.timestamp = now

	// Construction de l'ID final par opérations binaires (Bitwise OR)
	// 1. (now - epoch) << timeShift : On place le temps tout à gauche
	// 2. (nodeID << nodeShift)      : On place l'ID du noeud au milieu
	// 3. (n.step)                   : On place la séquence à la fin
	id := ((now - variables.Epoch) << variables.TimeShift) | (n.nodeID << variables.NodeShift) | (n.step)

	return id
}
