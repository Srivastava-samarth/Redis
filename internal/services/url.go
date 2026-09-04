package service

import (
	"context"
	"log"
	"time"

	repository "redis-lab/internal/repositories"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type URLService struct {
	urlRepo   *repository.URLRepository
	redisRepo *repository.RedisRepository
}

func NewURLService(
	urlRepo *repository.URLRepository,
	redisRepo *repository.RedisRepository,
) *URLService {
	return &URLService{
		urlRepo:   urlRepo,
		redisRepo: redisRepo,
	}
}

func (s *URLService) CreateURL(
	ctx context.Context,
	shortCode string,
	originalURL string,
) (*repository.URL, error) {

	url, err := s.urlRepo.Create(
		ctx,
		shortCode,
		originalURL,
	)

	if err != nil {
		return nil, err
	}

	cacheKey := "url:" + shortCode

	start := time.Now()

	err = s.redisRepo.Set(
		ctx,
		cacheKey,
		originalURL,
		5*time.Minute,
	)

	log.Printf(
		"Redis SET | key=%s | duration=%v",
		cacheKey,
		time.Since(start),
	)

	if err != nil {
		log.Printf(
			"Redis SET failed | key=%s | error=%v",
			cacheKey,
			err,
		)
	}

	return url, nil
}

func (s *URLService) GetURL(
	ctx context.Context,
	shortCode string,
) (*repository.URL, string, error) {

	cacheKey := "url:" + shortCode
	lockKey := "lock:" + cacheKey

	// --------------------------------------------------
	// 1. Check Redis
	// --------------------------------------------------

	start := time.Now()

	cachedURL, err := s.redisRepo.Get(
		ctx,
		cacheKey,
	)

	log.Printf(
		"Redis GET | key=%s | duration=%v",
		cacheKey,
		time.Since(start),
	)

	if err == nil {
		log.Printf(
			"Redis cache HIT | shortCode=%s",
			shortCode,
		)

		return &repository.URL{
			ShortCode:   &shortCode,
			OriginalURL: &cachedURL,
		}, "redis", nil
	}

	if err != redis.Nil {
		log.Printf(
			"Redis GET error | key=%s | error=%v",
			cacheKey,
			err,
		)
	}

	log.Printf(
		"Redis cache MISS | shortCode=%s",
		shortCode,
	)

	// --------------------------------------------------
	// 2. Try to acquire distributed lock
	// --------------------------------------------------

	lockValue := uuid.NewString()

	start = time.Now()

	acquired, err := s.redisRepo.AcquireLock(
		ctx,
		lockKey,
		lockValue,
		10*time.Second,
	)

	log.Printf(
		"Redis SETNX | key=%s | duration=%v",
		lockKey,
		time.Since(start),
	)

	if err != nil {
		log.Printf(
			"Redis lock error | key=%s | error=%v",
			lockKey,
			err,
		)

		// Redis lock unavailable.
		// Continue to PostgreSQL.
		acquired = false
	} else if acquired {
		log.Printf(
			"Lock acquired | key=%s",
			lockKey,
		)
	} else {
		log.Printf(
			"Lock already acquired | key=%s",
			lockKey,
		)
	}

	// --------------------------------------------------
	// 3. Another request owns the lock
	// --------------------------------------------------

	if !acquired && err == nil {

		log.Printf(
			"Waiting for cache | shortCode=%s",
			shortCode,
		)

		for i := 0; i < 300; i++ {

			time.Sleep(50 * time.Millisecond)

			cachedURL, err := s.redisRepo.Get(
				ctx,
				cacheKey,
			)

			if err == nil {

				log.Printf(
					"Cache populated by another request | shortCode=%s",
					shortCode,
				)

				return &repository.URL{
					ShortCode:   &shortCode,
					OriginalURL: &cachedURL,
				}, "redis", nil
			}

			if err != redis.Nil {

				log.Printf(
					"Redis GET error while waiting | key=%s | error=%v",
					cacheKey,
					err,
				)

				break
			}
		}

		log.Printf(
			"Cache was not populated | falling back to PostgreSQL | shortCode=%s",
			shortCode,
		)
	}

	// --------------------------------------------------
	// 4. PostgreSQL
	// --------------------------------------------------

	log.Printf(
		"🔥 DB FALLBACK | shortCode=%s",
		shortCode,
	)

	start = time.Now()

	url, err := s.urlRepo.GetByShortCode(
		ctx,
		shortCode,
	)

	log.Printf(
		"PostgreSQL SELECT | shortCode=%s | duration=%v",
		shortCode,
		time.Since(start),
	)

	if err != nil {

		if acquired {
			log.Printf(
				"Releasing lock after DB error | key=%s",
				lockKey,
			)

			if err := s.redisRepo.ReleaseLock(
				ctx,
				lockKey,
				lockValue,
			); err != nil {
				log.Printf(
					"Redis lock release error | key=%s | error=%v",
					lockKey,
					err,
				)
			}
		}

		return nil, "", err
	}

	log.Println("🐌 Simulating slow DB/cache population")
	time.Sleep(12 * time.Second)

	// --------------------------------------------------
	// 5. Populate Redis
	// --------------------------------------------------

	start = time.Now()

	err = s.redisRepo.Set(
		ctx,
		cacheKey,
		*url.OriginalURL,
		5*time.Minute,
	)

	log.Printf(
		"Redis SET | key=%s | duration=%v",
		cacheKey,
		time.Since(start),
	)

	if err != nil {
		log.Printf(
			"Redis SET error | key=%s | error=%v",
			cacheKey,
			err,
		)
	}

	// --------------------------------------------------
	// 6. Release lock
	// --------------------------------------------------

	if acquired {

		log.Printf(
			"Releasing lock | key=%s",
			lockKey,
		)

		if err := s.redisRepo.ReleaseLock(
			ctx,
			lockKey,
			lockValue,
		); err != nil {
			log.Printf(
				"Redis lock release error | key=%s | error=%v",
				lockKey,
				err,
			)
		} else {
			log.Printf(
				"Lock released | key=%s",
				lockKey,
			)
		}
	}

	return url, "postgres", nil
}
