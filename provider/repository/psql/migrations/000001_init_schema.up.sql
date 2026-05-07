-- Reusable trigger function for updated_at
-- CURRENT_TIMESTAMP returns TIMESTAMPTZ which is automatically stored in UTC
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Sessions table (NO string address fields - BYTEA only)
CREATE TABLE sessions (
    id VARCHAR(255) PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_keep_alive TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status VARCHAR(50) NOT NULL,
    metadata JSONB,
    ended_at TIMESTAMPTZ,
    end_reason INTEGER,

    -- Escrow addresses - BYTEA with CHECK constraints
    payer BYTEA NOT NULL CHECK (length(payer) = 20),
    receiver BYTEA NOT NULL CHECK (length(receiver) = 20),
    data_service BYTEA NOT NULL CHECK (length(data_service) = 20),
    signer BYTEA NOT NULL CHECK (length(signer) = 20),

    -- Usage tracking
    blocks_processed BIGINT NOT NULL DEFAULT 0,
    bytes_transferred BIGINT NOT NULL DEFAULT 0,
    requests BIGINT NOT NULL DEFAULT 0,
    total_cost NUMERIC,

    -- Baseline snapshots
    baseline_blocks BIGINT NOT NULL DEFAULT 0,
    baseline_bytes BIGINT NOT NULL DEFAULT 0,
    baseline_reqs BIGINT NOT NULL DEFAULT 0,
    baseline_cost NUMERIC
);

CREATE INDEX idx_sessions_payer ON sessions(payer);
CREATE INDEX idx_sessions_status ON sessions(status);
CREATE INDEX idx_sessions_created_at ON sessions(created_at);

CREATE TRIGGER sessions_updated_at
BEFORE UPDATE ON sessions
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

-- RAVs table (one-to-one with sessions)
CREATE TABLE ravs (
    session_id VARCHAR(255) PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,

    -- RAV message fields with CHECK constraints
    collection_id BYTEA NOT NULL CHECK (length(collection_id) = 32),
    payer BYTEA NOT NULL CHECK (length(payer) = 20),
    service_provider BYTEA NOT NULL CHECK (length(service_provider) = 20),
    data_service BYTEA NOT NULL CHECK (length(data_service) = 20),
    timestamp_ns BIGINT NOT NULL,
    value_aggregate NUMERIC NOT NULL,
    metadata BYTEA,

    -- Signature (always present)
    signature BYTEA NOT NULL CHECK (length(signature) = 65),

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Collection lifecycle records (settlement state, separate from runtime sessions)
CREATE TABLE collection_records (
    session_id VARCHAR(255) NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,

    -- Settlement tuple
    collection_id BYTEA NOT NULL CHECK (length(collection_id) = 32),
    payer BYTEA NOT NULL CHECK (length(payer) = 20),
    service_provider BYTEA NOT NULL CHECK (length(service_provider) = 20),
    data_service BYTEA NOT NULL CHECK (length(data_service) = 20),

    -- Accepted RAV snapshot for this lifecycle record
    rav_timestamp_ns BIGINT NOT NULL,
    value_aggregate NUMERIC NOT NULL,
    rav_metadata BYTEA,
    rav_signature BYTEA NOT NULL CHECK (length(rav_signature) = 65),

    -- Collection lifecycle state
    state VARCHAR(50) NOT NULL CHECK (state IN ('collectible', 'collect_pending', 'collected', 'collect_failed_retryable')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_tx_hash TEXT,
    last_error TEXT,
    collected_amount NUMERIC,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (session_id, collection_id, payer, service_provider, data_service)
);

CREATE INDEX idx_collection_records_state ON collection_records(state);
CREATE INDEX idx_collection_records_payer ON collection_records(payer);
CREATE INDEX idx_collection_records_updated_at ON collection_records(updated_at);

-- Workers table
CREATE TABLE workers (
    key VARCHAR(255) PRIMARY KEY,
    session_id VARCHAR(255) NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    payer BYTEA NOT NULL CHECK (length(payer) = 20),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    trace_id VARCHAR(255)
);

CREATE INDEX idx_workers_session_id ON workers(session_id);
CREATE INDEX idx_workers_payer ON workers(payer);

-- Quota usage table
CREATE TABLE quota_usage (
    payer BYTEA PRIMARY KEY CHECK (length(payer) = 20),
    active_sessions INTEGER NOT NULL DEFAULT 0,
    active_workers INTEGER NOT NULL DEFAULT 0,
    last_updated TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Note: No trigger needed since SQL explicitly sets last_updated = CURRENT_TIMESTAMP

-- Usage events table
CREATE TABLE usage_events (
    id SERIAL PRIMARY KEY,
    session_id VARCHAR(255) NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    blocks BIGINT NOT NULL DEFAULT 0,
    bytes BIGINT NOT NULL DEFAULT 0,
    requests BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX idx_usage_events_session_id ON usage_events(session_id);
CREATE INDEX idx_usage_events_timestamp ON usage_events(timestamp);
