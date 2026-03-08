package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/swarm-emotions/orchestrator/internal/model"
)

type Client struct {
	pool *pgxpool.Pool
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

	client := &Client{pool: pool}
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
	return c.pool.Ping(probeCtx)
}

func (c *Client) GetAgentConfig(ctx context.Context, agentID string) (*model.AgentConfig, error) {
	const query = `
SELECT agent_id, display_name, baseline, w_matrix, w_dimension, weights, decay_lambda, noise_enabled, noise_sigma
FROM agent_configs
WHERE agent_id = $1
`
	var (
		cfg          model.AgentConfig
		baselineJSON []byte
		wMatrixJSON  []byte
		weightsJSON  []byte
	)
	err := c.pool.QueryRow(ctx, query, agentID).Scan(
		&cfg.AgentID,
		&cfg.DisplayName,
		&baselineJSON,
		&wMatrixJSON,
		&cfg.WDimension,
		&weightsJSON,
		&cfg.DecayLambda,
		&cfg.NoiseEnabled,
		&cfg.NoiseSigma,
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
	cfg.Baseline = baseline

	cfg.WMatrix, err = decodeFloat32Slice(wMatrixJSON)
	if err != nil {
		return nil, fmt.Errorf("decode w_matrix: %w", err)
	}

	cfg.Weights, err = decodeScoreWeights(weightsJSON)
	if err != nil {
		return nil, fmt.Errorf("decode weights: %w", err)
	}

	return &cfg, nil
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
	_, err = c.pool.Exec(ctx, query,
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
	return err
}

func (c *Client) ListAgentConfigs(ctx context.Context) ([]model.AgentConfig, error) {
	const query = `
SELECT agent_id, display_name, baseline, w_matrix, w_dimension, weights, decay_lambda, noise_enabled, noise_sigma
FROM agent_configs
ORDER BY agent_id
`
	rows, err := c.pool.Query(ctx, query)
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
}

func (c *Client) DeleteAgentConfig(ctx context.Context, agentID string) error {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM interaction_log WHERE agent_id = $1`, agentID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM emotion_history WHERE agent_id = $1`, agentID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM cognitive_contexts WHERE agent_id = $1`, agentID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM agent_configs WHERE agent_id = $1`, agentID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (c *Client) GetCognitiveContext(ctx context.Context, agentID string) (*model.CognitiveContext, error) {
	const query = `
SELECT active_goals, norms, beliefs, (EXTRACT(EPOCH FROM updated_at) * 1000)::bigint
FROM cognitive_contexts
WHERE agent_id = $1
`
	var (
		goalsJSON   []byte
		normsJSON   []byte
		beliefsJSON []byte
		updatedAtMs int64
	)
	err := c.pool.QueryRow(ctx, query, agentID).Scan(&goalsJSON, &normsJSON, &beliefsJSON, &updatedAtMs)
	if err != nil {
		if err == pgx.ErrNoRows {
			return &model.CognitiveContext{AgentID: agentID}, nil
		}
		return nil, err
	}

	var goals []string
	if err := json.Unmarshal(goalsJSON, &goals); err != nil {
		return nil, err
	}

	var norms map[string]any
	if err := json.Unmarshal(normsJSON, &norms); err != nil {
		return nil, err
	}
	var beliefs map[string]any
	if err := json.Unmarshal(beliefsJSON, &beliefs); err != nil {
		return nil, err
	}

	return &model.CognitiveContext{
		AgentID:        agentID,
		Goals:          goals,
		Constraints:    readStringSlice(norms["constraints"]),
		WorkingSummary: readString(beliefs["working_summary"]),
		UpdatedAtMs:    updatedAtMs,
	}, nil
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

	goalsJSON, err := json.Marshal(cognitive.Goals)
	if err != nil {
		return err
	}
	normsJSON, err := json.Marshal(map[string]any{
		"constraints": cognitive.Constraints,
	})
	if err != nil {
		return err
	}
	beliefsJSON, err := json.Marshal(map[string]any{
		"working_summary": cognitive.WorkingSummary,
	})
	if err != nil {
		return err
	}

	const query = `
INSERT INTO cognitive_contexts (agent_id, active_goals, norms, beliefs, updated_at)
VALUES ($1, $2::jsonb, $3::jsonb, $4::jsonb, to_timestamp($5 / 1000.0))
ON CONFLICT (agent_id) DO UPDATE SET
	active_goals = EXCLUDED.active_goals,
	norms = EXCLUDED.norms,
	beliefs = EXCLUDED.beliefs,
	updated_at = EXCLUDED.updated_at
`
	_, err = c.pool.Exec(ctx, query, agentID, goalsJSON, normsJSON, beliefsJSON, cognitive.UpdatedAtMs)
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
	_, err = c.pool.Exec(ctx, query,
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
	return err
}

func (c *Client) GetInteractionLogs(ctx context.Context, agentID string) ([]model.InteractionLog, error) {
	const query = `
SELECT input_text, llm_response, stimulus_type, fsm_to, emotion_after, intensity, (EXTRACT(EPOCH FROM timestamp) * 1000)::bigint
FROM interaction_log
WHERE agent_id = $1
ORDER BY timestamp ASC
`
	rows, err := c.pool.Query(ctx, query, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]model.InteractionLog, 0)
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
		logs = append(logs, entry)
	}
	return logs, rows.Err()
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
	_, err = c.pool.Exec(ctx, query,
		entry.AgentID,
		entry.CreatedAtMs,
		emotionJSON,
		entry.Intensity,
		entry.FsmState.StateName,
	)
	return err
}

func (c *Client) GetEmotionHistory(ctx context.Context, agentID string) ([]model.EmotionHistoryEntry, error) {
	const query = `
SELECT fsm_state, emotion, intensity, (EXTRACT(EPOCH FROM timestamp) * 1000)::bigint
FROM emotion_history
WHERE agent_id = $1
ORDER BY timestamp ASC
`
	rows, err := c.pool.Query(ctx, query, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]model.EmotionHistoryEntry, 0)
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
		out = append(out, entry)
	}
	return out, rows.Err()
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

func readString(value any) string {
	s, _ := value.(string)
	return s
}

func readStringSlice(value any) []string {
	if value == nil {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
