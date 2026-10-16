package variables

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
)

// registeredVariable définit le contrat interne d'une variable dynamique gérée par le registre.
type registeredVariable interface {
	Name() string
	Category() string
	TypeName() string
	GetCurrentValue() any
	GetDefaultValue() any
	IsCustomValue() bool
	ValidateAndSerialize(raw json.RawMessage) (serialized string, parsedVal any, isDefault bool, err error)
	ApplySerialized(serialized string) error
	ResetToDefault()
}

// variableDescriptor lie fortement une variable Go typée T à ses règles de validation et de sérialisation.
type variableDescriptor[T any] struct {
	name         string
	category     string
	typeName     string
	ptr          *T
	defaultValue T
	isCustom     bool
	parseFunc    func(raw json.RawMessage) (T, error)
	validateFunc func(val T) error
	formatFunc   func(val T) any
	equalFunc    func(a, b T) bool
	onChange     func(val T)
}

func (d *variableDescriptor[T]) Name() string         { return d.name }
func (d *variableDescriptor[T]) Category() string     { return d.category }
func (d *variableDescriptor[T]) TypeName() string     { return d.typeName }
func (d *variableDescriptor[T]) GetCurrentValue() any { return d.formatFunc(*d.ptr) }
func (d *variableDescriptor[T]) GetDefaultValue() any { return d.formatFunc(d.defaultValue) }
func (d *variableDescriptor[T]) IsCustomValue() bool  { return d.isCustom }

func (d *variableDescriptor[T]) ValidateAndSerialize(raw json.RawMessage) (string, any, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil, false, errors.New("la valeur ne peut pas être nulle")
	}

	val, err := d.parseFunc(trimmed)
	if err != nil {
		return "", nil, false, err
	}

	if d.validateFunc != nil {
		if err := d.validateFunc(val); err != nil {
			return "", nil, false, err
		}
	}

	isDefault := d.equalFunc(val, d.defaultValue)
	formatted := d.formatFunc(val)

	serializedBytes, err := json.Marshal(formatted)
	if err != nil {
		return "", nil, false, err
	}

	return string(serializedBytes), formatted, isDefault, nil
}

func (d *variableDescriptor[T]) ApplySerialized(serialized string) error {
	val, err := d.parseFunc(json.RawMessage(serialized))
	if err != nil {
		return err
	}
	if d.validateFunc != nil {
		if err := d.validateFunc(val); err != nil {
			return err
		}
	}
	prev := *d.ptr
	*d.ptr = val
	d.isCustom = !d.equalFunc(val, d.defaultValue)
	if d.onChange != nil && !d.equalFunc(prev, val) {
		d.onChange(val)
	}
	return nil
}

func (d *variableDescriptor[T]) ResetToDefault() {
	prev := *d.ptr
	*d.ptr = d.defaultValue
	d.isCustom = false
	if d.onChange != nil && !d.equalFunc(prev, d.defaultValue) {
		d.onChange(d.defaultValue)
	}
}

// ============================================================================
// ÉTAT GLOBAL DU REGISTRE (THREAD-SAFE)
// ============================================================================

var (
	regMu           sync.RWMutex
	registry        = make(map[string]registeredVariable)
	aliasMap        = make(map[string]string) // ex: "FEED_PAGE_SIZE" -> "FeedPageSize"
	currentVersion  int64
	standardTTLHook func(time.Duration)
)

// PreparedVariableChange représente une modification validée prête à être persistée dans Redis.
type PreparedVariableChange struct {
	CanonicalName string
	Serialized    string
	FormattedVal  any
	IsDefault     bool
}

// PreparedPatch regroupe l'ensemble des modifications validées d'une requête PATCH.
type PreparedPatch struct {
	Changes  []PreparedVariableChange
	ToSet    map[string]string
	ToDelete []string
	Updated  map[string]any
}

// SnapshotEntry représente l'état instantané d'une variable lors d'une lecture.
type SnapshotEntry struct {
	Value        any
	DefaultValue any
	Type         string
	Category     string
	IsCustom     bool
}

