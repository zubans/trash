-- 013_update_app_release_indexes.sql
-- Change unique constraint from (platform, version_name) to (platform, version_code)
-- so that multiple builds of the same versionName can coexist.

DROP INDEX IF EXISTS idx_mobile_app_releases_active;

CREATE UNIQUE INDEX IF NOT EXISTS idx_mobile_app_releases_code
    ON mobile_app_releases(platform, version_code);

CREATE INDEX IF NOT EXISTS idx_mobile_app_releases_active
    ON mobile_app_releases(platform, is_active)
    WHERE is_active = TRUE;
