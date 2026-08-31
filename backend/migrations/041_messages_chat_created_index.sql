-- 041_messages_chat_created_index.sql
-- The order-chat history query filters by chat_id and sorts by created_at:
--   SELECT ... FROM messages WHERE chat_id = $1 AND is_deleted = false
--   ORDER BY created_at ASC
-- The existing (chat_id, status) index serves the filter but not the sort, so a
-- busy chat still sorts in memory. A (chat_id, created_at) index serves both the
-- filter and the ordering, keeping history reads cheap under load.

CREATE INDEX IF NOT EXISTS idx_messages_chat_created
    ON messages (chat_id, created_at);
