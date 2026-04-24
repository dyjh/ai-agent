-- +goose Up
ALTER TABLE agent_run_steps
    ADD COLUMN IF NOT EXISTS code_plan_json JSONB;

-- +goose Down
ALTER TABLE agent_run_steps
    DROP COLUMN IF EXISTS code_plan_json;

