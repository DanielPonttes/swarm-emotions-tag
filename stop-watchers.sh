#!/usr/bin/env bash
tmux kill-session -t swarm-go 2>/dev/null || true
tmux kill-session -t swarm-rust 2>/dev/null || true
tmux kill-session -t swarm-py 2>/dev/null || true
tmux kill-session -t swarm-orch 2>/dev/null || true
tmux kill-session -t swarm-logs 2>/dev/null || true

