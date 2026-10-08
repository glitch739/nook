-- 0012_hashed_sessions.sql
-- sessions.id now stores sha256(cookie token) instead of the token
-- itself (ADR 0005 §7). Existing rows hold raw tokens that can't be
-- converted meaningfully, so everyone logs in once more.
DELETE FROM sessions;
