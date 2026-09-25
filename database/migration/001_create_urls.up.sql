CREATE TABLE urls (
    id BIGSERIAL PRIMARY KEY,
    client_id VARCHAR(100) NOT NULL,
    short_code VARCHAR(20) NOT NULL,
    original_url TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT urls_short_code_key UNIQUE (short_code),
    CONSTRAINT urls_client_id_original_url_key UNIQUE (client_id, original_url)
);