INSERT INTO collection_records (
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
) VALUES (
    :session_id,
    :collection_id,
    :payer,
    :service_provider,
    :data_service,
    :rav_timestamp_ns,
    :value_aggregate,
    :rav_metadata,
    :rav_signature,
    'collectible',
    0,
    NULL,
    NULL,
    NULL,
    :created_at,
    :updated_at
)
ON CONFLICT (session_id, collection_id, payer, service_provider, data_service) DO UPDATE SET
    rav_timestamp_ns = EXCLUDED.rav_timestamp_ns,
    value_aggregate = EXCLUDED.value_aggregate,
    rav_metadata = EXCLUDED.rav_metadata,
    rav_signature = EXCLUDED.rav_signature,
    state = 'collectible',
    attempt_count = 0,
    last_tx_hash = NULL,
    last_error = NULL,
    collected_amount = NULL,
    updated_at = EXCLUDED.updated_at
WHERE collection_records.state IN ('collectible', 'collect_failed_retryable')
   OR (
      collection_records.state = 'collected'
      AND collection_records.value_aggregate < EXCLUDED.value_aggregate
   )
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
