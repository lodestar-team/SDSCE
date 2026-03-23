-- Add a usage event for a session
INSERT INTO usage_events (
    session_id,
    timestamp,
    blocks,
    bytes,
    requests
) VALUES (
    :session_id,
    :timestamp,
    :blocks,
    :bytes,
    :requests
)
RETURNING
    id,
    session_id,
    timestamp,
    blocks,
    bytes,
    requests
