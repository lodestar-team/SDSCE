UPDATE collection_records
SET
    state = 'collect_pending',
    attempt_count = attempt_count + 1,
    last_tx_hash = :last_tx_hash,
    last_error = NULL,
    updated_at = :updated_at
WHERE session_id = :session_id
  AND collection_id = :collection_id
  AND payer = :payer
  AND service_provider = :service_provider
  AND data_service = :data_service
  AND value_aggregate = :expected_value
  AND state IN ('collectible', 'collect_failed_retryable')
RETURNING
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
