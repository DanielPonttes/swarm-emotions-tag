// ── Configuration ───────────────────────────────────────────────────────────
const MAX_INTENSITY = Math.sqrt(6);
const DEFAULT_AGENT_ID = "sentient-console";

// ── Emotion Model (6 dimensions from backend) ──────────────────────────────
const DIMENSIONS = [
  { index: 0, label: "Valencia",   positive: "positivo",   negative: "negativo",     explanation: "Polaridade afetiva do texto." },
  { index: 1, label: "Arousal",    positive: "ativado",    negative: "sereno",        explanation: "Nivel de ativacao fisiologica percebida." },
  { index: 2, label: "Dominancia", positive: "agencia",    negative: "pressao",       explanation: "Sensacao de controle sobre o contexto." },
  { index: 3, label: "Certeza",    positive: "clareza",    negative: "ambiguidade",   explanation: "Confianca na interpretacao corrente." },
  { index: 4, label: "Social",     positive: "proximidade",negative: "distancia",     explanation: "Carga relacional e empatica do texto." },
  { index: 5, label: "Novidade",   positive: "exploracao", negative: "estabilidade",  explanation: "Nivel de surpresa ou descoberta." },
];

const FSM_LABELS = {
  neutral: "Neutral", joyful: "Joyful", curious: "Curious", empathetic: "Empathetic",
  calm: "Calm", worried: "Worried", frustrated: "Frustrated", anxious: "Anxious",
};

const MACRO_LABELS = {
  positive: "Macroestado positivo", neutral: "Macroestado neutro", negative: "Macroestado negativo",
};

const MACRO_BY_STATE = {
  joyful: "positive", curious: "positive", empathetic: "positive",
  neutral: "neutral", calm: "neutral",
  worried: "negative", frustrated: "negative", anxious: "negative",
};

const SUGGESTIONS = [
  "Estou sobrecarregado com varias entregas e preciso de clareza.",
  "Recebi uma critica dura e nao sei como responder.",
  "Algo finalmente funcionou e quero consolidar o aprendizado.",
  "Estou curioso sobre qual tom usar numa conversa dificil.",
];

// ── Application State ───────────────────────────────────────────────────────
const state = {
  agents: [],
  activeAgentId: "",
  history: [],
  interactions: [],
  agentState: null,
  transcript: [],
  ready: { status: "checking", detail: "Verificando disponibilidade do backend." },
  metrics: { intensity: 0, latencyMs: null, traceId: "" },
  busy: false,
};

// ── DOM References ──────────────────────────────────────────────────────────
const ui = {
  readyDot:          document.getElementById("ready-dot"),
  readyLabel:        document.getElementById("ready-label"),
  agentSelect:       document.getElementById("agent-select"),
  refreshBtn:        document.getElementById("refresh-btn"),
  createAgentForm:   document.getElementById("create-agent-form"),
  agentNameInput:    document.getElementById("agent-name-input"),
  agentIdInput:      document.getElementById("agent-id-input"),
  interactionForm:   document.getElementById("interaction-form"),
  textInput:         document.getElementById("text-input"),
  submitBtn:         document.getElementById("submit-btn"),
  streamStatus:      document.getElementById("stream-status"),
  suggestionRow:     document.getElementById("suggestion-row"),
  fsmState:          document.getElementById("fsm-state"),
  macroState:        document.getElementById("macro-state"),
  latencyValue:      document.getElementById("latency-value"),
  traceId:           document.getElementById("trace-id"),
  gauge:             document.getElementById("intensity-gauge"),
  intensityValue:    document.getElementById("intensity-value"),
  intensityCopy:     document.getElementById("intensity-copy"),
  dominantAxis:      document.getElementById("dominant-axis"),
  dominantAxisCopy:  document.getElementById("dominant-axis-copy"),
  socialSignal:      document.getElementById("social-signal"),
  socialSignalCopy:  document.getElementById("social-signal-copy"),
  noveltySignal:     document.getElementById("novelty-signal"),
  noveltySignalCopy: document.getElementById("novelty-signal-copy"),
  toneChipCloud:     document.getElementById("tone-chip-cloud"),
  fsmContext:        document.getElementById("fsm-context"),
  vectorMap:         document.getElementById("vector-map"),
  dimensionBars:     document.getElementById("dimension-bars"),
  historySparkline:  document.getElementById("history-sparkline"),
  historyList:       document.getElementById("history-list"),
  transcriptList:    document.getElementById("transcript-list"),
  toast:             document.getElementById("toast"),
};

