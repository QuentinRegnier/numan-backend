package badge_models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/QuentinRegnier/numan-backend/internal/domain"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// BadgeMetadata represents the structured metadata for an individual badge or certification.
type BadgeMetadata struct {
	IssuedAt  time.Time  `bson:"issued_at" json:"issued_at" msgpack:"issued_at"`
	ExpiresAt *time.Time `bson:"expires_at" json:"expires_at" msgpack:"expires_at"`
}

// Validate checks temporal integrity of a badge's issuance and optional expiration dates.
func (m *BadgeMetadata) Validate() error {
	if m == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "Les métadonnées du badge sont nulles.", nil)
	}
	if m.IssuedAt.IsZero() {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La date d'émission du badge est requise.", nil)
	}
	if m.ExpiresAt != nil && m.ExpiresAt.Before(m.IssuedAt) {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La date d'expiration du badge ne peut pas précéder la date d'émission.", nil)
	}
	return nil
}

// UserBadges represents a map of badge identifiers to their structured metadata.
type UserBadges map[string]BadgeMetadata

// Validate checks each badge key and metadata entry in the collection.
func (b *UserBadges) Validate() error {
	if b == nil {
		return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "La collection de badges est nulle.", nil)
	}
	for key, meta := range *b {
		if strings.TrimSpace(key) == "" || len(key) > 100 {
			return numan_error.NewBadRequest(numan_error.CodeInvalidPayload, "L'identifiant de badge est invalide.", nil)
		}
		metaCopy := meta
		if err := metaCopy.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Scan implements the sql.Scanner interface for PostgreSQL JSONB scanning.
func (b *UserBadges) Scan(value any) error {
	if value == nil {
		*b = make(UserBadges)
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("failed to scan UserBadges: incompatible type %T", value)
	}

	if len(bytes) == 0 || string(bytes) == "null" {
		*b = make(UserBadges)
		return nil
	}

	return b.UnmarshalJSON(bytes)
}

// Value implements the driver.Valuer interface for database serialization (PostgreSQL JSONB).
func (b UserBadges) Value() (driver.Value, error) {
	if b == nil {
		return "{}", nil
	}
	bytes, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	return string(bytes), nil
}

// MarshalJSON guarantees that nil UserBadges serializes as an empty JSON object "{}" instead of null.
func (b UserBadges) MarshalJSON() ([]byte, error) {
	if b == nil {
		return []byte("{}"), nil
	}
	type alias UserBadges
	return json.Marshal(alias(b))
}

// UnmarshalJSON parses JSON object data into UserBadges, supporting backward compatibility
// for legacy string arrays (e.g., ["cert1", "cert2"]).
func (b *UserBadges) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*b = make(UserBadges)
		return nil
	}

	// Backwards compatibility: handle legacy JSON array ["badge1", "badge2"]
	if data[0] == '[' {
		var legacySlugs []string
		if err := json.Unmarshal(data, &legacySlugs); err == nil {
			*b = make(UserBadges, len(legacySlugs))
			now := domain.NowTime()
			for _, slug := range legacySlugs {
				if slug != "" {
					(*b)[slug] = BadgeMetadata{
						IssuedAt:  now,
						ExpiresAt: nil,
					}
				}
			}
			return nil
		}
	}

	type alias UserBadges
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	if a == nil {
		*b = make(UserBadges)
	} else {
		*b = UserBadges(a)
	}
	return nil
}

// hasBadge checks if a badge exists and is currently valid (not expired).
func (b UserBadges) hasBadge(badgeName string) bool {
	if b == nil {
		return false
	}
	meta, exists := b[badgeName]
	if !exists {
		return false
	}
	if meta.ExpiresAt != nil && meta.ExpiresAt.Before(domain.NowTime()) {
		return false
	}
	return true
}

// isExpired checks if a badge exists and has expired.
func (b UserBadges) isExpired(badgeName string) bool {
	if b == nil {
		return false
	}
	meta, exists := b[badgeName]
	if !exists {
		return false
	}
	return meta.ExpiresAt != nil && meta.ExpiresAt.Before(domain.NowTime())
}

// Get returns the metadata for a badge and a boolean indicating whether it exists.
func (b UserBadges) Get(badgeName string) (BadgeMetadata, bool) {
	if b == nil {
		return BadgeMetadata{}, false
	}
	meta, exists := b[badgeName]
	return meta, exists
}
