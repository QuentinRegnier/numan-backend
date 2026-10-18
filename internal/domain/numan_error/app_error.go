package numan_error

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError est notre erreur structurée de niveau GAFAM.
type AppError struct {
	HTTPStatus int    // Le code HTTP à renvoyer (ex: 404, 500)
	Code       string // Le code métier interne standardisé (ex: "USER_BANNED")
	Message    string // Le message public, "safe" pour l'utilisateur
	Err        error  // L'erreur originelle (interne) pour la stack trace et les logs
}

type Error *AppError

// Error implémente l'interface native 'error' de Go.
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s -> %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// unwrap permet au moteur Go de remonter la chaîne d'erreurs (Error Wrapping).
func (e *AppError) unwrap() error {
	return e.Err
}

// ############################################################################
// # DICTIONNAIRE UNIQUE DES CODES MÉTIERS (STRICTEMENT RÉSERVÉ)
// # Tout nouveau code doit être ajouté ici. Interdiction de "hardcoder" un string.
// ############################################################################
const (
	CodeInternalError         = "INTERNAL_SERVER_ERROR"
	CodeDatabaseError         = "DATABASE_ERROR"
	CodeCacheError            = "CACHE_ERROR"
	CodeNotFound              = "RESOURCE_NOT_FOUND"
	CodeForbidden             = "FORBIDDEN_ACCESS"
	CodeUnauthorized          = "UNAUTHORIZED"
	CodeInvalidPayload        = "INVALID_PAYLOAD"
	CodeConflict              = "RESOURCE_CONFLICT"
	CodeTooManyRequests       = "TOO_MANY_REQUESTS"
	CodePayloadTooLarge       = "PAYLOAD_TOO_LARGE"
	CodeUnsupportedMedia      = "UNSUPPORTED_MEDIA_TYPE"
	CodeServiceUnavailable    = "SERVICE_UNAVAILABLE"
	CodeTelemetrySyncRequired = "TELEMETRY_SYNC_REQUIRED"
	CodeMissingFile           = "MISSING_FILE"
	CodeFileReadError         = "FILE_READ_ERROR"
	CodeUserIsNotIdentified   = "USER_NOT_IDENTIFIED"
	CodeTwoFactorRequired     = "TWO_FACTOR_REQUIRED"
	CodeTwoFactorInvalid      = "TWO_FACTOR_INVALID_PROOF"
	CodeTwoFactorExpired      = "TWO_FACTOR_SESSION_EXPIRED"
	CodePasskeyCloneAlert     = "PASSKEY_CLONE_DETECTED"
)

// ############################################################################
// # CONSTRUCTEURS SÉMANTIQUES
// # Ces fonctions wrap l'erreur originelle pour ne jamais la fuiter au client.
// ############################################################################

// NewInternal s'utilise quand la base de données (Postgres, Mongo) ou Redis crashe.
// Le message public est volontairement générique. L'erreur brute est logguée.
func NewInternal(errs ...error) *AppError {
	var err error
	if len(errs) > 0 {
		err = errs[0]
	}
	return &AppError{
		HTTPStatus: http.StatusInternalServerError,
		Code:       CodeInternalError,
		Message:    "Une erreur interne est survenue. Nos équipes ont été alertées.",
		Err:        err,
	}
}

// NewNotFound s'utilise quand une entité (Post, Utilisateur) n'existe pas ou plus.
func NewNotFound(code, message string, err error) *AppError {
	if code == "" {
		code = CodeNotFound
	}
	return &AppError{HTTPStatus: http.StatusNotFound, Code: code, Message: message, Err: err}
}

// NewForbidden s'utilise pour les règles métier (banni, manque de droits).
func NewForbidden(code, message string, err error) *AppError {
	if code == "" {
		code = CodeForbidden
	}
	return &AppError{HTTPStatus: http.StatusForbidden, Code: code, Message: message, Err: err}
}

// NewBadRequest s'utilise pour les JSON mal formés, tailles dépassées, requêtes impossibles.
func NewBadRequest(code, message string, err error) *AppError {
	if code == "" {
		code = CodeInvalidPayload
	}
	return &AppError{HTTPStatus: http.StatusBadRequest, Code: code, Message: message, Err: err}
}

// NewConflict s'utilise quand une ressource existe déjà (doublon email, pseudo, etc.).
func NewConflict(code, message string, err error) *AppError {
	if code == "" {
		code = CodeConflict
	}
	return &AppError{HTTPStatus: http.StatusConflict, Code: code, Message: message, Err: err}
}

// NewUnauthorized s'utilise pour les problèmes d'authentification (Token invalide, manquant, expiré).
func NewUnauthorized(code, message string, err error) *AppError {
	if code == "" {
		code = CodeUnauthorized
	}
	return &AppError{HTTPStatus: http.StatusUnauthorized, Code: code, Message: message, Err: err}
}

// NewTooManyRequests s'utilise pour les Rate Limiters (Anti-Spam).
func NewTooManyRequests(message string, err error) *AppError {
	return &AppError{HTTPStatus: http.StatusTooManyRequests, Code: CodeTooManyRequests, Message: message, Err: err}
}

// NewAppError permet de construire une erreur avec un code HTTP sur-mesure pour les Middlewares.
func NewAppError(httpStatus int, code, message string, err error) *AppError {
	return &AppError{
		HTTPStatus: httpStatus,
		Code:       code,
		Message:    message,
		Err:        err,
	}
}

// Combine fusionne deux AppError. Si les codes HTTP diffèrent, le plus critique (élevé) est conservé.
func Combine(err1, err2 *AppError) *AppError {
	// 1. Gestion des valeurs nulles
	if err1 == nil {
		return err2
	}
	if err2 == nil {
		return err1
	}

	// 2. Fusion des messages publics
	combinedMessage := fmt.Sprintf("%s | %s", err1.Message, err2.Message)

	// 3. Fusion des erreurs internes brutes (stack trace/logs)
	combinedErr := errors.Join(err1.Err, err2.Err)

	// 4. Détermination du code HTTP le plus restrictif/critique (ex: 500 l'emporte sur 400)
	status := err1.HTTPStatus
	if err2.HTTPStatus > err1.HTTPStatus {
		status = err2.HTTPStatus
	}

	// 5. Création d'un code métier composite
	combinedCode := fmt.Sprintf("%s_AND_%s", err1.Code, err2.Code)

	return &AppError{
		HTTPStatus: status,
		Code:       combinedCode,
		Message:    combinedMessage,
		Err:        combinedErr,
	}
}
