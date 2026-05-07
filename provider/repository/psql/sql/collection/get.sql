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
WHERE session_id = :session_id
  AND collection_id = :collection_id
  AND payer = :payer
  AND service_provider = :service_provider
  AND data_service = :data_service