// SetStandardTTLHook permet au repository Redis d'enregistrer un callback pour rafraîchir
// dynamiquement le DefaultTTL des collections lorsque StandardTTL est modifié à chaud.
func SetStandardTTLHook(fn func(time.Duration)) {
	regMu.Lock()
	defer regMu.Unlock()
	standardTTLHook = fn
}

// GetVersion retourne la version locale actuelle de la configuration.
func GetVersion() int64 {
	regMu.RLock()
	defer regMu.RUnlock()
	return currentVersion
}

// RLock permet aux composants critiques de verrouiller en lecture un ensemble cohérent de variables.
func RLock()   { regMu.RLock() }
func RUnlock() { regMu.RUnlock() }

func resolveCanonicalName(inputKey string) (registeredVariable, bool) {
	if v, ok := registry[inputKey]; ok {
		return v, true
	}
	if canonical, ok := aliasMap[strings.ToUpper(strings.TrimSpace(inputKey))]; ok {
		v, exists := registry[canonical]
		return v, exists
	}
	return nil, false
}

// ValidateAndPreparePatch exécute la Phase 1 (Validation stricte sans effet de bord).
func ValidateAndPreparePatch(rawPatch map[string]json.RawMessage) (*PreparedPatch, error) {
	if len(rawPatch) == 0 {
		return nil, numan_error.NewBadRequest(
			numan_error.CodeInvalidPayload,
			"Aucune variable fournie dans le corps de la requête.",
			nil,
		)
	}

	regMu.RLock()
	defer regMu.RUnlock()

	prepared := &PreparedPatch{
		Changes:  make([]PreparedVariableChange, 0, len(rawPatch)),
		ToSet:    make(map[string]string),
		ToDelete: make([]string, 0),
		Updated:  make(map[string]any, len(rawPatch)),
	}

	seenCanonical := make(map[string]string, len(rawPatch))

	for rawKey, rawVal := range rawPatch {
		desc, exists := resolveCanonicalName(rawKey)
		if !exists {
			return nil, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				fmt.Sprintf("La variable '%s' est inconnue ou n'est pas modifiable dynamiquement.", rawKey),
				nil,
			)
		}

		canonical := desc.Name()
		if firstKey, duplicate := seenCanonical[canonical]; duplicate {
			return nil, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				fmt.Sprintf("Conflit de clés dupliquées dans la requête : '%s' et '%s' ciblent la même variable '%s'.", firstKey, rawKey, canonical),
				nil,
			)
		}
		seenCanonical[canonical] = rawKey

		serialized, formattedVal, isDefault, err := desc.ValidateAndSerialize(rawVal)
		if err != nil {
			return nil, numan_error.NewBadRequest(
				numan_error.CodeInvalidPayload,
				fmt.Sprintf("Valeur invalide pour la variable '%s' (type attendu: %s) : %s", canonical, desc.TypeName(), err.Error()),
				err,
			)
		}

		prepared.Changes = append(prepared.Changes, PreparedVariableChange{
			CanonicalName: canonical,
			Serialized:    serialized,
			FormattedVal:  formattedVal,
			IsDefault:     isDefault,
		})

		if isDefault {
			prepared.ToDelete = append(prepared.ToDelete, canonical)
		} else {
			prepared.ToSet[canonical] = serialized
		}
		prepared.Updated[canonical] = formattedVal
	}

	return prepared, nil
}

// ApplyPreparedPatchInMemory applique en mémoire un patch déjà validé si la version est plus récente.
func ApplyPreparedPatchInMemory(prepared *PreparedPatch, newVersion int64) {
	if prepared == nil {
		return
	}

	regMu.Lock()
	defer regMu.Unlock()

	if newVersion < currentVersion {
		return
	}

	for _, change := range prepared.Changes {
		desc, exists := registry[change.CanonicalName]
		if !exists {
			continue
		}
		if change.IsDefault {
			desc.ResetToDefault()
		} else {
			_ = desc.ApplySerialized(change.Serialized)
		}
	}
	currentVersion = newVersion
}

