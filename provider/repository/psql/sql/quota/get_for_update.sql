-- Get quota usage for a payer and lock the row for reservation
SELECT
    payer,
    active_sessions,
    active_workers,
    last_updated
FROM quota_usage
WHERE payer = :payer
FOR UPDATE
