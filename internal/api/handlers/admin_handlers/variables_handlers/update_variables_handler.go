package variables_handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/QuentinRegnier/numan-backend/internal/domain/models/variables_models"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/pkg"
	"github.com/QuentinRegnier/numan-backend/internal/service/variables_service"
	"github.com/gin-gonic/gin"
)

// UpdateVariablesHandler godoc
// @Summary      Modifier partiellement les variables dynamiques de l'application
// @Description  Permet à un administrateur de modifier à chaud une ou plusieurs variables dynamiques sans redémarrer les serveurs. Les modifications sont validées de manière atomique, persistées dans Redis et propagées en temps réel à toutes les instances de l'API.
// @Description
// @Description  **Authentication & Authorization:**
// @Description  - Authentication requirements: Route sécurisée. Requiert un JWT valide et une signature HMAC valide calculée sur le corps JSON.
// @Description  - Required permissions or roles: Le `userID` extrait du contexte doit correspondre à un compte actif possédant strictement le grade Administrateur (`UserGradeAdmin = 4`).
// @Description  - Relevant middleware: TraceIDMiddleware, CustomRecoveryMiddleware, JWTMiddleware, HMACMiddleware, MaxBodySize, RateLimiter, CORS.
// @Description
// @Description  **Request Contract:**
// @Description  - Required fields: Un objet JSON non vide (`variables_models.UpdateVariablesInput`) où chaque clé est le nom d'une variable dynamique autorisée (`PascalCase` ou `UPPER_SNAKE_CASE`) et chaque valeur sa nouvelle valeur typée.
// @Description  - Validation rules: Décodage JSON strict (`UseNumber()`), interdiction des clés inconnues ou des constantes immuables, interdiction des doublons d'alias dans la même requête, interdiction de `null`, validation stricte du type Go sans coercition implicite et validation des bornes métier (ex: durées `> 0`, ratios dans `[0.0, 1.0]`).
// @Description
// @Description  **Execution Workflow:**
// @Description  1. Extraction et validation du `callerID` depuis le contexte Gin sécurisé.
// @Description  2. Lecture et décodage strict du corps JSON en `variables_models.UpdateVariablesInput` (`map[string]json.RawMessage`).
// @Description  3. Contrôle d'accès Zero-Trust (`security_service.LeftAdmin`) en cascade L1 (`SpeedCache`) -> L2 (MongoDB) -> L3 (PostgreSQL).
// @Description  4. Phase 1 (Validation 2PC en mémoire via `variables.ValidateAndPreparePatch`) : vérification de l'existence de chaque variable dans le registre, détection des collisions d'alias, validation stricte du type et des contraintes métier. Si une seule entrée est invalide, toute la requête est rejetée sans aucun effet de bord.
// @Description  5. Distinction des valeurs par défaut : les nouvelles valeurs égales à la `DefaultValue` du code sont marquées pour suppression (`HDEL`) dans Redis, tandis que les valeurs personnalisées sont marquées pour insertion/mise à jour (`HSET`).
// @Description  6. Phase 2 (Persistance atomique via `cache_service.ApplyAndBroadcastDynamicVariablesPatch`) : exécution du script Lua `applyVariablesPatchScript` qui applique tous les `HSET`/`HDEL` sur `system:variables:overrides` et incrémente `system:variables:version` en une seule transaction Redis.
// @Description  7. Diffusion temps réel inter-serveurs via le pattern Claim-Check Pub/Sub (`DynamicVariablesSync.PushFlux`) pour notifier toutes les instances Go.
// @Description  8. Synchronisation immédiate de la RAM locale de l'instance courante (et exécution des hooks dynamiques comme `RefreshStandardTTL` si `StandardTTL` a changé).
// @Description  9. Journalisation d'audit sécurisée (`numan_log.Info`) enregistrant uniquement la liste des noms de clés modifiées et la nouvelle version (sans exposer les valeurs dans les logs).
// @Description
// @Description  **Success Behavior:**
// @Description  - HTTP status: 200 OK
// @Description  - Response body: JSON `variables_models.UpdateVariablesOutput` contenant le message de confirmation, le nouveau numéro de `version` et la map `updated_variables`.
// @Description  - Persistence guarantees: Persistance synchrone et atomique dans Redis (AOF actif, collection sans TTL protégée de l'éviction `volatile-lfu`).
// @Description  - Side effects: Mise à jour immédiate de la RAM locale, broadcast Redis Pub/Sub vers les autres pods API/Workers, rafraîchissement des `DefaultTTL` des collections Redis si `StandardTTL` fait partie du patch.
// @Description
// @Description  **Error Responses & Reproduction Conditions:**
// @Description
// @Description  🔴 **400 Bad Request:**
// @Description
// @Description  - **[INVALID_USER_ID] Malformed user ID in context:**
// @Description    - Trigger: L'identifiant utilisateur stocké sous forme de chaîne dans le contexte ne peut pas être converti en `int64`.
// @Description    - Execution stage: Extraction de l'identité (`pkg.GetUserIDFromContext`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `"INVALID_USER_ID"` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  - **[INVALID_PAYLOAD] Malformed JSON, unknown variable, duplicate alias, or type/constraint violation:**
// @Description    - Trigger: Corps de requête vide ou non-JSON, objet JSON vide `{}`, clé inconnue ou constante immuable (ex: `VectorDimTotal`), collision d'alias (ex: `FeedPageSize` et `FEED_PAGE_SIZE` dans le même appel), valeur `null`, type JSON incompatible (ex: `"100"` ou `12.5` pour un `int`), ou violation des règles métier (ex: durée `<= 0s`, qualité AVIF hors de `[1, 100]`).
// @Description    - Execution stage: Décodage du body dans le Handler ou Phase 1 de validation (`variables.ValidateAndPreparePatch`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInvalidPayload` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🔐 **401 Unauthorized:**
// @Description
// @Description  - **[UNAUTHORIZED] Invalid or zero caller ID:**
// @Description    - Trigger: Le JWT ou la signature HMAC est invalide/absente, ou le `callerID` extrait est inférieur ou égal à `0`.
// @Description    - Execution stage: Middleware d'authentification / Contrôle initial `LeftAdmin`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeUnauthorized` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⛔ **403 Forbidden:**
// @Description
// @Description  - **[UNAUTHORIZED] Missing user in context:**
// @Description    - Trigger: La clé `userID` est absente du contexte Gin.
// @Description    - Execution stage: Extraction de l'identité (`pkg.GetUserIDFromContext`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `"UNAUTHORIZED"` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  - **[FORBIDDEN_ACCESS] Insufficient privileges or suspended account:**
// @Description    - Trigger: L'utilisateur appelant possède un grade inférieur à `UserGradeAdmin` (`4`), ou son compte est banni/désactivé.
// @Description    - Execution stage: Contrôle d'autorisation métier (`security_service.LeftAdmin`).
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeForbidden` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  🟠 **404 Not Found:**
// @Description
// @Description  - **[RESOURCE_NOT_FOUND] Administrator account does not exist:**
// @Description    - Trigger: Le `callerID` est introuvable en RAM (L1), dans MongoDB (L2) et dans PostgreSQL (L3).
// @Description    - Execution stage: Recherche en cascade dans `security_service.LeftAdmin`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeNotFound` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Description
// @Description  ⚫ **500 Internal Server Error:**
// @Description
// @Description  - **[INTERNAL_SERVER_ERROR] Redis persistence failure or L3 database failure:**
// @Description    - Trigger: Redis est injoignable ou l'exécution du script Lua atomique échoue (la modification est alors refusée pour éviter tout split-brain entre instances), ou PostgreSQL échoue lors du fallback `LeftAdmin`.
// @Description    - Execution stage: Persistance atomique Redis (`cache_service.ApplyAndBroadcastDynamicVariablesPatch`) / Fallback L3 `LeftAdmin`.
// @Description    - Response: `numan_error.PublicErrorResponse`.
// @Description    - Error code: `numan_error.CodeInternalError` (returned in the `code` field of `numan_error.PublicErrorResponse`).
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        Authorization header string true "Bearer <votre_jwt>"
// @Param        X-Signature   header string true "Signature HMAC de la requête"
// @Param        X-Timestamp   header string true "Timestamp Unix de la requête"
// @Param        input         body   variables_models.UpdateVariablesInput true "Objet JSON partiel des variables à modifier (ex: {\"FeedPageSize\": 100, \"MongoBulkWriteOrdered\": true, \"FeedReloadDelay\": \"15m\"})"
// @Success      200  {object}  variables_models.UpdateVariablesOutput
// @Failure      400  {object}  numan_error.PublicErrorResponse "Invalid Payload (Unknown variable, type mismatch, or constraint error)"
// @Failure      401  {object}  numan_error.PublicErrorResponse "Unauthorized"
// @Failure      403  {object}  numan_error.PublicErrorResponse "Forbidden (Admin grade required)"
// @Failure      404  {object}  numan_error.PublicErrorResponse "Admin User Not Found"
// @Failure      500  {object}  numan_error.PublicErrorResponse "Internal Server Error"
// @Router       /variables/update [patch]
func UpdateVariablesHandler(c *gin.Context) {
	callerID, err := pkg.GetUserIDFromContext(c)
	if err != nil {
		numan_error.RespondWithError(c, err)
		return
	}

	bodyBytes, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Impossible de lire le corps de la requête.", errRead))
		return
	}

	trimmed := bytes.TrimSpace(bodyBytes)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Le corps de la requête doit être un objet JSON valide.", nil))
		return
	}

	var input variables_models.UpdateVariablesInput
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if errDecode := decoder.Decode(&input); errDecode != nil || decoder.More() {
		numan_error.RespondWithError(c, numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Format JSON invalide.", errDecode))
		return
	}

	output, errUpdate := variables_service.UpdateVariables(c.Request.Context(), callerID, input)
	if errUpdate != nil {
		numan_error.RespondWithError(c, errUpdate)
		return
	}

	c.JSON(http.StatusOK, output)
}
