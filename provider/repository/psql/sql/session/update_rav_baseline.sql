UPDATE sessions
SET
    updated_at = :updated_at,
    baseline_blocks = :baseline_blocks,
    baseline_bytes = :baseline_bytes,
    baseline_reqs = :baseline_reqs,
    baseline_cost = :baseline_cost
WHERE id = :id
RETURNING
    id
