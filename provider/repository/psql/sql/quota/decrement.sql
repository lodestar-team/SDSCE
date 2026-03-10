-- Decrement quota usage for a payer
UPDATE quota_usage SET
    active_sessions = GREATEST(0, active_sessions - :sessions),
    active_workers = GREATEST(0, active_workers - :workers),
    last_updated = CURRENT_TIMESTAMP
WHERE payer = :payer
RETURNING
    payer,
    active_sessions,
    active_workers,
    last_updated
