package badge_models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// BadgeMetadata represents the structured metadata for an individual badge or certification.
type BadgeMetadata struct {
	IssuedAt  time.Time  `bson:"issued_at" json:"issued_at" msgpack:"issued_at"`
	ExpiresAt *time.Time `bson:"expires_at" json:"expires_at" msgpack:"expires_at"`
}

// UserBadges represents a map of badge identifiers to their structured metadata.
type UserBadges map[string]BadgeMetadata

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
	type Alias UserBadges
	return json.Marshal(Alias(b))
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
			now := time.Now().UTC()
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

	type Alias UserBadges
	var a Alias
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

// HasBadge checks if a badge exists and is currently valid (not expired).
func (b UserBadges) HasBadge(badgeName string) bool {
	if b == nil {
		return false
	}
	meta, exists := b[badgeName]
	if !exists {
		return false
	}
	if meta.ExpiresAt != nil && meta.ExpiresAt.Before(time.Now().UTC()) {
		return false
	}
	return true
}

// IsExpired checks if a badge exists and has expired.
func (b UserBadges) IsExpired(badgeName string) bool {
	if b == nil {
		return false
	}
	meta, exists := b[badgeName]
	if !exists {
		return false
	}
	return meta.ExpiresAt != nil && meta.ExpiresAt.Before(time.Now().UTC())
}

// Get returns the metadata for a badge and a boolean indicating whether it exists.
func (b UserBadges) Get(badgeName string) (BadgeMetadata, bool) {
	if b == nil {
		return BadgeMetadata{}, false
	}
	meta, exists := b[badgeName]
	return meta, exists
}
