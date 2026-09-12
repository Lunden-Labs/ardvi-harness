package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

type nativeAgents struct {
	Schema int           `json:"schema"`
	Agents []nativeAgent `json:"agents"`
}

type nativeAgent struct {
	Client        string `json:"client,omitempty"`
	Model         string `json:"model"`
	ModelProvider string `json:"model_provider,omitempty"`
	ModelVariant  string `json:"model_variant,omitempty"`
	AgentKey      string `json:"agent_key"`
	SessionName   string `json:"session_name,omitempty"`
}

type nativeBinding struct {
	AgentKey      string `json:"agent_key"`
	SessionName   string `json:"session_name,omitempty"`
	Model         string `json:"model"`
	ModelProvider string `json:"model_provider"`
	ModelVariant  string `json:"model_variant,omitempty"`
	NativeCwd     string `json:"native_cwd,omitempty"`
}

var nativeKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

func loadNativeAgents() (nativeAgents, error) {
	dir, err := ardviConfigDir("")
	if err != nil {
		return nativeAgents{}, err
	}
	path := filepath.Join(dir, "agents.json")
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nativeAgents{Schema: 1}, nil
	}
	if err != nil {
		return nativeAgents{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64<<10))
	if err != nil {
		return nativeAgents{}, err
	}
	if len(data) == 64<<10 {
		return nativeAgents{}, errors.New("agents.json exceeds 64KiB")
	}
	var cfg nativeAgents
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return nativeAgents{}, fmt.Errorf("invalid %s: %w", path, err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nativeAgents{}, errors.New("agents.json contains trailing data")
	}
	if cfg.Schema != 1 {
		return nativeAgents{}, errors.New("agents.json schema must be 1")
	}
	seenBinding, seenKey := map[string]bool{}, map[string]bool{}
	for index := range cfg.Agents {
		agent := &cfg.Agents[index]
		if agent.Client == "" {
			agent.Client = "codex"
		}
		binding := agent.Client + "\x00" + agent.Model + "\x00" + agent.ModelProvider
		key := agent.Client + "\x00" + agent.AgentKey
		if (agent.Client != "codex" && agent.Client != "opencode") || agent.Model == "" || agent.ModelProvider == "" || agent.AgentKey == "" || !nativeKey.MatchString(agent.AgentKey) || seenBinding[binding] || seenKey[key] {
			return nativeAgents{}, errors.New("agents.json requires unique non-empty client/model/provider and client/agent_key values")
		}
		seenBinding[binding], seenKey[key] = true, true
	}
	return cfg, nil
}

func (cfg nativeAgents) byClientKey(client, key string) (nativeAgent, bool) {
	for _, agent := range cfg.Agents {
		if agent.Client == client && agent.AgentKey == key {
			return agent, true
		}
	}
	return nativeAgent{}, false
}
