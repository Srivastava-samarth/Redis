package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type URL struct {
	ID          *int64
	ShortCode   *string
	OriginalURL *string
}

type URLRepository struct {
	db *pgxpool.Pool
}

func NewURLRepository(db *pgxpool.Pool) *URLRepository {
	return &URLRepository{
		db: db,
	}
}

func (r *URLRepository) Create(
	ctx context.Context,
	shortCode string,
	originalURL string,
) (*URL, error) {

	var url URL

	err := r.db.QueryRow(
		ctx,
		`INSERT INTO urls (short_code, original_url)
		 VALUES ($1, $2)
		 RETURNING id, short_code, original_url`,
		shortCode,
		originalURL,
	).Scan(
		&url.ID,
		&url.ShortCode,
		&url.OriginalURL,
	)

	if err != nil {
		return nil, err
	}

	return &url, nil
}

func (r *URLRepository) GetByShortCode(
	ctx context.Context,
	shortCode string,
) (*URL, error) {

	var url URL

	err := r.db.QueryRow(
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

	if err != nil {
		return nil, err
	}

	return &url, nil
}