// ── Boot ────────────────────────────────────────────────────────────────────
boot().catch((err) => showToast(err.message || "Falha ao inicializar."));

async function boot() {
  renderSuggestionButtons();
  bindEvents();
  await Promise.all([loadReadyStatus(), loadAgents()]);
  render();
}

function bindEvents() {
  ui.refreshBtn.addEventListener("click", () => refreshAgentData());
  ui.agentSelect.addEventListener("change", (e) => setActiveAgent(e.target.value));
  ui.createAgentForm.addEventListener("submit", handleCreateAgent);
  ui.interactionForm.addEventListener("submit", handleInteractionSubmit);
  ui.agentNameInput.addEventListener("input", () => {
    if (!ui.agentIdInput.value.trim()) {
      ui.agentIdInput.value = slugify(ui.agentNameInput.value);
    }
  });
}

function renderSuggestionButtons() {
  ui.suggestionRow.innerHTML = "";
  for (const s of SUGGESTIONS) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "suggestion-button";
    btn.textContent = s;
    btn.addEventListener("click", () => {
      ui.textInput.value = s;
      ui.textInput.focus();
    });
    ui.suggestionRow.appendChild(btn);
  }
}

// ── API Layer ───────────────────────────────────────────────────────────────
async function fetchJSON(url, options = {}) {
  const response = await fetch(url, {
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options,
  });
  if (!response.ok) throw new Error(await extractError(response));
  return response.json();
}

async function extractError(response) {
  try {
    const payload = await response.json();
    return payload.error || payload.message || `${response.status} ${response.statusText}`;
  } catch {
    return `${response.status} ${response.statusText}`;
  }
}

// ── Data Loading ────────────────────────────────────────────────────────────
async function loadReadyStatus() {
  try {
    const payload = await fetchJSON("/ready");
    state.ready = {
      status: payload.status === "ready" ? "ready" : "error",
      detail: payload.status === "ready"
        ? "Dependencias prontas para processar interacoes."
        : (payload.error || "Backend ainda nao esta pronto."),
    };
  } catch (err) {
    state.ready = { status: "error", detail: err.message || "Nao foi possivel consultar /ready." };
  }
  renderReadyStatus();
}

async function loadAgents() {
  const payload = await fetchJSON("/api/v1/agents/");
  state.agents = Array.isArray(payload.agents) ? payload.agents : [];
  if (!state.agents.length) {
    await createAgent(DEFAULT_AGENT_ID, "Sentient Console");
    return loadAgents();
  }
  const stored = window.localStorage.getItem("swarm-active-agent");
  const next = state.agents.some((a) => a.agent_id === stored) ? stored : state.agents[0].agent_id;
  await setActiveAgent(next, { persist: false });
}

async function setActiveAgent(agentID, options = {}) {
  if (!agentID) return;
  state.activeAgentId = agentID;
  if (options.persist !== false) window.localStorage.setItem("swarm-active-agent", agentID);
  renderAgentOptions();
  await refreshAgentData();
}

async function refreshAgentData(options = {}) {
  if (!state.activeAgentId) return false;
  const expectedText = String(options.expectedUserText || "").trim();
  const retries = Number(options.retries || 0);
  const delay = Number(options.retryDelayMs || 180);

  try {
    const id = encodeURIComponent(state.activeAgentId);
    const [agentState, historyPayload, interactionsPayload] = await Promise.all([
      fetchJSON(`/api/v1/agents/${id}/state`),
      fetchJSON(`/api/v1/agents/${id}/history`),
      fetchJSON(`/api/v1/agents/${id}/interactions`),
    ]);
    state.agentState = agentState;
    state.history = Array.isArray(historyPayload.history) ? historyPayload.history : [];
    state.interactions = Array.isArray(interactionsPayload.interactions) ? interactionsPayload.interactions : [];
    state.metrics.intensity = state.history[0]?.intensity ?? computeIntensity(agentState?.current_emotion?.components || []);

    const persisted = !expectedText || state.interactions.some((e) => String(e.user_text || "").trim() === expectedText);
    if (!persisted && retries > 0) {
      await sleep(delay);
      return refreshAgentData({ expectedUserText: expectedText, retries: retries - 1, retryDelayMs: delay });
    }
    if (!state.busy && persisted) state.transcript = [];
    render();
    return persisted;
  } catch (err) {
    showToast(err.message || "Falha ao sincronizar o estado do agente.");
    return false;
  }
}

