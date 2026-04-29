WITH touched AS (
    UPDATE sessions
    SET
        updated_at = :updated_at,
        last_keep_alive = :last_keep_alive
    WHERE id = :id
      AND last_keep_alive < :last_keep_alive
    RETURNING
        id
)
SELECT id FROM touched
UNION ALL
SELECT id FROM sessions
WHERE id = :id
  AND NOT EXISTS (SELECT 1 FROM touched)