// ApplyRemoteState synchronise intégralement la mémoire locale avec l'état de référence Redis.
func ApplyRemoteState(overrides map[string]string, remoteVersion int64) error {
	regMu.Lock()
	defer regMu.Unlock()

	var applyErrs []error
	for name, desc := range registry {
		if rawVal, hasOverride := overrides[name]; hasOverride {
			if err := desc.ApplySerialized(rawVal); err != nil {
				applyErrs = append(applyErrs, fmt.Errorf("%s: %w", name, err))
				desc.ResetToDefault()
			}
		} else {
			desc.ResetToDefault()
		}
	}

	currentVersion = remoteVersion
	return errors.Join(applyErrs...)
}

// GetSnapshot retourne une copie atomique et cohérente de toutes les variables dynamiques.
func GetSnapshot() (map[string]any, map[string]SnapshotEntry, int64) {
	regMu.RLock()
	defer regMu.RUnlock()

	values := make(map[string]any, len(registry))
	details := make(map[string]SnapshotEntry, len(registry))

	for name, desc := range registry {
		cur := desc.GetCurrentValue()
		def := desc.GetDefaultValue()
		values[name] = cur
		details[name] = SnapshotEntry{
			Value:        cur,
			DefaultValue: def,
			Type:         desc.TypeName(),
			Category:     desc.Category(),
			IsCustom:     desc.IsCustomValue(),
		}
	}

	return values, details, currentVersion
}

// ============================================================================
// CONSTRUCTEURS ET PARSEURS STRICTS PAR TYPE
// ============================================================================

func registerDescriptor[T any](desc *variableDescriptor[T]) {
	registry[desc.name] = desc
	aliasMap[strings.ToUpper(desc.name)] = desc.name
	aliasMap[toUpperSnakeCase(desc.name)] = desc.name
}

func toUpperSnakeCase(s string) string {
	var out []rune
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextIsLower) {
				out = append(out, '_')
			}
		}
		out = append(out, unicode.ToUpper(r))
	}
	return string(out)
}

func registerInt(name, category string, ptr *int, validator func(int) error) {
	registerDescriptor(&variableDescriptor[int]{
		name:         name,
		category:     category,
		typeName:     "int",
		ptr:          ptr,
		defaultValue: *ptr,
		parseFunc:    parseStrictInt,
		validateFunc: validator,
		formatFunc:   func(v int) any { return v },
		equalFunc:    func(a, b int) bool { return a == b },
	})
}

func registerInt64(name, category string, ptr *int64, validator func(int64) error) {
	registerDescriptor(&variableDescriptor[int64]{
		name:         name,
		category:     category,
		typeName:     "int64",
		ptr:          ptr,
		defaultValue: *ptr,
		parseFunc:    parseStrictInt64,
		validateFunc: validator,
		formatFunc:   func(v int64) any { return v },
		equalFunc:    func(a, b int64) bool { return a == b },
	})
}

func registerFloat64(name, category string, ptr *float64, validator func(float64) error) {
	registerDescriptor(&variableDescriptor[float64]{
		name:         name,
		category:     category,
		typeName:     "float64",
		ptr:          ptr,
		defaultValue: *ptr,
		parseFunc:    parseStrictFloat64,
		validateFunc: validator,
		formatFunc:   func(v float64) any { return v },
		equalFunc:    func(a, b float64) bool { return a == b },
	})
}

func registerBool(name, category string, ptr *bool) {
	registerDescriptor(&variableDescriptor[bool]{
		name:         name,
		category:     category,
		typeName:     "bool",
		ptr:          ptr,
		defaultValue: *ptr,
		parseFunc:    parseStrictBool,
		formatFunc:   func(v bool) any { return v },
		equalFunc:    func(a, b bool) bool { return a == b },
	})
}

func registerString(name, category string, ptr *string, validator func(string) error) {
	registerDescriptor(&variableDescriptor[string]{
		name:         name,
		category:     category,
		typeName:     "string",
		ptr:          ptr,
		defaultValue: *ptr,
		parseFunc:    parseStrictString,
		validateFunc: validator,
		formatFunc:   func(v string) any { return v },
		equalFunc:    func(a, b string) bool { return a == b },
	})
}