async function handleCreateAgent(event) {
  event.preventDefault();
  const displayName = ui.agentNameInput.value.trim();
  const rawId = ui.agentIdInput.value.trim() || displayName || DEFAULT_AGENT_ID;
  const agentID = slugify(rawId) || DEFAULT_AGENT_ID;
  try {
    await createAgent(agentID, displayName || humanize(agentID));
    ui.createAgentForm.reset();
    await loadAgents();
    await setActiveAgent(agentID);
    showToast(`Agente ${agentID} pronto para uso.`, "success");
  } catch (err) {
    showToast(err.message || "Nao foi possivel criar o agente.");
  }
}

async function createAgent(agentID, displayName) {
  await fetchJSON("/api/v1/agents/", {
    method: "POST",
    body: JSON.stringify({ agent_id: agentID, display_name: displayName }),
  });
}

// ── Interaction (Streaming) ─────────────────────────────────────────────────
async function handleInteractionSubmit(event) {
  event.preventDefault();
  const text = ui.textInput.value.trim();
  if (!text || state.busy) return;
  if (!state.activeAgentId) {
    showToast("Crie ou selecione um agente antes de enviar o texto.");
    return;
  }

  const userMessage = { id: crypto.randomUUID(), role: "user", text, createdAt: Date.now() };
  const assistantMessage = { id: crypto.randomUUID(), role: "assistant", text: "", createdAt: Date.now() };
  state.transcript.push(userMessage, assistantMessage);
  ui.textInput.value = "";
  setBusy(true, "Processando via /api/v1/interact/stream ...");
  renderTranscript();

  try {
    await streamInteraction(text, assistantMessage.id);
    const persisted = await refreshAgentData({ expectedUserText: text, retries: 8, retryDelayMs: 220 });
    if (!persisted) {
      showToast("Interacao concluida; a persistencia ainda nao apareceu na timeline.");
    }
    if (!findTranscript(assistantMessage.id)?.text.trim()) {
      updateTranscript(assistantMessage.id, "Resposta concluida sem chunks de texto.");
    }
  } catch (err) {
    updateTranscript(assistantMessage.id, `Falha ao processar a interacao: ${err.message || "erro desconhecido"}`);
    showToast(err.message || "Falha ao processar a interacao.");
  } finally {
    setBusy(false, "Streaming encerrado.");
    render();
  }
}

async function streamInteraction(text, assistantMessageID) {
  const response = await fetch("/api/v1/interact/stream", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ agent_id: state.activeAgentId, text }),
  });
  if (!response.ok) throw new Error(await extractError(response));

  const contentType = response.headers.get("content-type") || "";
  if (!contentType.includes("text/event-stream") || !response.body) {
    const payload = await response.json();
    handleFallback(payload, assistantMessageID);
    return;
  }

  await parseSSE(response.body, {
    metadata(payload) {
      state.metrics.traceId = payload.trace_id || state.metrics.traceId;
      state.metrics.intensity = Number(payload.intensity || 0);
      state.agentState = {
        agent_id: state.activeAgentId,
        current_emotion: payload.emotion || state.agentState?.current_emotion || { components: [] },
        current_fsm_state: {
          state_name: payload.fsm_state || state.agentState?.current_fsm_state?.state_name || "neutral",
          macro_state: macroFor(payload.fsm_state || "neutral"),
          entered_at_ms: Date.now(),
        },
        updated_at_ms: Date.now(),
      };
      render();
    },
    chunk(payload) {
      const previous = findTranscript(assistantMessageID)?.text || "";
      updateTranscript(assistantMessageID, previous + String(payload.text || ""));
      renderTranscript();
    },
    done(payload) {
      state.metrics.latencyMs = Number(payload.latency_ms || 0);
      renderHeaderMetrics();
    },
    error(payload) {
      throw new Error(payload.error || "Erro no fluxo SSE.");
    },
  });
}

