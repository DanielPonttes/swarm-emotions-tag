package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/swarm-emotions/orchestrator/internal/model"
	"github.com/swarm-emotions/orchestrator/internal/observability"
	"github.com/swarm-emotions/orchestrator/internal/resilience"
)

type Client struct {
	pool    *pgxpool.Pool
	metrics observability.Reporter
	retry   resilience.RetryPolicy
}

func NewClient(dsn string) (*Client, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.HealthCheckPeriod = 15 * time.Second

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	client := &Client{
		pool:    pool,
		metrics: observability.NewNoopReporter(),
		retry: resilience.RetryPolicy{
			Attempts:  3,
			BaseDelay: 10 * time.Millisecond,
			MaxDelay:  120 * time.Millisecond,
		},
	}
	if err := client.ensureSchema(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ensure postgres schema: %w", err)
	}
	return client, nil
}

func (c *Client) Close() {
	c.pool.Close()
}

func (c *Client) Ready(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	err := c.pool.Ping(probeCtx)
	if err != nil {
		c.metrics.IncDependencyError("postgres", "ready")
	}
	return err
}

func (c *Client) SetMetricsReporter(reporter observability.Reporter) {
	if reporter == nil {
		c.metrics = observability.NewNoopReporter()
		return
	}
	c.metrics = reporter
}

func (c *Client) GetAgentConfig(ctx context.Context, agentID string) (*model.AgentConfig, error) {
	const query = `
SELECT agent_id, display_name, baseline, w_matrix, w_dimension, weights, decay_lambda, noise_enabled, noise_sigma
FROM agent_configs
WHERE agent_id = $1
`
	cfg, err := resilience.DoValue(ctx, c.retry, func(attemptCtx context.Context) (*model.AgentConfig, error) {
		callCtx, cancel := context.WithTimeout(attemptCtx, 300*time.Millisecond)
		defer cancel()

		var (
			item         model.AgentConfig
			baselineJSON []byte
			wMatrixJSON  []byte
			weightsJSON  []byte
		)
		err := c.pool.QueryRow(callCtx, query, agentID).Scan(
			&item.AgentID,
			&item.DisplayName,
			&baselineJSON,
			&wMatrixJSON,
			&item.WDimension,
			&weightsJSON,
			&item.DecayLambda,
			&item.NoiseEnabled,
			&item.NoiseSigma,
		)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, nil
			}
			return nil, err
		}

		baseline, err := decodeEmotionVector(baselineJSON)
		if err != nil {
			return nil, fmt.Errorf("decode baseline: %w", err)
		}
		item.Baseline = baseline

		item.WMatrix, err = decodeFloat32Slice(wMatrixJSON)
		if err != nil {
			return nil, fmt.Errorf("decode w_matrix: %w", err)
		}

		item.Weights, err = decodeScoreWeights(weightsJSON)
		if err != nil {
			return nil, fmt.Errorf("decode weights: %w", err)
		}

		return &item, nil
	})
	if err != nil {
		c.metrics.IncDependencyError("postgres", "get_agent_config")
	}
	return cfg, err
}

func (c *Client) SaveAgentConfig(ctx context.Context, cfg *model.AgentConfig) error {
	if cfg == nil {
		return fmt.Errorf("agent config is nil")
	}
	if cfg.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}

	baselineJSON, err := json.Marshal(cfg.Baseline)
	if err != nil {
		return err
	}
	wMatrixJSON, err := json.Marshal(cfg.WMatrix)
	if err != nil {
		return err
	}
	weightsJSON, err := json.Marshal(cfg.Weights)
	if err != nil {
		return err
	}

	const query = `
INSERT INTO agent_configs (
	agent_id,
	display_name,
	baseline,
	w_matrix,
	w_dimension,
	fsm_transitions,
	fsm_constraints,
	weights,
	decay_lambda,
	noise_enabled,
	noise_sigma,
	updated_at
) VALUES (
	$1, $2, $3::jsonb, $4::jsonb, $5, '{}'::jsonb, '{}'::jsonb, $6::jsonb, $7, $8, $9, NOW()
)
ON CONFLICT (agent_id) DO UPDATE SET
	display_name = EXCLUDED.display_name,
	baseline = EXCLUDED.baseline,
	w_matrix = EXCLUDED.w_matrix,
	w_dimension = EXCLUDED.w_dimension,
	weights = EXCLUDED.weights,
	decay_lambda = EXCLUDED.decay_lambda,
	noise_enabled = EXCLUDED.noise_enabled,
	noise_sigma = EXCLUDED.noise_sigma,
	updated_at = NOW()
`
	err = resilience.Do(ctx, c.retry, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 300*time.Millisecond)
		defer cancel()
		_, execErr := c.pool.Exec(callCtx, query,
			cfg.AgentID,
			cfg.DisplayName,
			baselineJSON,
			wMatrixJSON,
			cfg.WDimension,
			weightsJSON,
			cfg.DecayLambda,
			cfg.NoiseEnabled,
			cfg.NoiseSigma,
		)
		return execErr
	})
	if err != nil {
		c.metrics.IncDependencyError("postgres", "save_agent_config")
	}
	return err
}

