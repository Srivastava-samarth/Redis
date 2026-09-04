package repository

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

const releaseLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`

type RedisRepository struct {
	client *redis.Client
}

func NewRedisRepository(client *redis.Client) *RedisRepository {
	return &RedisRepository{
		client: client,
	}
}

func (r *RedisRepository) Get(
	ctx context.Context,
	key string,
) (string, error) {

	return r.client.Get(ctx, key).Result()
}

func (r *RedisRepository) Set(
	ctx context.Context,
	key string,
	value string,
	ttl time.Duration,
) error {

	return r.client.Set(
		ctx,
		key,
		value,
		ttl,
	).Err()
}

func (r *RedisRepository) AcquireLock(
	ctx context.Context,
	key string,
	value string,
	ttl time.Duration,
) (bool, error) {

	return r.client.SetNX(
		ctx,
		key,
		value,
		ttl,
	).Result()
}

func (r *RedisRepository) ReleaseLock(
    ctx context.Context,
    key string,
    value string,
) error {
    result, err := r.client.Eval(
        ctx,
        releaseLockScript,
        []string{key},
        value,
    ).Int64()

    if err != nil {
        return err
    }

    if result == 1 {
        log.Printf("Lock actually released | key=%s", key)
    } else {
        log.Printf("Lock NOT released - ownership changed | key=%s", key)
    }

    return nil
}