async function parseSSE(stream, handlers) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let currentEvent = "message";
  let dataLines = [];

  while (true) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
    let idx = buffer.indexOf("\n");
    while (idx >= 0) {
      const raw = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 1);
      const line = raw.replace(/\r$/, "");
      if (line === "") {
        await dispatchSSE(currentEvent, dataLines, handlers);
        currentEvent = "message";
        dataLines = [];
      } else if (line.startsWith("event:")) {
        currentEvent = line.slice("event:".length).trim();
      } else if (line.startsWith("data:")) {
        dataLines.push(line.slice("data:".length).trim());
      }
      idx = buffer.indexOf("\n");
    }
    if (done) {
      if (dataLines.length) await dispatchSSE(currentEvent, dataLines, handlers);
      return;
    }
  }
}

async function dispatchSSE(eventName, dataLines, handlers) {
  if (!dataLines.length) return;
  const payload = JSON.parse(dataLines.join("\n"));
  const handler = handlers[eventName];
  if (typeof handler === "function") await handler(payload);
}

function handleFallback(payload, assistantMessageID) {
  updateTranscript(assistantMessageID, String(payload.response || "Resposta recebida sem corpo."));
  state.metrics.traceId = payload.trace_id || "";
  state.metrics.latencyMs = Number(payload.latency_ms || 0);
  state.metrics.intensity = Number(payload.intensity || 0);
  state.agentState = {
    agent_id: state.activeAgentId,
    current_emotion: payload.emotion_state || { components: [] },
    current_fsm_state: {
      state_name: payload.fsm_state || "neutral",
      macro_state: macroFor(payload.fsm_state || "neutral"),
      entered_at_ms: Date.now(),
    },
    updated_at_ms: Date.now(),
  };
  render();
}

// ── Render Pipeline ─────────────────────────────────────────────────────────
function render() {
  renderReadyStatus();
  renderAgentOptions();
  renderHeaderMetrics();
  renderSummary();
  renderFSMContext();
  renderDimensionBars();
  renderVectorMap();
  renderHistory();
  renderTranscript();
}

function renderReadyStatus() {
  const s = state.ready.status;
  ui.readyLabel.textContent = s === "ready" ? "Online" : s === "error" ? "Offline" : "Checando";
  ui.readyDot.className = "status-dot";
  if (s === "ready") ui.readyDot.classList.add("ready");
  else if (s === "error") ui.readyDot.classList.add("error");
}

function renderAgentOptions() {
  const previousValue = ui.agentSelect.value;
  ui.agentSelect.innerHTML = "";
  for (const agent of state.agents) {
    const option = document.createElement("option");
    option.value = agent.agent_id;
    option.textContent = `${agent.display_name || humanize(agent.agent_id)} (${agent.agent_id})`;
    ui.agentSelect.appendChild(option);
  }
  ui.agentSelect.value = state.activeAgentId || previousValue || "";
}

function renderHeaderMetrics() {
  const fsmState = state.agentState?.current_fsm_state?.state_name || "neutral";
  const macroState = state.agentState?.current_fsm_state?.macro_state || macroFor(fsmState);
  ui.fsmState.textContent = FSM_LABELS[fsmState] || humanize(fsmState);
  ui.macroState.textContent = MACRO_LABELS[macroState] || `Macroestado ${macroState}`;
  ui.latencyValue.textContent =
    typeof state.metrics.latencyMs === "number" && state.metrics.latencyMs > 0
      ? `${Math.round(state.metrics.latencyMs)} ms`
      : "--";
  ui.traceId.textContent = state.metrics.traceId ? `trace ${state.metrics.traceId}` : "trace indisponivel";

  const normalizedIntensity = normalizeIntensity(state.metrics.intensity);
  ui.gauge.style.setProperty("--score", String(normalizedIntensity));
  ui.intensityValue.textContent = `${Math.round(normalizedIntensity)}%`;
  ui.intensityCopy.textContent =
    normalizedIntensity > 70
      ? "Leitura intensa: o sistema detecta forte carga afetiva no ultimo turno."
      : normalizedIntensity > 35
        ? "Leitura moderada: ha energia emocional suficiente para influenciar a resposta."
        : "Leitura baixa: o estado atual esta proximo do baseline do agente.";
}

