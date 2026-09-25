package repository

import (
	"redis-lab/database/models"

	"gorm.io/gorm"
)

type URLRepository struct {
	db *gorm.DB
}

func NewURLRepository(db *gorm.DB) *URLRepository {
	return &URLRepository{
		db: db,
	}
}

func (r *URLRepository) Create(url *models.URL) error {
	return r.db.Create(url).Error
}

func (r *URLRepository) GetByClientAndURL(
	clientID string,
	originalURL string,
) (*models.URL, error) {
	var url models.URL

	err := r.db.
		Where("client_id = ? AND original_url = ?", clientID, originalURL).
		Take(&url).Error

	if err != nil {
		return nil, err
	}

	return &url, nil
}

func (r *URLRepository) GetByShortCode(
	shortCode string,
) (*models.URL, error) {
	var url models.URL

	err := r.db.
		Where("short_code = ?", shortCode).
		First(&url).Error

	if err != nil {
		return nil, err
	}

	return &url, nil
}
