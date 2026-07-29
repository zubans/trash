-- 009_geocoding_cache.sql

CREATE TABLE IF NOT EXISTS geocoding_cache (
    id SERIAL PRIMARY KEY,
    query VARCHAR NOT NULL UNIQUE,
    address VARCHAR NOT NULL,
    lat NUMERIC(18, 8) NOT NULL,
    lon NUMERIC(18, 8) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_geocoding_cache_query ON geocoding_cache(query);
