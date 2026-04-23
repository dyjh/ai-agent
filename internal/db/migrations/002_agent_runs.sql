-- +goose Up
CREATE TABLE IF NOT EXISTS agent_runs (
    run_id TEXT PRIMARY KEY,
    conversation_id TEXT,
    status TEXT NOT NULL,
    current_step TEXT,
    current_step_index INTEGER NOT NULL DEFAULT 0,
    step_count INTEGER NOT NULL DEFAULT 0,
    max_steps INTEGER NOT NULL DEFAULT 0,
    user_message TEXT,
    approval_id TEXT,
    error TEXT,
    state_json JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_runs_status_updated_at
    ON agent_runs (status, updated_at DESC);

CREATE TABLE IF NOT EXISTS agent_run_steps (
    step_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES agent_runs(run_id) ON DELETE CASCADE,
    step_index INTEGER NOT NULL,
    step_type TEXT NOT NULL,
    status TEXT NOT NULL,
    proposal_json JSONB,
    inference_json JSONB,
    policy_json JSONB,
    approval_json JSONB,
    tool_result_json JSONB,
    summary TEXT,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_run_steps_run_id_step_index
    ON agent_run_steps (run_id, step_index);

-- +goose Down
DROP TABLE IF EXISTS agent_run_steps;
DROP TABLE IF EXISTS agent_runs;
