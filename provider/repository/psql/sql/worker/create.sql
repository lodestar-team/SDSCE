-- Create a new worker record
INSERT INTO workers (
    key,
    session_id,
    payer,
    created_at,
    trace_id
) VALUES (
    :key,
    :session_id,
    :payer,
    :created_at,
    :trace_id
)
RETURNING
    key,
    session_id,
    payer,
    created_at,
    trace_id
