-- Get RAV for a session
SELECT
    session_id,
    collection_id,
    payer,
    service_provider,
    data_service,
    timestamp_ns,
    value_aggregate,
    metadata,
    signature,
    created_at
FROM ravs
WHERE session_id = :session_id