func (c *Client) ListAgentConfigs(ctx context.Context) ([]model.AgentConfig, error) {
	const query = `
SELECT agent_id, display_name, baseline, w_matrix, w_dimension, weights, decay_lambda, noise_enabled, noise_sigma
FROM agent_configs
ORDER BY agent_id
`
	items, err := resilience.DoValue(ctx, c.retry, func(attemptCtx context.Context) ([]model.AgentConfig, error) {
		callCtx, cancel := context.WithTimeout(attemptCtx, 300*time.Millisecond)
		defer cancel()

		rows, err := c.pool.Query(callCtx, query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		out := make([]model.AgentConfig, 0)
		for rows.Next() {
			var (
				cfg          model.AgentConfig
				baselineJSON []byte
				wMatrixJSON  []byte
				weightsJSON  []byte
			)
			if err := rows.Scan(
				&cfg.AgentID,
				&cfg.DisplayName,
				&baselineJSON,
				&wMatrixJSON,
				&cfg.WDimension,
				&weightsJSON,
				&cfg.DecayLambda,
				&cfg.NoiseEnabled,
				&cfg.NoiseSigma,
			); err != nil {
				return nil, err
			}
			cfg.Baseline, err = decodeEmotionVector(baselineJSON)
			if err != nil {
				return nil, err
			}
			cfg.WMatrix, err = decodeFloat32Slice(wMatrixJSON)
			if err != nil {
				return nil, err
			}
			cfg.Weights, err = decodeScoreWeights(weightsJSON)
			if err != nil {
				return nil, err
			}
			out = append(out, cfg)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return out, nil
	})
	if err != nil {
		c.metrics.IncDependencyError("postgres", "list_agent_configs")
	}
	return items, err
}

func (c *Client) DeleteAgentConfig(ctx context.Context, agentID string) error {
	err := resilience.Do(ctx, c.retry, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 300*time.Millisecond)
		defer cancel()

		tx, err := c.pool.Begin(callCtx)
		if err != nil {
			return err
		}
		defer tx.Rollback(callCtx)

		if _, err := tx.Exec(callCtx, `DELETE FROM interaction_log WHERE agent_id = $1`, agentID); err != nil {
			return err
		}
		if _, err := tx.Exec(callCtx, `DELETE FROM emotion_history WHERE agent_id = $1`, agentID); err != nil {
			return err
		}
		if _, err := tx.Exec(callCtx, `DELETE FROM cognitive_contexts WHERE agent_id = $1`, agentID); err != nil {
			return err
		}
		if _, err := tx.Exec(callCtx, `DELETE FROM agent_configs WHERE agent_id = $1`, agentID); err != nil {
			return err
		}
		return tx.Commit(callCtx)
	})
	if err != nil {
		c.metrics.IncDependencyError("postgres", "delete_agent_config")
	}
	return err
}

