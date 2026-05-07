SELECT
    session_id,
    collection_id,
    payer,
    service_provider,
    data_service,
    rav_timestamp_ns,
    value_aggregate,
    rav_metadata,
    rav_signature,
    state,
    attempt_count,
    last_tx_hash,
    last_error,
    collected_amount,
    created_at,
    updated_at
FROM collection_records
ORDER BY updated_at DESC
