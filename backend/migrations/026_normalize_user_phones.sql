-- Migration 026: normalize legacy phone numbers to +7XXXXXXXXXX.
-- Moved here from the users repository constructor (see commit "fix(auth):
-- support email login, un-normalized phone lookup and DB phone migration"),
-- where it ran on every start with the error discarded.
--
-- Idempotent: rows already matching '+7%' are excluded by the WHERE clause.
UPDATE users
SET phone = '+7' || SUBSTRING(REGEXP_REPLACE(phone, '[^0-9]', '', 'g') FROM 2)
WHERE phone NOT LIKE '+7%'
  AND LENGTH(REGEXP_REPLACE(phone, '[^0-9]', '', 'g')) = 11;