function renderSummary() {
  const vector = currentVector();
  const dominant = topDimensions(vector, 3);
  const social = signedDescriptor(vector[4], DIMENSIONS[4]);
  const novelty = signedDescriptor(vector[5], DIMENSIONS[5]);

  if (!dominant.length) {
    ui.dominantAxis.textContent = "--";
    ui.dominantAxisCopy.textContent = "Sem dados ainda.";
  } else {
    ui.dominantAxis.textContent = dominant[0].label;
    ui.dominantAxisCopy.textContent = dominant[0].explanation;
  }

  ui.socialSignal.textContent = social.label;
  ui.socialSignalCopy.textContent = social.detail;
  ui.noveltySignal.textContent = novelty.label;
  ui.noveltySignalCopy.textContent = novelty.detail;

  ui.toneChipCloud.innerHTML = "";
  const chips = buildToneChips(vector, dominant[0]);
  for (const chipText of chips) {
    const chip = document.createElement("span");
    chip.className = "tone-chip";
    chip.textContent = chipText;
    ui.toneChipCloud.appendChild(chip);
  }
}

function renderFSMContext() {
  const fsmState = state.agentState?.current_fsm_state?.state_name || "neutral";
  const macro = macroFor(fsmState);
  const macroClass = macro === "positive" ? "fsm-positive" : macro === "negative" ? "fsm-negative" : "";

  ui.fsmContext.innerHTML = `
    <div class="fsm-row">
      <span class="fsm-label">Estado FSM</span>
      <span class="fsm-value">${FSM_LABELS[fsmState] || humanize(fsmState)}</span>
    </div>
    <div class="fsm-row">
      <span class="fsm-label">Macro</span>
      <span class="fsm-value ${macroClass}">${MACRO_LABELS[macro] || macro}</span>
    </div>
    <div class="fsm-row">
      <span class="fsm-label">Intensidade</span>
      <span class="fsm-value">${Math.round(normalizeIntensity(state.metrics.intensity))}%</span>
    </div>
  `;
}

function renderDimensionBars() {
  const vector = currentVector();
  ui.dimensionBars.innerHTML = "";

  for (const dim of DIMENSIONS) {
    const value = clamp(vector[dim.index] || 0, -1, 1);
    const row = document.createElement("div");
    row.className = "dimension-row";

    const head = document.createElement("div");
    head.className = "dimension-head";
    head.innerHTML = `
      <div>
        <span class="dimension-name">${dim.label}</span>
        <span class="dimension-explanation">${dim.explanation}</span>
      </div>
      <span class="${value >= 0 ? "dimension-val-positive" : "dimension-val-negative"}">${value.toFixed(2)}</span>
    `;

    const track = document.createElement("div");
    track.className = "dimension-track";
    const fill = document.createElement("div");
    fill.className = `dimension-fill ${value >= 0 ? "positive" : "negative"}`;
    fill.style.width = `${Math.abs(value) * 50}%`;
    track.appendChild(fill);

    row.append(head, track);
    ui.dimensionBars.appendChild(row);
  }

  ui.historySparkline.innerHTML = buildSparkline(state.history);
}

