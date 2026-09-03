package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type CreateURLRequest struct {
	ShortCode   *string `json:"short_code" binding:"required"`
	OriginalURL *string `json:"original_url" binding:"required,url"`
}

type URL struct {
	ID          *int64  `json:"id"`
	ShortCode   *string `json:"short_code"`
	OriginalURL *string `json:"original_url"`
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	ctx := context.Background()

	dsn := "postgres://" +
		os.Getenv("DB_USER") + ":" +
		os.Getenv("DB_PASSWORD") + "@" +
		os.Getenv("DB_HOST") + ":" +
		os.Getenv("DB_PORT") + "/" +
		os.Getenv("DB_NAME")

	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatal("Database connection failed:", err)
	}

	log.Println("Connected to PostgreSQL")

	redisClient := redis.NewClient(&redis.Options{
		Addr:               "localhost:6380",
		MaxRetries:         -1,
		DialerRetries:      1,
		DialTimeout:        100 * time.Millisecond,
		DialerRetryTimeout: 0,
		ReadTimeout:        100 * time.Millisecond,
		WriteTimeout:       100 * time.Millisecond,
	})

	log.Println("Connected to Redis")

	router := gin.Default()

	// --------------------------------------------------
	// CREATE URL
	// --------------------------------------------------

	router.POST("/urls", func(c *gin.Context) {
		var req CreateURLRequest

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		var url URL

		err := db.QueryRow(
			c,
			`INSERT INTO urls (short_code, original_url)
			 VALUES ($1, $2)
			 RETURNING id, short_code, original_url`,
			req.ShortCode,
			req.OriginalURL,
		).Scan(
			&url.ID,
			&url.ShortCode,
			&url.OriginalURL,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			return
		}

		// Cache the URL in Redis.
		key := "url:" + *url.ShortCode

		err = redisClient.Set(
			c,
			key,
			*url.OriginalURL,
			5*time.Minute,
		).Err()

		if err != nil {
			log.Printf("Redis SET failed for key %s: %v", key, err)
		}

		c.JSON(http.StatusCreated, url)
	})

	// --------------------------------------------------
	// GET URL
	// --------------------------------------------------

	router.GET("/urls/:shortCode", func(c *gin.Context) {
		shortCode := c.Param("shortCode")
		ctx := c.Request.Context()

		cacheKey := "url:" + shortCode
		lockKey := "lock:" + cacheKey

		// --------------------------------------------------
		// 1. Check Redis
		// --------------------------------------------------

		start := time.Now()

		cachedURL, err := redisClient.Get(
			ctx,
			cacheKey,
		).Result()

		log.Printf(
			"Redis GET | duration=%v",
			time.Since(start),
		)

		if err == nil {
			log.Println("Redis cache HIT")

			c.JSON(http.StatusOK, gin.H{
				"short_code":   shortCode,
				"original_url": cachedURL,
				"source":       "redis",
			})
			return
		}

		if err != redis.Nil {
			log.Println("Redis error:", err)
		}

		log.Println("Redis cache MISS")

		// --------------------------------------------------
		// 2. Try to acquire distributed lock
		// --------------------------------------------------

		lockValue := uuid.NewString()

		acquired, err := redisClient.SetNX(
			ctx,
			lockKey,
			lockValue,
			10*time.Second,
		).Result()

		if err != nil {
			log.Println("Redis lock error:", err)

			// If Redis lock fails, fallback to PostgreSQL.
			// This preserves availability.
			acquired = true
		}

		if !acquired {
			// --------------------------------------------------
			// Another request is already loading this key.
			// Wait and check Redis again.
			// --------------------------------------------------

			log.Println("Lock already acquired, waiting for cache")

			for i := 0; i < 20; i++ {
				time.Sleep(50 * time.Millisecond)

				cachedURL, err := redisClient.Get(
					ctx,
					cacheKey,
				).Result()

				if err == nil {
					log.Println("Cache populated by another request")

					c.JSON(http.StatusOK, gin.H{
						"short_code":   shortCode,
						"original_url": cachedURL,
						"source":       "redis",
					})
					return
				}

				if err != redis.Nil {
					log.Println("Redis GET error while waiting:", err)
					break
				}
			}

			// If we reach here, the lock holder didn't populate
			// Redis within the expected time.
			log.Println("Cache was not populated, falling back to PostgreSQL")
		}

		// --------------------------------------------------
		// 3. PostgreSQL
		// --------------------------------------------------

		log.Println("🔥 DB FALLBACK | shortCode=", shortCode)

		var url URL

		startdb := time.Now()

		err = db.QueryRow(
			ctx,
			`SELECT id, short_code, original_url
			 FROM urls
			 WHERE short_code = $1`,
			shortCode,
		).Scan(
			&url.ID,
			&url.ShortCode,
			&url.OriginalURL,
		)

		log.Printf(
			"PostgreSQL SELECT | duration=%v",
			time.Since(startdb),
		)

		if err != nil {
			// Release lock if we own it.
			if acquired {
				redisClient.Del(ctx, lockKey)
			}

			c.JSON(http.StatusNotFound, gin.H{
				"error": "URL not found",
			})
			return
		}

		// --------------------------------------------------
		// 4. Store result in Redis
		// --------------------------------------------------

		err = redisClient.Set(
			ctx,
			cacheKey,
			*url.OriginalURL,
			5*time.Minute,
		).Err()

		if err != nil {
			log.Println("Redis SET error:", err)
		}

		// --------------------------------------------------
		// 5. Release lock
		// --------------------------------------------------

		if acquired {
			err = redisClient.Del(
				ctx,
				lockKey,
			).Err()

			if err != nil {
				log.Println("Redis lock release error:", err)
			}
		}

		// --------------------------------------------------
		// 6. Return response
		// --------------------------------------------------

		c.JSON(http.StatusOK, gin.H{
			"id":           url.ID,
			"short_code":   url.ShortCode,
			"original_url": url.OriginalURL,
			"source":       "postgres",
		})
	})

	log.Println("Server running on :8080")

	if err := router.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}