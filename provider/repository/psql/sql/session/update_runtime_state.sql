WITH updated AS (
    UPDATE sessions
    SET
        updated_at = :updated_at,
        status = :status,
        metadata = :metadata,
        ended_at = :ended_at,
        end_reason = :end_reason
    WHERE id = :id
      AND status <> 'terminated'
    RETURNING
        id
)
SELECT id FROM updated
UNION ALL
SELECT id FROM sessions
WHERE id = :id
  AND NOT EXISTS (SELECT 1 FROM updated)
