package main

import (
	"context"
	"log"
	"os"
	handler "redis-lab/internal/controllers"
	repository "redis-lab/internal/repositories"
	service "redis-lab/internal/services"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

func main() {

	// --------------------------------------------------
	// Environment
	// --------------------------------------------------

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	ctx := context.Background()

	// --------------------------------------------------
	// PostgreSQL
	// --------------------------------------------------

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

	// --------------------------------------------------
	// Redis
	// --------------------------------------------------

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

	// --------------------------------------------------
	// Dependencies
	// --------------------------------------------------

	urlRepository := repository.NewURLRepository(db)
	redisRepository := repository.NewRedisRepository(redisClient)

	urlService := service.NewURLService(
		urlRepository,
		redisRepository,
	)

	urlHandler := handler.NewURLHandler(
		urlService,
	)

	// --------------------------------------------------
	// Routes
	// --------------------------------------------------

	router := gin.Default()

	router.POST(
		"/urls",
		urlHandler.CreateURL(),
	)

	router.GET(
		"/urls/:shortCode",
		urlHandler.GetURL(),
	)

	// --------------------------------------------------
	// Server
	// --------------------------------------------------

	log.Println("Server running on :8080")

	if err := router.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