function renderVectorMap() {
  const vector = currentVector();
  const width = 680;
  const height = 380;
  const centerX = width / 2;
  const centerY = height / 2;
  const angles = [-160, -100, -35, 20, 95, 160];

  const points = DIMENSIONS.map((dim, idx) => {
    const value = clamp(vector[idx] || 0, -1, 1);
    const radius = 90 + Math.abs(value) * 70;
    const angle = (angles[idx] * Math.PI) / 180;
    return {
      dim,
      value,
      x: centerX + Math.cos(angle) * radius,
      y: centerY + Math.sin(angle) * radius,
    };
  });

  const path = buildSmoothPath(points);
  const dominant = topDimensions(vector, 1)[0];
  const macroState = state.agentState?.current_fsm_state?.macro_state || "neutral";
  const fsmState = state.agentState?.current_fsm_state?.state_name || "neutral";

  ui.vectorMap.innerHTML = `
    <svg viewBox="0 0 ${width} ${height}" role="img" aria-label="Mapa vetorial das seis dimensoes emocionais">
      <defs>
        <linearGradient id="vectorStroke" x1="0%" x2="100%" y1="0%" y2="100%">
          <stop offset="0%" stop-color="#ffb55c" />
          <stop offset="50%" stop-color="#b28cff" />
          <stop offset="100%" stop-color="#ff8ab1" />
        </linearGradient>
      </defs>
      <circle cx="${centerX}" cy="${centerY}" fill="none" r="52" stroke="rgba(151,166,209,0.18)" />
      <circle cx="${centerX}" cy="${centerY}" fill="none" r="112" stroke="rgba(151,166,209,0.12)" />
      <circle cx="${centerX}" cy="${centerY}" fill="none" r="172" stroke="rgba(151,166,209,0.08)" />
      <path d="${path}" fill="rgba(178,140,255,0.08)" stroke="url(#vectorStroke)" stroke-width="3"></path>
      ${points
        .map(
          (point) => `
            <line x1="${centerX}" y1="${centerY}" x2="${point.x.toFixed(1)}" y2="${point.y.toFixed(1)}" stroke="rgba(151,166,209,0.12)" />
            <circle cx="${point.x.toFixed(1)}" cy="${point.y.toFixed(1)}" r="${(8 + Math.abs(point.value) * 10).toFixed(1)}" fill="${point.value >= 0 ? "#b28cff" : "#ff8ab1"}" fill-opacity="${(0.45 + Math.abs(point.value) * 0.4).toFixed(2)}" />
            <text x="${point.x.toFixed(1)}" y="${(point.y - 16).toFixed(1)}" fill="#eef2ff" font-size="12" font-family="Space Grotesk" text-anchor="middle">${point.dim.label}</text>
            <text x="${point.x.toFixed(1)}" y="${(point.y + 22).toFixed(1)}" fill="#97a6d1" font-size="11" font-family="Space Grotesk" text-anchor="middle">${point.value.toFixed(2)}</text>
          `
        )
        .join("")}
      <circle cx="${centerX}" cy="${centerY}" r="48" fill="rgba(17,33,70,0.88)" stroke="rgba(178,140,255,0.24)" />
      <text x="${centerX}" y="${centerY - 8}" fill="#eef2ff" font-size="22" font-family="Epilogue" font-weight="700" text-anchor="middle">${FSM_LABELS[fsmState] || "Neutral"}</text>
      <text x="${centerX}" y="${centerY + 18}" fill="#97a6d1" font-size="12" font-family="Space Grotesk" text-anchor="middle">${MACRO_LABELS[macroState] || "Macroestado neutro"}</text>
      <text x="28" y="${height - 28}" fill="#97a6d1" font-size="12" font-family="Space Grotesk">Dominante: ${dominant ? dominant.label : "--"}</text>
    </svg>
  `;
}

function renderHistory() {
  if (!state.history.length) {
    ui.historyList.innerHTML = `<div class="empty-state">Nenhuma persistencia emocional encontrada para este agente ainda.</div>`;
    return;
  }

  ui.historyList.innerHTML = state.history
    .slice(0, 8)
    .map((entry) => {
      const timestamp = formatTimestamp(entry.created_at_ms);
      const intensity = normalizeIntensity(entry.intensity || 0);
      const vector = entry.emotion?.components || [];
      const dominant = topDimensions(vector, 1)[0];
      return `
        <article class="history-item fade-in">
          <div class="history-topline">
            <span class="history-state">${FSM_LABELS[entry.fsm_state?.state_name] || "Neutral"}</span>
            <small class="muted">${timestamp}</small>
          </div>
          <div class="history-metrics">
            <div>
              <span class="summary-label">Intensidade</span>
              <strong>${Math.round(intensity)}%</strong>
            </div>
            <div>
              <span class="summary-label">Dominante</span>
              <strong>${dominant ? dominant.label : "--"}</strong>
            </div>
            <div>
              <span class="summary-label">Macro</span>
              <strong>${MACRO_LABELS[entry.fsm_state?.macro_state] || "--"}</strong>
            </div>
          </div>
        </article>
      `;
    })
    .join("");
}

