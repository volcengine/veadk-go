// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runner

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"strings"

	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/integrations/agentkit"
	vememory "github.com/volcengine/veadk-go/v2/memory"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/memory"
	adkrunner "google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

const defaultAppName = "veadk_default_app"

type Config struct {
	AppName string
	Agent   agent.Agent

	SessionService  session.Service
	ShortTermMemory session.Service

	ArtifactService artifact.Service

	MemoryService  memory.Service
	LongTermMemory memory.Service

	PluginConfig adkrunner.PluginConfig

	AutoCreateSession bool
	// SkillsMode is required for generic ADK agents when AIOSandboxLifecycle is
	// configured. VeADK LlmAgents carry their constructed mode and Skill IDs.
	SkillsMode string
	// AIOSandboxLifecycle is optional. Local-mode runners leave it nil.
	// Its background worker is started by the embedding application.
	AIOSandboxLifecycle  *agentkit.AIOLifecycle
	AutoSaveSession      bool
	AutoSaveMemoryPolicy *vememory.MemoryAutoSavePolicy
}

type Runner struct {
	*adkrunner.Runner

	appName        string
	agent          agent.Agent
	sessionService session.Service
	memoryService  memory.Service
	aioLifecycle   *agentkit.AIOLifecycle
}

func New(cfg Config) (*Runner, error) {
	normalized, err := NormalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	adkRunner, err := adkrunner.New(adkrunner.Config{
		AppName:           normalized.AppName,
		Agent:             normalized.Agent,
		SessionService:    normalized.SessionService,
		ArtifactService:   normalized.ArtifactService,
		MemoryService:     normalized.MemoryService,
		PluginConfig:      normalized.PluginConfig,
		AutoCreateSession: normalized.AutoCreateSession,
	})
	if err != nil {
		return nil, err
	}
	return &Runner{
		Runner:         adkRunner,
		appName:        normalized.AppName,
		agent:          normalized.Agent,
		sessionService: normalized.SessionService,
		memoryService:  normalized.MemoryService,
		aioLifecycle:   normalized.AIOSandboxLifecycle,
	}, nil
}

func NormalizeConfig(cfg Config) (Config, error) {
	if cfg.Agent == nil {
		return cfg, fmt.Errorf("root agent is required")
	}
	if cfg.AIOSandboxLifecycle != nil {
		mode, ids, known := veagent.SkillsModeFor(cfg.Agent)
		if cfg.SkillsMode != "" && cfg.SkillsMode != "aio_sandbox" || known && (mode != "aio_sandbox" || !slices.Equal(ids, cfg.AIOSandboxLifecycle.SkillSpaceIDs())) || !known && cfg.SkillsMode != "aio_sandbox" {
			return cfg, fmt.Errorf("AIO sandbox lifecycle does not match Agent Skill mode or Skill Space IDs")
		}
		if cfg.AutoCreateSession {
			return cfg, fmt.Errorf("AIO sandbox lifecycle requires explicit ADK session creation")
		}
	}
	if cfg.AppName == "" {
		cfg.AppName = defaultAppName
	}
	if cfg.SessionService == nil {
		cfg.SessionService = cfg.ShortTermMemory
	}
	if cfg.SessionService == nil {
		cfg.SessionService = session.InMemoryService()
	}
	if cfg.MemoryService == nil {
		cfg.MemoryService = cfg.LongTermMemory
	}
	if cfg.MemoryService == nil {
		cfg.MemoryService, _ = veagent.LongTermMemoryFor(cfg.Agent)
	}
	if cfg.AutoSaveSession {
		if cfg.MemoryService == nil {
			return cfg, fmt.Errorf("auto save session requires long-term memory or memory service")
		}
		plugin, err := NewAutoSaveMemoryPlugin(cfg.MemoryService, cfg.AutoSaveMemoryPolicy)
		if err != nil {
			return cfg, err
		}
		cfg.PluginConfig.Plugins = appendPluginIfMissing(cfg.PluginConfig.Plugins, plugin)
	}
	return cfg, nil
}

func (r *Runner) SessionService() session.Service {
	return r.sessionService
}

func (r *Runner) MemoryService() memory.Service {
	return r.memoryService
}

func (r *Runner) AppName() string {
	return r.appName
}

func (r *Runner) SaveSessionToLongTermMemory(ctx context.Context, userID, sessionID string, policy *vememory.MemoryAutoSavePolicy) error {
	return SaveSessionToLongTermMemory(ctx, r.sessionService, r.memoryService, r.appName, userID, sessionID, policy)
}

func SaveSessionToLongTermMemory(ctx context.Context, sessionService session.Service, memoryService memory.Service, appName, userID, sessionID string, policy *vememory.MemoryAutoSavePolicy) error {
	if sessionService == nil {
		return fmt.Errorf("session service is required")
	}
	if memoryService == nil {
		return fmt.Errorf("long-term memory is not enabled")
	}
	resp, err := sessionService.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("get session %q failed: %w", sessionID, err)
	}
	return addSessionToMemory(ctx, memoryService, resp.Session, policy)
}

func (r *Runner) Run(ctx context.Context, userID, sessionID string, msg *genai.Content, cfg agent.RunConfig, opts ...adkrunner.RunOption) iter.Seq2[*session.Event, error] {
	if r.aioLifecycle != nil {
		return func(yield func(*session.Event, error) bool) {
			err := r.aioLifecycle.WithSessionContext(ctx, r.appName, userID, sessionID, func(ctx context.Context) error {
				for event, runErr := range r.Runner.Run(ctx, userID, sessionID, msg, cfg, opts...) {
					if !yield(event, runErr) || runErr != nil {
						break
					}
				}
				return nil
			})
			if err != nil {
				yield(nil, err)
			}
		}
	}
	return r.Runner.Run(ctx, userID, sessionID, msg, cfg, opts...)
}

// RunText sends a user text message and returns the final visible model text.
func (r *Runner) RunText(ctx context.Context, userID, sessionID, text string, cfg agent.RunConfig, opts ...adkrunner.RunOption) (string, error) {
	return CollectFinalText(r.Run(ctx, userID, sessionID, UserText(text), cfg, opts...))
}

// CollectFinalText consumes a runner event stream and returns the final visible model text.
func CollectFinalText(events iter.Seq2[*session.Event, error]) (string, error) {
	var finalText string
	var partialText strings.Builder
	var hasFinalText bool

	for event, err := range events {
		if err != nil {
			return "", err
		}
		text := eventText(event)
		if text == "" {
			continue
		}
		if event.Partial {
			partialText.WriteString(text)
			continue
		}
		finalText = text
		hasFinalText = true
	}
	if hasFinalText {
		return finalText, nil
	}
	return partialText.String(), nil
}

func eventText(event *session.Event) string {
	if event == nil || event.Content == nil {
		return ""
	}
	var text strings.Builder
	for _, part := range event.Content.Parts {
		if part == nil || part.Thought || strings.TrimSpace(part.Text) == "" {
			continue
		}
		text.WriteString(part.Text)
	}
	return text.String()
}
