package models

import "time"

type URL struct {
	ID          uint64    `gorm:"primaryKey"`
	ClientID    string    `gorm:"column:client_id;type:varchar(100);not null"`
	ShortCode   string    `gorm:"column:short_code;type:varchar(20);not null;unique"`
	OriginalURL string    `gorm:"column:original_url;not null"`
	CreatedAt   time.Time `gorm:"not null;default:now()"`
}

func (URL) TableName() string {
	return "urls"
}