function renderTranscript() {
  const entries = [...mapInteractionsToTranscript(state.interactions), ...state.transcript];

  if (!entries.length) {
    ui.transcriptList.innerHTML = `<div class="empty-state">Nenhuma conversa persistida ainda. Envie um texto para abrir o fluxo do orquestrador.</div>`;
    return;
  }

  ui.transcriptList.innerHTML = entries
    .map(
      (entry) => `
        <article class="transcript-item ${entry.role} fade-in">
          <div class="transcript-topline">
            <span class="transcript-role">${entry.role === "user" ? "Usuario" : "Orchestrator"}</span>
            <small class="muted">${formatTimestamp(entry.createdAt)}</small>
          </div>
          <div class="transcript-body">${escapeHTML(entry.text || (entry.role === "assistant" ? "Aguardando chunks..." : ""))}</div>
          ${renderTranscriptMeta(entry)}
        </article>
      `
    )
    .join("");
}

// ── Helpers ─────────────────────────────────────────────────────────────────
function currentVector() {
  const components = state.agentState?.current_emotion?.components;
  return Array.isArray(components) ? components : [0, 0, 0, 0.5, 0, 0];
}

function topDimensions(vector, count) {
  return DIMENSIONS.map((dim) => ({
    ...dim,
    value: clamp(vector[dim.index] || 0, -1, 1),
  }))
    .sort((a, b) => Math.abs(b.value) - Math.abs(a.value))
    .slice(0, count);
}

function signedDescriptor(value, dimension) {
  const score = clamp(value || 0, -1, 1);
  if (Math.abs(score) < 0.12) {
    return { label: "Balanceado", detail: `${dimension.label} esta perto do centro.` };
  }
  return {
    label: score > 0 ? dimension.positive : dimension.negative,
    detail: `${dimension.label} ${score > 0 ? "pressiona para fora" : "puxa para dentro"} com ${score.toFixed(2)}.`,
  };
}

function buildToneChips(vector, dominant) {
  const chips = [];
  const fsmState = state.agentState?.current_fsm_state?.state_name || "neutral";
  chips.push(`FSM ${FSM_LABELS[fsmState] || "Neutral"}`);
  if (dominant?.label) chips.push(`${dominant.label} em destaque`);
  for (const dim of topDimensions(vector, 3)) {
    const descriptor = dim.value >= 0 ? dim.positive : dim.negative;
    chips.push(`${dim.label}: ${descriptor}`);
  }
  return chips;
}

function buildSparkline(history) {
  if (!history.length) {
    return `<div class="empty-state">Ainda nao ha pontos suficientes para desenhar o pulso.</div>`;
  }

  const width = 400;
  const height = 96;
  const values = history
    .slice(0, 12)
    .reverse()
    .map((entry) => normalizeIntensity(entry.intensity || 0) / 100);

  const points = values.map((value, index) => {
    const x = values.length === 1 ? width / 2 : (index / (values.length - 1)) * width;
    const y = height - value * (height - 14) - 7;
    return [x, y];
  });

  const d = points
    .map(([x, y], index) => `${index === 0 ? "M" : "L"} ${x.toFixed(1)} ${y.toFixed(1)}`)
    .join(" ");

  return `
    <svg viewBox="0 0 ${width} ${height}" role="img" aria-label="Sparkline de intensidade historica">
      <defs>
        <linearGradient id="sparklineStroke" x1="0%" x2="100%" y1="0%" y2="0%">
          <stop offset="0%" stop-color="#ffb55c" />
          <stop offset="100%" stop-color="#b28cff" />
        </linearGradient>
      </defs>
      <path d="${d}" fill="none" stroke="url(#sparklineStroke)" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"></path>
      ${points
        .map(
          ([x, y]) => `<circle cx="${x.toFixed(1)}" cy="${y.toFixed(1)}" fill="#eef2ff" r="3"></circle>`
        )
        .join("")}
    </svg>
  `;
}

