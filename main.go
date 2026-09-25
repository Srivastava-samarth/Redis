package main

import (
	"context"
	"log"
	"os"
	// "time"

	controller "redis-lab/controllers"
	"redis-lab/repository"
	service "redis-lab/services"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	ctx := context.Background()

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	// PostgreSQL
	dsn := "host=" + os.Getenv("DB_HOST") +
		" user=" + os.Getenv("DB_USER") +
		" password=" + os.Getenv("DB_PASSWORD") +
		" dbname=" + os.Getenv("DB_NAME") +
		" port=" + os.Getenv("DB_PORT")

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("Failed to connect to PostgreSQL:", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal("Failed to get SQL DB:", err)
	}

	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetMaxIdleConns(10)

	log.Println("PostgreSQL connection pool configured")

	log.Println("Connected to PostgreSQL")

// 	go func() {
//     ticker := time.NewTicker(1 * time.Second)
//     defer ticker.Stop()

//     for range ticker.C {
//         stats := sqlDB.Stats()

//         log.Printf(
//             "DB Pool — Open: %d | InUse: %d | Idle: %d | WaitCount: %d | WaitDuration: %v",
//             stats.OpenConnections,
//             stats.InUse,
//             stats.Idle,
//             stats.WaitCount,
//             stats.WaitDuration,
//         )
//     }
// }()

	// Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6380",
	})

	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatal("Redis connection failed:", err)
	}

	log.Println("Connected to Redis")

	// Dependencies
	urlRepository := repository.NewURLRepository(db)
	urlService := service.NewURLService(
		urlRepository,
		redisClient,
	)
	urlController := controller.NewURLController(urlService)

	// Router
	router := gin.Default()

	router.POST("/urls", urlController.CreateURL)
	router.GET("/urls/:shortCode", urlController.GetURL)

	log.Println("Server running on :8080")

	if err := router.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
