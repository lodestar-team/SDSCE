-- Get a worker by key
SELECT
    key,
    session_id,
    payer,
    created_at,
    trace_id
FROM workers
WHERE key = :key
