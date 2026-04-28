-- Apply provider-authoritative metered usage to the session aggregates.
UPDATE sessions
SET
    blocks_processed = blocks_processed + :blocks_delta,
    bytes_transferred = bytes_transferred + :bytes_delta,
    requests = requests + :requests_delta,
    total_cost = COALESCE(total_cost, 0) + :cost_delta
WHERE id = :id
RETURNING
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