func (c *Client) GetCognitiveContext(ctx context.Context, agentID string) (*model.CognitiveContext, error) {
	const query = `
SELECT active_goals, norms, beliefs, conversation_phase, (EXTRACT(EPOCH FROM updated_at) * 1000)::bigint
FROM cognitive_contexts
WHERE agent_id = $1
`
	value, err := resilience.DoValue(ctx, c.retry, func(attemptCtx context.Context) (*model.CognitiveContext, error) {
		callCtx, cancel := context.WithTimeout(attemptCtx, 300*time.Millisecond)
		defer cancel()

		var (
			goalsJSON   []byte
			normsJSON   []byte
			beliefsJSON []byte
			phase       string
			updatedAtMs int64
		)
		err := c.pool.QueryRow(callCtx, query, agentID).Scan(&goalsJSON, &normsJSON, &beliefsJSON, &phase, &updatedAtMs)
		if err != nil {
			if err == pgx.ErrNoRows {
				return model.DefaultCognitiveContext(agentID), nil
			}
			return nil, err
		}

		activeGoals, legacyGoals, err := decodeCognitiveGoals(goalsJSON)
		if err != nil {
			return nil, err
		}

		var norms model.CognitiveNorms
		if err := json.Unmarshal(normsJSON, &norms); err != nil {
			return nil, err
		}
		var beliefs model.CognitiveBeliefs
		if err := json.Unmarshal(beliefsJSON, &beliefs); err != nil {
			return nil, err
		}

		cognitive := &model.CognitiveContext{
			AgentID:           agentID,
			Goals:             legacyGoals,
			ActiveGoals:       activeGoals,
			Constraints:       norms.Constraints,
			Norms:             norms,
			Beliefs:           beliefs,
			ConversationPhase: phase,
			WorkingSummary:    beliefs.WorkingSummary,
			UpdatedAtMs:       updatedAtMs,
		}
		cognitive.Normalize()
		return cognitive, nil
	})
	if err != nil {
		c.metrics.IncDependencyError("postgres", "get_cognitive_context")
	}
	return value, err
}

func (c *Client) UpdateCognitiveContext(ctx context.Context, agentID string, cognitive *model.CognitiveContext) error {
	if cognitive == nil {
		return fmt.Errorf("cognitive context is nil")
	}
	if agentID == "" {
		agentID = cognitive.AgentID
	}
	if agentID == "" {
		return fmt.Errorf("agent_id is required")
	}

	if cognitive.UpdatedAtMs == 0 {
		cognitive.UpdatedAtMs = time.Now().UnixMilli()
	}
	cognitive.Normalize()

	goalsJSON, err := json.Marshal(cognitive.ActiveGoals)
	if err != nil {
		return err
	}
	normsJSON, err := json.Marshal(cognitive.Norms)
	if err != nil {
		return err
	}
	beliefsJSON, err := json.Marshal(cognitive.Beliefs)
	if err != nil {
		return err
	}

	const query = `
INSERT INTO cognitive_contexts (agent_id, active_goals, norms, beliefs, conversation_phase, updated_at)
VALUES ($1, $2::jsonb, $3::jsonb, $4::jsonb, $5, to_timestamp($6 / 1000.0))
ON CONFLICT (agent_id) DO UPDATE SET
	active_goals = EXCLUDED.active_goals,
	norms = EXCLUDED.norms,
	beliefs = EXCLUDED.beliefs,
	conversation_phase = EXCLUDED.conversation_phase,
	updated_at = EXCLUDED.updated_at
`
	err = resilience.Do(ctx, c.retry, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 300*time.Millisecond)
		defer cancel()
		_, execErr := c.pool.Exec(
			callCtx,
			query,
			agentID,
			goalsJSON,
			normsJSON,
			beliefsJSON,
			cognitive.ConversationPhase,
			cognitive.UpdatedAtMs,
		)
		return execErr
	})
	if err != nil {
		c.metrics.IncDependencyError("postgres", "update_cognitive_context")
	}
	return err
}

func (c *Client) LogInteraction(ctx context.Context, entry *model.InteractionLog) error {
	if entry == nil {
		return fmt.Errorf("interaction log is nil")
	}
	if entry.CreatedAtMs == 0 {
		entry.CreatedAtMs = time.Now().UnixMilli()
	}
	emotionJSON, err := json.Marshal(entry.Emotion)
	if err != nil {
		return err
	}

	const query = `
INSERT INTO interaction_log (
	agent_id,
	timestamp,
	input_text,
	stimulus_type,
	fsm_to,
	emotion_after,
	intensity,
	llm_response,
	latency_ms,
	trace_id
) VALUES (
	$1,
	to_timestamp($2 / 1000.0),
	$3,
	$4,
	$5,
	$6::jsonb,
	$7,
	$8,
	$9,
	$10
)
`
	err = resilience.Do(ctx, resilience.RetryPolicy{
		Attempts:  1,
		BaseDelay: c.retry.BaseDelay,
		MaxDelay:  c.retry.MaxDelay,
	}, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 300*time.Millisecond)
		defer cancel()
		_, execErr := c.pool.Exec(callCtx, query,
			entry.AgentID,
			entry.CreatedAtMs,
			entry.UserText,
			entry.Stimulus,
			entry.FsmState.StateName,
			emotionJSON,
			entry.Intensity,
			entry.ResponseText,
			0,
			"",
		)
		return execErr
	})
	if err != nil {
		c.metrics.IncDependencyError("postgres", "log_interaction")
	}
	return err
}

