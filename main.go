package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type CreateURLRequest struct {
	URL string `json:"url" binding:"required"`
}

func main() {
	ctx := context.Background()

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	// PostgreSQL
	dsn := "postgres://" +
		os.Getenv("DB_USER") + ":" +
		os.Getenv("DB_PASSWORD") + "@" +
		os.Getenv("DB_HOST") + ":" +
		os.Getenv("DB_PORT") + "/" +
		os.Getenv("DB_NAME")

	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal("Failed to create PostgreSQL pool:", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		log.Fatal("PostgreSQL connection failed:", err)
	}

	log.Println("Connected to PostgreSQL")

	// Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6380",
	})
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatal("Redis connection failed:", err)
	}

	log.Println("Connected to Redis")

	router := gin.Default()

	router.POST("/urls", func(c *gin.Context) {
		var req CreateURLRequest
		clientID := c.GetHeader("X-Client-ID")
		if clientID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "X-Client-ID header is required",
			})
			return
		}

		idempotencyKey := c.GetHeader("Idempotency-Key")

		if idempotencyKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Idempotency-Key header is required",
			})
			return
		}

		idempotencyRedisKey := fmt.Sprintf(
			"idempotency:%s:%s",
			clientID,
			idempotencyKey,
		)

		storedShortCode, errG := redisClient.Get(ctx, idempotencyRedisKey).Result()

		if errG == nil {
			if storedShortCode == "processing" {
				c.JSON(http.StatusConflict, gin.H{
					"error": "request is already being processed",
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"short_code": storedShortCode,
			})
			return
		}

		if errG != redis.Nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to check idempotency key",
			})
			return
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "url is required",
			})
			return
		}

		// idempotency doesn't exist so create a key
		wasSet, errSI := redisClient.SetNX(ctx, idempotencyRedisKey, "processing", 24*time.Hour).Result()
		if errSI != nil {
			log.Fatal("error setting idempotecny key:", errSI)
		}

		if !wasSet {
			c.JSON(http.StatusConflict, gin.H{
				"error": "request is already being processed",
			})
			return
		}

		time.Sleep(10 * time.Second)

		var shortCode string
		counter := fmt.Sprintf("rate-limit:%s", clientID)
		// setting counter to 0, if does not exist
		_, errS := redisClient.SetNX(ctx, counter, 0, 60*time.Second).Result()
		if errS != nil {
			log.Fatal("error setting counter:", errS)
		}

		// increment the counter
		id, errI := redisClient.Incr(ctx, counter).Result()
		if errI != nil {
			log.Fatal("error incrementing value:", errI)
		}

		if id > 5 {
			_, err := redisClient.Del(ctx, idempotencyRedisKey).Result()
			if err != nil {
				log.Println("error deleting idempotency key:", err)
			}

			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}

		counter2 := "url:id"
		// increment the counter
		id2, errII := redisClient.Incr(ctx, counter2).Result()
		if errII != nil {
			log.Fatal("error incrementing value:", errII)
		}

		shortCode = "test" + strconv.FormatInt(id2, 10)

		err := db.QueryRow(
			c,
			"INSERT INTO urls (client_id, short_code, original_url) VALUES ($1, $2, $3) RETURNING short_code",
			clientID,
			shortCode,
			req.URL,
		).Scan(&shortCode)

		if err != nil {
			log.Println("Failed to create URL:", err)

			_, err := redisClient.Del(ctx, idempotencyRedisKey).Result()
			if err != nil {
				log.Println("error deleting idempotency key:", err)
			}

			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to create URL",
			})
			return
		}

		_, errIU := redisClient.Set(
			ctx,
			idempotencyRedisKey,
			shortCode,
			24*time.Hour,
		).Result()

		if errIU != nil {
			log.Println("error updating the key:", errIU)
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"short_code": shortCode,
			"url":        req.URL,
		})
	})

	router.GET("/urls/:shortCode", func(c *gin.Context) {
		shortCode := c.Param("shortCode")

		var originalURL string

		err := db.QueryRow(
			c,
			"SELECT original_url FROM urls WHERE short_code = $1",
			shortCode,
		).Scan(&originalURL)

		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "URL not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"url": originalURL,
		})
	})

	log.Println("Server running on :8080")

	if err := router.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
