-- List all sessions (filtering is done in Go code)
SELECT
    id,
    created_at,
    updated_at,
    last_keep_alive,
    status,
    metadata,
    ended_at,
    end_reason,
    payer,
    receiver,
    data_service,
    signer,
    blocks_processed,
    bytes_transferred,
    requests,
    total_cost,
    baseline_blocks,
    baseline_bytes,
    baseline_reqs,
    baseline_cost
FROM sessions
ORDER BY created_at DESC