function buildSmoothPath(points) {
  if (!points.length) return "";
  const closedPoints = [...points, points[0], points[1]];
  let path = `M ${points[0].x.toFixed(1)} ${points[0].y.toFixed(1)}`;
  for (let i = 1; i < closedPoints.length - 1; i += 1) {
    const current = closedPoints[i];
    const next = closedPoints[i + 1];
    const cx = ((current.x + next.x) / 2).toFixed(1);
    const cy = ((current.y + next.y) / 2).toFixed(1);
    path += ` Q ${current.x.toFixed(1)} ${current.y.toFixed(1)} ${cx} ${cy}`;
  }
  return `${path} Z`;
}

function mapInteractionsToTranscript(interactions) {
  if (!Array.isArray(interactions)) return [];
  return interactions.flatMap((interaction, index) => {
    const baseCreatedAt = Number(interaction.created_at_ms || 0);
    const fsmState = interaction.fsm_state?.state_name || "neutral";
    const intensity = normalizeIntensity(interaction.intensity || 0);
    const stimulus = interaction.stimulus || "novelty";
    return [
      {
        id: `persisted-user-${index}-${baseCreatedAt}`,
        role: "user",
        text: interaction.user_text || "",
        createdAt: baseCreatedAt,
      },
      {
        id: `persisted-assistant-${index}-${baseCreatedAt}`,
        role: "assistant",
        text: interaction.response_text || "",
        createdAt: baseCreatedAt + 1,
        meta: [
          `stimulus ${stimulus}`,
          `fsm ${FSM_LABELS[fsmState] || humanize(fsmState)}`,
          `intensidade ${Math.round(intensity)}%`,
        ],
      },
    ];
  });
}

function renderTranscriptMeta(entry) {
  if (!Array.isArray(entry.meta) || !entry.meta.length) return "";
  return `
    <div class="transcript-meta">
      ${entry.meta
        .map((item) => `<span class="transcript-badge">${escapeHTML(item)}</span>`)
        .join("")}
    </div>
  `;
}

function setBusy(nextBusy, message) {
  state.busy = nextBusy;
  ui.submitBtn.disabled = nextBusy;
  ui.refreshBtn.disabled = nextBusy;
  ui.streamStatus.textContent = message;
}

function normalizeIntensity(value) {
  return clamp((Number(value || 0) / MAX_INTENSITY) * 100, 0, 100);
}

function computeIntensity(components) {
  if (!Array.isArray(components) || !components.length) return 0;
  return Math.sqrt(components.reduce((sum, c) => sum + c * c, 0));
}

function macroFor(stateName) {
  return MACRO_BY_STATE[stateName] || "neutral";
}

function clamp(value, min, max) {
  return Math.min(Math.max(value, min), max);
}

function slugify(value) {
  return String(value || "")
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function humanize(value) {
  return String(value || "")
    .replace(/[-_]+/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

function formatTimestamp(timestampMs) {
  if (!timestampMs) return "--";
  try {
    return new Date(timestampMs).toLocaleString("pt-BR", {
      hour: "2-digit", minute: "2-digit", second: "2-digit",
      day: "2-digit", month: "2-digit",
    });
  } catch {
    return "--";
  }
}

function escapeHTML(value) {
  return String(value || "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function findTranscript(id) {
  return state.transcript.find((entry) => entry.id === id);
}

function updateTranscript(id, text) {
  const item = findTranscript(id);
  if (item) item.text = text;
}

let toastTimer = null;
function showToast(message, variant = "error") {
  ui.toast.textContent = message;
  ui.toast.classList.remove("hidden");
  ui.toast.style.borderColor = variant === "success" ? "rgba(99,225,165,0.22)" : "rgba(255,113,143,0.18)";
  ui.toast.style.background = variant === "success" ? "rgba(10,35,23,0.92)" : "rgba(34,11,25,0.9)";
  ui.toast.style.color = variant === "success" ? "#ceffe5" : "#ffd2dd";
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => ui.toast.classList.add("hidden"), 3200);
}
