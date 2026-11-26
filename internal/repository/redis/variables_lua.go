package redis

import (
	"context"
	"errors"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	goredis "github.com/redis/go-redis/v9"
)

const applyVariablesPatchScript = `
local overrides_key = KEYS[1]
local version_key = KEYS[2]
local set_count = tonumber(ARGV[1])
local idx = 2

for i = 1, set_count do
    local k = ARGV[idx]
    local v = ARGV[idx + 1]
    redis.call('HSET', overrides_key, k, v)
    idx = idx + 2
end

local del_count = tonumber(ARGV[idx])
idx = idx + 1
for i = 1, del_count do
    local k = ARGV[idx]
    redis.call('HDEL', overrides_key, k)
    idx = idx + 1
end

local new_version = redis.call('INCR', version_key)
return new_version
`

const loadVariablesStateScript = `
local overrides_key = KEYS[1]
local version_key = KEYS[2]
local ver = tonumber(redis.call('GET', version_key) or '0')
local raw = redis.call('HGETALL', overrides_key)
return {ver, raw}
`

var (
	applyVariablesPatchCompiled = goredis.NewScript(applyVariablesPatchScript)
	loadVariablesStateCompiled  = goredis.NewScript(loadVariablesStateScript)
)

// ApplyVariablesPatchAtomic applique un lot de mises à jour/suppressions dans le Hash Redis
// et incrémente le numéro de version global en une seule transaction atomique Lua (EVALSHA avec repli EVAL).
// Les deux clés doivent partager le même hash-tag Redis Cluster (ex: {config}).
func (c *Collection) ApplyVariablesPatchAtomic(
	ctx context.Context,
	overridesID any,
	versionID any,
	toSet map[string]string,
	toDelete []string,
) (int64, error) {
	if !c.IsReady() {
		return 0, numan_error.NewInternal(ErrNotInitialized)
	}

	args := make([]any, 0, 2+len(toSet)*2+len(toDelete))
	args = append(args, len(toSet))
	for k, v := range toSet {
		args = append(args, k, v)
	}
	args = append(args, len(toDelete))
	for _, k := range toDelete {
		args = append(args, k)
	}

	keys := []string{c.Key(overridesID), c.Key(versionID)}
	return applyVariablesPatchCompiled.Run(ctx, c.client, keys, args...).Int64()
}

// LoadVariablesStateAtomic récupère de manière atomique le couple (version, overrides) en 1 seul RTT (EVALSHA).
// Les deux clés doivent partager le même hash-tag Redis Cluster (ex: {config}).
func (c *Collection) LoadVariablesStateAtomic(
	ctx context.Context,
	overridesID any,
	versionID any,
) (map[string]string, int64, error) {
	if !c.IsReady() {
		return nil, 0, numan_error.NewInternal(ErrNotInitialized)
	}

	keys := []string{c.Key(overridesID), c.Key(versionID)}
	res, err := loadVariablesStateCompiled.Run(ctx, c.client, keys).Result()
	if err != nil {
		return nil, 0, err
	}

	slice, ok := res.([]any)
	if !ok || len(slice) != 2 {
		return nil, 0, numan_error.NewInternal(errors.New("réponse Lua inattendue pour LoadVariablesStateAtomic"))
	}

	version, _ := slice[0].(int64)
	overrides := make(map[string]string)

	if rawList, ok := slice[1].([]any); ok {
		for i := 0; i+1 < len(rawList); i += 2 {
			k, kOk := rawList[i].(string)
			v, vOk := rawList[i+1].(string)
			if kOk && vOk {
				overrides[k] = v
			}
		}
	}

	return overrides, version, nil
}
