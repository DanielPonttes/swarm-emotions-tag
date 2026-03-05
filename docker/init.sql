CREATE TABLE agent_configs (
    agent_id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    baseline JSONB NOT NULL,
    w_matrix JSONB NOT NULL,
    w_dimension INTEGER NOT NULL DEFAULT 6,
    fsm_transitions JSONB NOT NULL,
    fsm_constraints JSONB DEFAULT '{}'::jsonb,
    weights JSONB NOT NULL DEFAULT '{"alpha": 0.5, "beta": 0.3, "gamma": 0.2}'::jsonb,
    decay_lambda REAL NOT NULL DEFAULT 0.1,
    noise_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    noise_sigma REAL NOT NULL DEFAULT 0.01,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE cognitive_contexts (
    agent_id TEXT PRIMARY KEY REFERENCES agent_configs(agent_id),
    active_goals JSONB NOT NULL DEFAULT '[]'::jsonb,
    beliefs JSONB NOT NULL DEFAULT '{}'::jsonb,
    norms JSONB NOT NULL DEFAULT '{}'::jsonb,
    conversation_phase TEXT NOT NULL DEFAULT 'idle',
    interlocutor_model JSONB DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE interaction_log (
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

CREATE INDEX idx_interaction_log_agent ON interaction_log(agent_id, timestamp DESC);

CREATE TABLE emotion_history (
    id BIGSERIAL PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agent_configs(agent_id),
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    emotion JSONB NOT NULL,
    intensity REAL NOT NULL,
    fsm_state TEXT NOT NULL
);

CREATE INDEX idx_emotion_history_agent ON emotion_history(agent_id, timestamp DESC);

