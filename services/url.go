package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "redis-lab/database/models"
	"redis-lab/repository"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	ErrRequestProcessing = errors.New("request is already being processed")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

type URLService struct {
	repo  *repository.URLRepository
	redis *redis.Client
}

func NewURLService(
	repo *repository.URLRepository,
	redis *redis.Client,
) *URLService {
	return &URLService{
		repo:  repo,
		redis: redis,
	}
}

func (s *URLService) CreateURL(
	clientID string,
	originalURL string,
	idempotencyKey string,
) (*model.URL, error) {
	ctx := context.Background()

	if err := s.checkRateLimit(ctx, clientID); err != nil {
        return nil, err
    }
	// 1. Check idempotency.
	existingURL, err := s.checkIdempotency(
		ctx,
		clientID,
		idempotencyKey,
		originalURL,
	)
	if err != nil {
		return nil, err
	}

	if existingURL != nil {
		return existingURL, nil
	}

	// 2. Check if this client already has a short URL
	//    for the same original URL.
	existingURL, err = s.findExistingURL(
		clientID,
		originalURL,
	)
	if err != nil {
		return nil, err
	}

	if existingURL != nil {
		// Recreate the idempotency record because Redis
		// may have lost/expired it.
		if err := s.saveCompletedIdempotency(
			ctx,
			clientID,
			idempotencyKey,
			existingURL.ShortCode,
		); err != nil {
			return nil, err
		}

		return existingURL, nil
	}

	// 3. Claim the idempotency key.
	claimed, err := s.claimIdempotency(
		ctx,
		clientID,
		idempotencyKey,
	)
	if err != nil {
		return nil, err
	}

	if !claimed {
		return nil, ErrRequestProcessing
	}

	// 5. Generate a short code.
	shortCode := s.generateShortCode(ctx)

	url := &model.URL{
		ClientID:    clientID,
		ShortCode:   shortCode,
		OriginalURL: originalURL,
	}

	// 6. Create the URL in PostgreSQL.
	if err := s.repo.Create(url); err != nil {
		// The DB might already contain the URL because another
		// request won the race on the unique constraint.
		existingURL, lookupErr := s.findExistingURL(
			clientID,
			originalURL,
		)

		if lookupErr != nil {
			s.deleteIdempotencyKey(ctx, clientID, idempotencyKey)
			return nil, err
		}

		if existingURL != nil {
			if finalizeErr := s.saveCompletedIdempotency(
				ctx,
				clientID,
				idempotencyKey,
				existingURL.ShortCode,
			); finalizeErr != nil {
				return nil, finalizeErr
			}

			return existingURL, nil
		}

		s.deleteIdempotencyKey(ctx, clientID, idempotencyKey)

		return nil, err
	}

	// 7. Finalize idempotency.
	if err := s.saveCompletedIdempotency(
		ctx,
		clientID,
		idempotencyKey,
		shortCode,
	); err != nil {
		// PostgreSQL already contains the URL.
		// The client can retry and recover it from the DB.
		return nil, err
	}

	return url, nil
}

func (s *URLService) checkIdempotency(
	ctx context.Context,
	clientID string,
	idempotencyKey string,
	originalURL string,
) (*model.URL, error) {
	key := fmt.Sprintf(
		"idempotency:%s:%s",
		clientID,
		idempotencyKey,
	)

	value, err := s.redis.Get(ctx, key).Result()

	switch {
	case err == nil && value != "processing":
		return &model.URL{
			ClientID:    clientID,
			ShortCode:   value,
			OriginalURL: originalURL,
		}, nil

	case err == nil && value == "processing":
		existingURL, err := s.repo.GetByClientAndURL(
			clientID,
			originalURL,
		)

		if err == nil {
			if err := s.redis.Set(
				ctx,
				key,
				existingURL.ShortCode,
				0,
			).Err(); err != nil {
				return nil, err
			}

			return existingURL, nil
		}

		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}

		return nil, ErrRequestProcessing

	case errors.Is(err, redis.Nil):
		return nil, nil

	default:
		return nil, err
	}
}

func (s *URLService) findExistingURL(
	clientID string,
	originalURL string,
) (*model.URL, error) {
	url, err := s.repo.GetByClientAndURL(
		clientID,
		originalURL,
	)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return url, nil
}

func (s *URLService) claimIdempotency(
	ctx context.Context,
	clientID string,
	idempotencyKey string,
) (bool, error) {
	key := fmt.Sprintf(
		"idempotency:%s:%s",
		clientID,
		idempotencyKey,
	)

	claimed, err := s.redis.SetNX(
		ctx,
		key,
		"processing",
		60*time.Second,
	).Result()

	if err != nil {
		return false, err
	}

	return claimed, nil
}

func (s *URLService) saveCompletedIdempotency(
	ctx context.Context,
	clientID string,
	idempotencyKey string,
	shortCode string,
) error {
	key := fmt.Sprintf(
		"idempotency:%s:%s",
		clientID,
		idempotencyKey,
	)

	return s.redis.Set(
		ctx,
		key,
		shortCode,
		0,
	).Err()
}

func (s *URLService) deleteIdempotencyKey(
	ctx context.Context,
	clientID string,
	idempotencyKey string,
) {
	key := fmt.Sprintf(
		"idempotency:%s:%s",
		clientID,
		idempotencyKey,
	)

	if err := s.redis.Del(ctx, key).Err(); err != nil {
		fmt.Printf(
			"failed to delete idempotency key %s: %v\n",
			key,
			err,
		)
	}
}

func (s *URLService) checkRateLimit(
	ctx context.Context,
	clientID string,
) error {
	key := fmt.Sprintf(
		"rate-limit:%s",
		clientID,
	)

	count, err := s.redis.Incr(ctx, key).Result()
	if err != nil {
		return err
	}

	if count == 1 {
		if err := s.redis.Expire(
			ctx,
			key,
			60*time.Second,
		).Err(); err != nil {
			return err
		}
	}

	if count >  10000{
		return ErrRateLimitExceeded
	}

	return nil
}

func (s *URLService) generateShortCode(
	ctx context.Context,
) string {
	id, _ := s.redis.Incr(ctx, "url:id").Result()

	return fmt.Sprintf("url%d", id)
}

func (s *URLService) GetURL(
	shortCode string,
) (*model.URL, error) {
	return s.repo.GetByShortCode(shortCode)
}
