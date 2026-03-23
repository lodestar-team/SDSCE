-- Insert or update RAV for a session
INSERT INTO ravs (
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
) VALUES (
    :session_id,
    :collection_id,
    :payer,
    :service_provider,
    :data_service,
    :timestamp_ns,
    :value_aggregate,
    :metadata,
    :signature,
    :created_at
)
ON CONFLICT (session_id) DO UPDATE SET
    collection_id = EXCLUDED.collection_id,
    payer = EXCLUDED.payer,
    service_provider = EXCLUDED.service_provider,
    data_service = EXCLUDED.data_service,
    timestamp_ns = EXCLUDED.timestamp_ns,
    value_aggregate = EXCLUDED.value_aggregate,
    metadata = EXCLUDED.metadata,
    signature = EXCLUDED.signature,
    created_at = EXCLUDED.created_at
RETURNING
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