func (c *Client) GetInteractionLogs(ctx context.Context, agentID string) ([]model.InteractionLog, error) {
	const query = `
SELECT input_text, llm_response, stimulus_type, fsm_to, emotion_after, intensity, (EXTRACT(EPOCH FROM timestamp) * 1000)::bigint
FROM interaction_log
WHERE agent_id = $1
ORDER BY timestamp ASC
`
	logs, err := resilience.DoValue(ctx, c.retry, func(attemptCtx context.Context) ([]model.InteractionLog, error) {
		callCtx, cancel := context.WithTimeout(attemptCtx, 300*time.Millisecond)
		defer cancel()

		rows, err := c.pool.Query(callCtx, query, agentID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		result := make([]model.InteractionLog, 0)
		for rows.Next() {
			var (
				entry       model.InteractionLog
				fsmState    string
				emotionJSON []byte
			)
			if err := rows.Scan(
				&entry.UserText,
				&entry.ResponseText,
				&entry.Stimulus,
				&fsmState,
				&emotionJSON,
				&entry.Intensity,
				&entry.CreatedAtMs,
			); err != nil {
				return nil, err
			}
			entry.AgentID = agentID
			entry.FsmState = model.FsmState{StateName: fsmState}
			entry.Emotion, err = decodeEmotionVector(emotionJSON)
			if err != nil {
				return nil, err
			}
			result = append(result, entry)
		}
		return result, rows.Err()
	})
	if err != nil {
		c.metrics.IncDependencyError("postgres", "get_interaction_logs")
	}
	return logs, err
}

func (c *Client) AppendEmotionHistory(ctx context.Context, entry *model.EmotionHistoryEntry) error {
	if entry == nil {
		return fmt.Errorf("emotion history entry is nil")
	}
	if entry.CreatedAtMs == 0 {
		entry.CreatedAtMs = time.Now().UnixMilli()
	}
	emotionJSON, err := json.Marshal(entry.Emotion)
	if err != nil {
		return err
	}

	const query = `
INSERT INTO emotion_history (agent_id, timestamp, emotion, intensity, fsm_state)
VALUES ($1, to_timestamp($2 / 1000.0), $3::jsonb, $4, $5)
`
	err = resilience.Do(ctx, resilience.RetryPolicy{
		Attempts:  1,
		BaseDelay: c.retry.BaseDelay,
		MaxDelay:  c.retry.MaxDelay,
	}, func(attemptCtx context.Context) error {
		callCtx, cancel := context.WithTimeout(attemptCtx, 300*time.Millisecond)
		defer cancel()
		_, execErr := c.pool.Exec(callCtx, query,
			entry.AgentID,
			entry.CreatedAtMs,
			emotionJSON,
			entry.Intensity,
			entry.FsmState.StateName,
		)
		return execErr
	})
	if err != nil {
		c.metrics.IncDependencyError("postgres", "append_emotion_history")
	}
	return err
}

func (c *Client) GetEmotionHistory(ctx context.Context, agentID string) ([]model.EmotionHistoryEntry, error) {
	const query = `
SELECT fsm_state, emotion, intensity, (EXTRACT(EPOCH FROM timestamp) * 1000)::bigint
FROM emotion_history
WHERE agent_id = $1
ORDER BY timestamp ASC
`
	out, err := resilience.DoValue(ctx, c.retry, func(attemptCtx context.Context) ([]model.EmotionHistoryEntry, error) {
		callCtx, cancel := context.WithTimeout(attemptCtx, 300*time.Millisecond)
		defer cancel()

		rows, err := c.pool.Query(callCtx, query, agentID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		result := make([]model.EmotionHistoryEntry, 0)
		for rows.Next() {
			var (
				entry       model.EmotionHistoryEntry
				fsmState    string
				emotionJSON []byte
			)
			if err := rows.Scan(&fsmState, &emotionJSON, &entry.Intensity, &entry.CreatedAtMs); err != nil {
				return nil, err
			}
			entry.AgentID = agentID
			entry.FsmState = model.FsmState{StateName: fsmState}
			entry.Emotion, err = decodeEmotionVector(emotionJSON)
			if err != nil {
				return nil, err
			}
			result = append(result, entry)
		}
		return result, rows.Err()
	})
	if err != nil {
		c.metrics.IncDependencyError("postgres", "get_emotion_history")
	}
	return out, err
}

func (c *Client) ensureSchema(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS agent_configs (
	agent_id TEXT PRIMARY KEY,
	display_name TEXT NOT NULL,
	baseline JSONB NOT NULL,
	w_matrix JSONB NOT NULL,
	w_dimension INTEGER NOT NULL DEFAULT 6,
	fsm_transitions JSONB NOT NULL DEFAULT '{}'::jsonb,
	fsm_constraints JSONB NOT NULL DEFAULT '{}'::jsonb,
	weights JSONB NOT NULL DEFAULT '{"alpha": 0.4, "beta": 0.3, "gamma": 0.3, "pseudoperm_boost": 0.2}'::jsonb,
	decay_lambda REAL NOT NULL DEFAULT 0.1,
	noise_enabled BOOLEAN NOT NULL DEFAULT FALSE,
	noise_sigma REAL NOT NULL DEFAULT 0.01,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cognitive_contexts (
	agent_id TEXT PRIMARY KEY REFERENCES agent_configs(agent_id),
	active_goals JSONB NOT NULL DEFAULT '[]'::jsonb,
	beliefs JSONB NOT NULL DEFAULT '{}'::jsonb,
	norms JSONB NOT NULL DEFAULT '{}'::jsonb,
	conversation_phase TEXT NOT NULL DEFAULT 'idle',
	interlocutor_model JSONB DEFAULT '{}'::jsonb,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS interaction_log (
	id BIGSERIAL PRIMARY KEY,
	agent_id TEXT NOT NULL REFERENCES agent_configs(agent_id),
	timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	input_text TEXT,
	stimulus_type TEXT,
	fsm_from TEXT,
	fsm_to TEXT,
	emotion_before JSONB,
	emotion_after JSONB,
	intensity REAL,
	llm_response TEXT,
	latency_ms INTEGER,
	trace_id TEXT
);
CREATE INDEX IF NOT EXISTS idx_interaction_log_agent ON interaction_log(agent_id, timestamp DESC);

CREATE TABLE IF NOT EXISTS emotion_history (
	id BIGSERIAL PRIMARY KEY,
	agent_id TEXT NOT NULL REFERENCES agent_configs(agent_id),
	timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	emotion JSONB NOT NULL,
	intensity REAL NOT NULL,
	fsm_state TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_emotion_history_agent ON emotion_history(agent_id, timestamp DESC);
`
	_, err := c.pool.Exec(ctx, ddl)
	return err
}

func decodeEmotionVector(raw []byte) (model.EmotionVector, error) {
	var vector model.EmotionVector
	if err := json.Unmarshal(raw, &vector); err == nil && len(vector.Components) > 0 {
		return vector, nil
	}

	components, err := decodeFloat32Slice(raw)
	if err != nil {
		return model.EmotionVector{}, err
	}
	return model.EmotionVector{Components: components}, nil
}

func decodeFloat32Slice(raw []byte) ([]float32, error) {
	var values []float32
	if err := json.Unmarshal(raw, &values); err == nil {
		return values, nil
	}

	var values64 []float64
	if err := json.Unmarshal(raw, &values64); err != nil {
		return nil, err
	}
	values = make([]float32, len(values64))
	for i, value := range values64 {
		values[i] = float32(value)
	}
	return values, nil
}

func decodeScoreWeights(raw []byte) (model.ScoreWeights, error) {
	var weights model.ScoreWeights
	if err := json.Unmarshal(raw, &weights); err != nil {
		return model.ScoreWeights{}, err
	}
	return weights, nil
}

func decodeCognitiveGoals(raw []byte) ([]model.CognitiveGoal, []string, error) {
	var goals []model.CognitiveGoal
	if err := json.Unmarshal(raw, &goals); err == nil {
		legacy := make([]string, 0, len(goals))
		for _, goal := range goals {
			if goal.Description != "" {
				legacy = append(legacy, goal.Description)
			}
		}
		return goals, legacy, nil
	}

	var legacyGoals []string
	if err := json.Unmarshal(raw, &legacyGoals); err != nil {
		return nil, nil, err
	}

	goals = make([]model.CognitiveGoal, 0, len(legacyGoals))
	for i, goal := range legacyGoals {
		goals = append(goals, model.CognitiveGoal{
			ID:          fmt.Sprintf("goal_%d", i+1),
			Description: goal,
			Priority:    1.0 - float32(i)*0.15,
		})
	}
	return goals, legacyGoals, nil
}
