-- 010_ratings_and_reviews.sql

CREATE TABLE IF NOT EXISTS order_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    author_id UUID NOT NULL REFERENCES users(id),
    target_id UUID NOT NULL REFERENCES users(id),
    author_role VARCHAR(20) NOT NULL CHECK (author_role IN ('CUSTOMER', 'EXECUTOR')),
    rating INT NOT NULL CHECK (rating >= 1 AND rating <= 5),
    tags JSONB DEFAULT '[]'::jsonb,
    comment TEXT,
    photos JSONB DEFAULT '[]'::jsonb,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT unique_order_author UNIQUE (order_id, author_id)
);

CREATE INDEX IF NOT EXISTS idx_order_reviews_target ON order_reviews(target_id);
CREATE INDEX IF NOT EXISTS idx_order_reviews_order ON order_reviews(order_id);

ALTER TABLE customer_profiles
    ADD COLUMN IF NOT EXISTS rating NUMERIC(3,2) NOT NULL DEFAULT 5.00,
    ADD COLUMN IF NOT EXISTS reviews_count INT NOT NULL DEFAULT 0;

ALTER TABLE executor_profiles
    ADD COLUMN IF NOT EXISTS rating NUMERIC(3,2) NOT NULL DEFAULT 5.00,
    ADD COLUMN IF NOT EXISTS reviews_count INT NOT NULL DEFAULT 0;
