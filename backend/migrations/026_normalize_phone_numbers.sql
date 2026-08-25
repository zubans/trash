-- Migration 026: normalise stored phone numbers to a single canonical form.
--
-- The service normalises on write (service.normalizePhone), so "89991234567",
-- "+7 999 123-45-67" and "79991234567" can no longer become three accounts for
-- the same person. Existing rows predate that and are converted here.
--
-- This statement was originally added to the UserRepository constructor, where
-- it re-ran on every process start; it belongs in a migration because it is a
-- one-off data fix. The WHERE clause keeps it a no-op once applied.

UPDATE users
SET phone = '+7' || SUBSTRING(REGEXP_REPLACE(phone, '[^0-9]', '', 'g') FROM 2)
WHERE phone NOT LIKE '+7%'
  AND LENGTH(REGEXP_REPLACE(phone, '[^0-9]', '', 'g')) = 11;
