-- Get quota usage for a payer
SELECT
    payer,
    active_sessions,
    active_workers,
    last_updated
FROM quota_usage
WHERE payer = :payer
