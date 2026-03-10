-- Increment quota usage for a payer
INSERT INTO quota_usage (
    payer,
    active_sessions,
    active_workers,
    last_updated
) VALUES (
    :payer,
    :sessions,
    :workers,
    CURRENT_TIMESTAMP
)
ON CONFLICT (payer) DO UPDATE SET
    active_sessions = quota_usage.active_sessions + EXCLUDED.active_sessions,
    active_workers = quota_usage.active_workers + EXCLUDED.active_workers,
    last_updated = CURRENT_TIMESTAMP
RETURNING
    payer,
    active_sessions,
    active_workers,
    last_updated
