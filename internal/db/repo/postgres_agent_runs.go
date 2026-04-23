package repo

import (
	"context"
	"encoding/json"

	"local-agent/internal/core"
)

func (p *postgresRuns) UpsertRun(ctx context.Context, run core.AgentRunRecord) error {
	stateJSON, err := json.Marshal(run.StateJSON)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO agent_runs (
			run_id, conversation_id, status, current_step, current_step_index, step_count,
			max_steps, user_message, approval_id, error, state_json, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (run_id) DO UPDATE
		SET conversation_id = EXCLUDED.conversation_id,
			status = EXCLUDED.status,
			current_step = EXCLUDED.current_step,
			current_step_index = EXCLUDED.current_step_index,
			step_count = EXCLUDED.step_count,
			max_steps = EXCLUDED.max_steps,
			user_message = EXCLUDED.user_message,
			approval_id = EXCLUDED.approval_id,
			error = EXCLUDED.error,
			state_json = EXCLUDED.state_json,
			updated_at = EXCLUDED.updated_at
	`, run.RunID, run.ConversationID, run.Status, run.CurrentStep, run.CurrentStepIndex, run.StepCount,
		run.MaxSteps, run.UserMessage, run.ApprovalID, run.Error, stateJSON, run.CreatedAt, run.UpdatedAt)
	return err
}

func (p *postgresRuns) GetRun(ctx context.Context, runID string) (*core.AgentRunRecord, error) {
	var (
		item      core.AgentRunRecord
		stateJSON []byte
	)
	err := p.pool.QueryRow(ctx, `
		SELECT run_id, conversation_id, status, current_step, current_step_index, step_count,
		       max_steps, user_message, approval_id, error, state_json, created_at, updated_at
		FROM agent_runs
		WHERE run_id = $1
	`, runID).Scan(
		&item.RunID, &item.ConversationID, &item.Status, &item.CurrentStep, &item.CurrentStepIndex,
		&item.StepCount, &item.MaxSteps, &item.UserMessage, &item.ApprovalID, &item.Error,
		&stateJSON, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(stateJSON) > 0 {
		_ = json.Unmarshal(stateJSON, &item.StateJSON)
	}
	return &item, nil
}

func (p *postgresRuns) ListRunsByStatus(ctx context.Context, statuses []string, limit int) ([]core.AgentRunRecord, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT run_id, conversation_id, status, current_step, current_step_index, step_count,
		       max_steps, user_message, approval_id, error, state_json, created_at, updated_at
		FROM agent_runs
		WHERE cardinality($1::text[]) = 0 OR status = ANY($1::text[])
		ORDER BY updated_at DESC
		LIMIT CASE WHEN $2 <= 0 THEN 100 ELSE $2 END
	`, statuses, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []core.AgentRunRecord
	for rows.Next() {
		var (
			item      core.AgentRunRecord
			stateJSON []byte
		)
		if err := rows.Scan(
			&item.RunID, &item.ConversationID, &item.Status, &item.CurrentStep, &item.CurrentStepIndex,
			&item.StepCount, &item.MaxSteps, &item.UserMessage, &item.ApprovalID, &item.Error,
			&stateJSON, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(stateJSON) > 0 {
			_ = json.Unmarshal(stateJSON, &item.StateJSON)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *postgresRuns) DeleteRun(ctx context.Context, runID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM agent_runs WHERE run_id = $1`, runID)
	return err
}

func (p *postgresRunSteps) UpsertStep(ctx context.Context, step core.AgentRunStepRecord) error {
	proposalJSON, err := json.Marshal(step.ProposalJSON)
	if err != nil {
		return err
	}
	inferenceJSON, err := json.Marshal(step.InferenceJSON)
	if err != nil {
		return err
	}
	policyJSON, err := json.Marshal(step.PolicyJSON)
	if err != nil {
		return err
	}
	approvalJSON, err := json.Marshal(step.ApprovalJSON)
	if err != nil {
		return err
	}
	resultJSON, err := json.Marshal(step.ToolResultJSON)
	if err != nil {
		return err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO agent_run_steps (
			step_id, run_id, step_index, step_type, status,
			proposal_json, inference_json, policy_json, approval_json, tool_result_json,
			summary, error, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (step_id) DO UPDATE
		SET status = EXCLUDED.status,
			proposal_json = EXCLUDED.proposal_json,
			inference_json = EXCLUDED.inference_json,
			policy_json = EXCLUDED.policy_json,
			approval_json = EXCLUDED.approval_json,
			tool_result_json = EXCLUDED.tool_result_json,
			summary = EXCLUDED.summary,
			error = EXCLUDED.error,
			updated_at = EXCLUDED.updated_at
	`, step.StepID, step.RunID, step.StepIndex, step.StepType, step.Status,
		proposalJSON, inferenceJSON, policyJSON, approvalJSON, resultJSON,
		step.Summary, step.Error, step.CreatedAt, step.UpdatedAt)
	return err
}

func (p *postgresRunSteps) ListStepsByRun(ctx context.Context, runID string) ([]core.AgentRunStepRecord, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT step_id, run_id, step_index, step_type, status,
		       proposal_json, inference_json, policy_json, approval_json, tool_result_json,
		       summary, error, created_at, updated_at
		FROM agent_run_steps
		WHERE run_id = $1
		ORDER BY step_index ASC, created_at ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []core.AgentRunStepRecord
	for rows.Next() {
		var (
			item                                    core.AgentRunStepRecord
			proposalJSON, inferenceJSON, policyJSON []byte
			approvalJSON, resultJSON                []byte
		)
		if err := rows.Scan(
			&item.StepID, &item.RunID, &item.StepIndex, &item.StepType, &item.Status,
			&proposalJSON, &inferenceJSON, &policyJSON, &approvalJSON, &resultJSON,
			&item.Summary, &item.Error, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(proposalJSON) > 0 {
			_ = json.Unmarshal(proposalJSON, &item.ProposalJSON)
		}
		if len(inferenceJSON) > 0 {
			_ = json.Unmarshal(inferenceJSON, &item.InferenceJSON)
		}
		if len(policyJSON) > 0 {
			_ = json.Unmarshal(policyJSON, &item.PolicyJSON)
		}
		if len(approvalJSON) > 0 {
			_ = json.Unmarshal(approvalJSON, &item.ApprovalJSON)
		}
		if len(resultJSON) > 0 {
			_ = json.Unmarshal(resultJSON, &item.ToolResultJSON)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *postgresRunSteps) DeleteStepsByRun(ctx context.Context, runID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM agent_run_steps WHERE run_id = $1`, runID)
	return err
}
