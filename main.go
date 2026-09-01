package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type CreateURLRequest struct {
	OriginalURL string `json:"original_url" binding:"required,url"`
}

type URL struct {
	ID          int64  `json:"id"`
	ShortCode   string `json:"short_code"`
	OriginalURL string `json:"original_url"`
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
    Addr:              "localhost:6380",
    MaxRetries:        -1,
    DialerRetries:     1,
    DialTimeout:       100 * time.Millisecond,
    DialerRetryTimeout: 0,
    ReadTimeout:       100 * time.Millisecond,
    WriteTimeout:      100 * time.Millisecond,
})

	// if err := redisClient.Ping(ctx).Err(); err != nil {
	// 	log.Fatal("Redis connection failed:", err)
	// }

	log.Println("Connected to Redis")

	router := gin.Default()

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
			 VALUES ('abc124', $1)
			 RETURNING id, short_code, original_url`,
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

		c.JSON(http.StatusCreated, url)
	})

	router.GET("/urls/:shortCode", func(c *gin.Context) {
		shortCode := c.Param("shortCode")
		ctx := c.Request.Context()

		// 1. Check Redis
		start := time.Now()

		cachedURL, err := redisClient.Get(ctx, "url:"+shortCode).Result()

		log.Printf("Redis GET | duration=%v", time.Since(start))

		if err == nil {
			// Cache HIT
			log.Println("Redis cache HIT")

			c.JSON(http.StatusOK, gin.H{
				"short_code":   shortCode,
				"original_url": cachedURL,
				"source":       "redis",
			})
			return
		}

		if err != redis.Nil {
			// Redis failed for some reason.
			// For now, we'll log it and continue to PostgreSQL.
			log.Println("Redis error:", err)
		}

		// 2. Cache MISS → PostgreSQL
		log.Println("Redis cache MISS")

		var url URL

		startdb := time.Now()

		err = db.QueryRow(
			ctx,
			`SELECT id, short_code, original_url
				FROM urls
				WHERE short_code = $1`,
			shortCode,
		).Scan(&url.ID, &url.ShortCode, &url.OriginalURL)

		log.Printf("PostgreSQL SELECT | duration=%v", time.Since(startdb))

		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "URL not found",
			})
			return
		}

		// 3. Store result in Redis
		err = redisClient.Set(
			ctx,
			"url:"+shortCode,
			url.OriginalURL,
			5*time.Minute,
		).Err()

		if err != nil {
			log.Println("Redis SET error:", err)
		}

		// 4. Return response
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