func registerDuration(name, category string, ptr *time.Duration, validator func(time.Duration) error, onChange func(time.Duration)) {
	registerDescriptor(&variableDescriptor[time.Duration]{
		name:         name,
		category:     category,
		typeName:     "duration",
		ptr:          ptr,
		defaultValue: *ptr,
		parseFunc:    parseStrictDuration,
		validateFunc: validator,
		formatFunc:   func(v time.Duration) any { return v.String() },
		equalFunc:    func(a, b time.Duration) bool { return a == b },
		onChange:     onChange,
	})
}

func registerStringSlice(name, category string, ptr *[]string, validator func([]string) error) {
	defCopy := append([]string(nil), (*ptr)...)
	registerDescriptor(&variableDescriptor[[]string]{
		name:         name,
		category:     category,
		typeName:     "[]string",
		ptr:          ptr,
		defaultValue: defCopy,
		parseFunc:    parseStrictStringSlice,
		validateFunc: validator,
		formatFunc: func(v []string) any {
			cp := make([]string, len(v))
			copy(cp, v)
			return cp
		},
		equalFunc: func(a, b []string) bool { return reflect.DeepEqual(a, b) },
	})
}

func parseStrictInt64(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || raw[0] == '"' || raw[0] == '{' || raw[0] == '[' || raw[0] == 't' || raw[0] == 'f' {
		return 0, errors.New("attendu un entier JSON strict (sans guillemets ni décimales)")
	}
	if bytes.ContainsAny(raw, ".eE") {
		return 0, errors.New("les nombres décimaux ou en notation scientifique ne sont pas acceptés pour un entier")
	}
	var num json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&num); err != nil {
		return 0, errors.New("format numérique entier invalide")
	}
	if dec.More() {
		return 0, errors.New("jetons JSON superflus")
	}
	val, err := num.Int64()
	if err != nil {
		return 0, errors.New("dépassement de capacité ou entier invalide")
	}
	return val, nil
}

func parseStrictInt(raw json.RawMessage) (int, error) {
	val64, err := parseStrictInt64(raw)
	if err != nil {
		return 0, err
	}
	if val64 < int64(math.MinInt) || val64 > int64(math.MaxInt) {
		return 0, errors.New("dépassement de capacité pour le type int")
	}
	return int(val64), nil
}

func parseStrictFloat64(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 || raw[0] == '"' || raw[0] == '{' || raw[0] == '[' || raw[0] == 't' || raw[0] == 'f' {
		return 0, errors.New("attendu un nombre flottant JSON strict")
	}
	var num json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&num); err != nil {
		return 0, errors.New("format numérique flottant invalide")
	}
	val, err := num.Float64()
	if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
		return 0, errors.New("nombre flottant invalide (NaN/Inf interdit)")
	}
	return val, nil
}

func parseStrictBool(raw json.RawMessage) (bool, error) {
	if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
		return false, errors.New("attendu un booléen JSON strict (true ou false)")
	}
	return bytes.Equal(raw, []byte("true")), nil
}

func parseStrictString(raw json.RawMessage) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", errors.New("attendu une chaîne de caractères JSON entre guillemets")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", errors.New("chaîne de caractères JSON mal formée")
	}
	return s, nil
}

func parseStrictDuration(raw json.RawMessage) (time.Duration, error) {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, errors.New("chaîne de durée JSON invalide")
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("format de durée Go invalide (ex: '30m', '5s', '24h') : %w", err)
		}
		return d, nil
	}
	return 0, errors.New("attendu une chaîne de durée valide (ex: \"30m\", \"5s\", \"24h\")")
}

func parseStrictStringSlice(raw json.RawMessage) ([]string, error) {
	if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
		return nil, errors.New("attendu un tableau JSON de chaînes de caractères")
	}
	var rawItems []json.RawMessage
	if err := json.Unmarshal(raw, &rawItems); err != nil {
		return nil, errors.New("tableau JSON mal formé")
	}
	res := make([]string, 0, len(rawItems))
	for _, item := range rawItems {
		s, err := parseStrictString(bytes.TrimSpace(item))
		if err != nil {
			return nil, errors.New("tous les éléments du tableau doivent être des chaînes de caractères")
		}
		res = append(res, s)
	}
	return res, nil
}
