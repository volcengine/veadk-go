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

package llmagent

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/volcengine/veadk-go/v2/auth/veauth"
	"github.com/volcengine/veadk-go/v2/common"
	"github.com/volcengine/veadk-go/v2/configs"
	agentkit "github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"github.com/volcengine/veadk-go/v2/knowledgebase"
	"github.com/volcengine/veadk-go/v2/model"
	"github.com/volcengine/veadk-go/v2/prompts"
	"github.com/volcengine/veadk-go/v2/skills"
	"github.com/volcengine/veadk-go/v2/tool/builtin_tools"
	"github.com/volcengine/veadk-go/v2/tool/skilltool"
	"github.com/volcengine/veadk-go/v2/utils"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/memory"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/loadmemorytool"
)

var longTermMemoryByAgent sync.Map
var skillModeByAgent sync.Map

type skillModeSelection struct {
	mode string
	ids  []string
}

// SkillsModeFor reports the mode selected when a VeADK LlmAgent was built.
// Generic ADK agents have no registered mode.
func SkillsModeFor(a agent.Agent) (string, []string, bool) {
	value, ok := skillModeByAgent.Load(a)
	if !ok {
		return "", nil, false
	}
	selection := value.(skillModeSelection)
	return selection.mode, append([]string(nil), selection.ids...), true
}

var aioSkillSpaceID = regexp.MustCompile(`^ss-[A-Za-z0-9][A-Za-z0-9_-]*$`)

type Config struct {
	llmagent.Config
	ModelName           string
	ModelProvider       string
	ModelAPIBase        string
	ModelAPIKey         string
	ModelAPIKeyProvider model.APIKeyProvider
	ModelExtraConfig    map[string]any
	ModelExtraHeaders   map[string]string
	ModelHTTPClient     *http.Client
	KnowledgeBase       *knowledgebase.KnowledgeBase
	// LongTermMemory enables automatic wiring of the long-term memory search tool.
	// The same memory service still needs to be passed to the runner/app RunConfig.
	LongTermMemory memory.Service
	PromptManager  prompts.BasePromptManager
	DisableThought bool
	// Skills contains local root directories by default. In aio_sandbox mode,
	// it contains Skill Space IDs discovered when the Agent is constructed.
	Skills                []string
	SkillDiscoveryOptions skills.DiscoveryOptions
	// SkillsMode selects local (the default) or session-bound aio_sandbox Skills.
	// AIO sandboxes are created lazily on bash/file tool use or file upload;
	// constructing an Agent, listing Skills, and ordinary chat do not start one.
	SkillsMode string
	// SkillSpaceSourceConfig optionally overrides the Skill service endpoint,
	// region or credentials. SpaceID is taken from each entry in Skills.
	SkillSpaceSourceConfig skills.RemoteSourceConfig
}

func New(cfg *Config) (agent.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("llmagent config is required")
	}
	var aioInstruction string
	switch cfg.SkillsMode {
	case "", "local":
		if err := configureLocalSkills(cfg); err != nil {
			return nil, err
		}
	case "aio_sandbox":
		metadata, ids, err := discoverAIOSkills(cfg)
		if err != nil {
			return nil, err
		}
		toolset, err := skilltool.NewAIOSandboxToolset(metadata, ids...)
		if err != nil {
			return nil, err
		}
		aioInstruction = toolset.Instruction()
		cfg.Toolsets = append(cfg.Toolsets, toolset)
	default:
		return nil, fmt.Errorf("unsupported SkillsMode %q", cfg.SkillsMode)
	}

	if cfg.Name == "" {
		cfg.Name = common.DEFAULT_LLMAGENT_NAME
	}

	if cfg.Instruction == "" {
		if cfg.PromptManager != nil {
			cfg.Instruction = cfg.PromptManager.GetPrompt()
		} else {
			cfg.Instruction = prompts.DEFAULT_INSTRUCTION
		}
	}
	if aioInstruction != "" {
		cfg.Instruction += "\n\n" + aioInstruction
	}

	if cfg.Description == "" {
		cfg.Description = prompts.DEFAULT_DESCRIPTION
	}

	if cfg.DisableThought {
		newModelExtraConfig, err := addDisableThoughtConfig(cfg.ModelExtraConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to set DisableThought config: %w", err)
		}
		cfg.ModelExtraConfig = newModelExtraConfig
	}

	if cfg.Model == nil {
		if cfg.ModelName == "" {
			cfg.ModelName = utils.GetEnvWithDefault(common.MODEL_AGENT_NAME, configs.GetGlobalConfig().Model.Agent.Name, common.DEFAULT_MODEL_AGENT_NAME)
		}
		if cfg.ModelProvider == "" {
			cfg.ModelProvider = utils.GetEnvWithDefault(common.MODEL_AGENT_PROVIDER, configs.GetGlobalConfig().Model.Agent.Provider, common.DEFAULT_MODEL_AGENT_PROVIDER)
		}
		if cfg.ModelAPIKey == "" && cfg.ModelAPIKeyProvider == nil {
			cfg.ModelAPIKey = utils.GetEnvWithDefault(common.MODEL_AGENT_API_KEY, configs.GetGlobalConfig().Model.Agent.ApiKey)
		}
		if cfg.ModelAPIKey == "" && cfg.ModelAPIKeyProvider == nil {
			cfg.ModelAPIKeyProvider = veauth.NewArkTokenProvider(veauth.ArkTokenProviderConfig{
				Region: common.DEFAULT_MODEL_REGION,
			})
		}
		if cfg.ModelAPIBase == "" {
			cfg.ModelAPIBase = utils.GetEnvWithDefault(common.MODEL_AGENT_API_BASE, configs.GetGlobalConfig().Model.Agent.ApiBase, common.DEFAULT_MODEL_AGENT_API_BASE)
		}

		var veModel adkmodel.LLM
		var err error

		switch cfg.ModelProvider {
		case "ark":
			veModel, err = model.NewArkModel(
				context.Background(),
				cfg.ModelName,
				&model.ArkClientConfig{
					APIKey:         cfg.ModelAPIKey,
					APIKeyProvider: cfg.ModelAPIKeyProvider,
					BaseURL:        cfg.ModelAPIBase,
					ExtraBody:      cfg.ModelExtraConfig,
					ExtraHeaders:   cfg.ModelExtraHeaders,
					HTTPClient:     cfg.ModelHTTPClient,
				})
		default: // "openai"
			veModel, err = model.NewOpenAIModel(
				context.Background(),
				cfg.ModelName,
				&model.ClientConfig{
					APIKey:         cfg.ModelAPIKey,
					APIKeyProvider: cfg.ModelAPIKeyProvider,
					BaseURL:        cfg.ModelAPIBase,
					ExtraBody:      cfg.ModelExtraConfig,
					ExtraHeaders:   cfg.ModelExtraHeaders,
					HTTPClient:     cfg.ModelHTTPClient,
				})
		}
		if err != nil {
			return nil, err
		}
		cfg.Model = veModel
	}

	if cfg.KnowledgeBase != nil {
		knowledgeTool, err := builtin_tools.LoadKnowledgeBaseTool(cfg.KnowledgeBase)
		if err != nil {
			return nil, err
		}
		appendTool(cfg, knowledgeTool)
	}

	if cfg.LongTermMemory != nil {
		appendToolIfMissing(cfg, loadmemorytool.New())
	}

	a, err := llmagent.New(cfg.Config)
	if err != nil {
		return nil, err
	}
	if cfg.LongTermMemory != nil {
		longTermMemoryByAgent.Store(a, cfg.LongTermMemory)
	}
	mode := cfg.SkillsMode
	if mode == "" {
		mode = "local"
	}
	selection := skillModeSelection{mode: mode}
	if mode == "aio_sandbox" {
		selection.ids = append([]string(nil), cfg.Skills...)
	}
	skillModeByAgent.Store(a, selection)
	return a, nil
}

func discoverAIOSkills(cfg *Config) ([]skills.RemoteSkill, []string, error) {
	ids := append([]string(nil), cfg.Skills...)
	metadata := make([]skills.RemoteSkill, 0)
	seenIDs := map[string]bool{}
	seenNames := map[string]bool{}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, id := range ids {
		if !aioSkillSpaceID.MatchString(id) || seenIDs[id] {
			return nil, nil, fmt.Errorf("aio_sandbox Skills must contain unique AgentKit Skill Space IDs; invalid value %q", id)
		}
		seenIDs[id] = true
		options := cfg.SkillSpaceSourceConfig
		options.SpaceID = id
		service, region, host, scheme := agentkit.EndpointConfig()
		if options.Endpoint == "" {
			options.Endpoint = scheme + "://" + host
		}
		if options.Region == "" {
			options.Region = region
		}
		if options.Service == "" {
			options.Service = service
		}
		source, err := skills.NewRemoteSource(options)
		if err != nil {
			return nil, nil, fmt.Errorf("configure Skill Space %q: %w", id, err)
		}
		found, err := source.Discover(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("discover Skill Space %q: %w", id, err)
		}
		for _, sk := range found {
			if seenNames[sk.Name] {
				return nil, nil, fmt.Errorf("duplicate Skill name %q across Skill Spaces", sk.Name)
			}
			seenNames[sk.Name] = true
			metadata = append(metadata, sk)
		}
	}
	return metadata, ids, nil
}

func LongTermMemoryFor(a agent.Agent) (memory.Service, bool) {
	if a == nil {
		return nil, false
	}
	value, ok := longTermMemoryByAgent.Load(a)
	if !ok {
		return nil, false
	}
	memoryService, ok := value.(memory.Service)
	return memoryService, ok
}

func appendTool(cfg *Config, t tool.Tool) {
	if cfg.Tools == nil {
		cfg.Tools = []tool.Tool{}
	}
	cfg.Tools = append(cfg.Tools, t)
}

func appendToolIfMissing(cfg *Config, t tool.Tool) {
	for _, existing := range cfg.Tools {
		if existing.Name() == t.Name() {
			return
		}
	}
	appendTool(cfg, t)
}

func addDisableThoughtConfig(extConfig map[string]any) (map[string]any, error) {
	if extConfig == nil {
		extConfig = map[string]any{
			"extra_body": map[string]any{
				"thinking": map[string]string{
					"type": "disabled",
				},
			},
		}
		return extConfig, nil
	}

	extraBodyVal, exists := extConfig["extra_body"]
	var extraBody map[string]any
	if !exists {
		extConfig["extra_body"] = map[string]any{
			"thinking": map[string]string{
				"type": "disabled",
			},
		}
		return extConfig, nil
	} else {
		var ok bool
		extraBody, ok = extraBodyVal.(map[string]any)
		if !ok {
			return extConfig, fmt.Errorf("type conflict for field 'extra_body' in ModelExtraConfig: expected type map[string]any, but got %T", extraBodyVal)
		}
	}

	thinkingVal, exists := extraBody["thinking"]
	var thinking map[string]string
	if !exists {
		extraBody["thinking"] = map[string]string{
			"type": "disabled",
		}
	} else {
		var ok bool
		thinking, ok = thinkingVal.(map[string]string)
		if !ok {
			return extConfig, fmt.Errorf("type conflict for field 'thinking' in ModelExtraConfig: expected type map[string]any, but got %T", thinkingVal)
		}
	}

	thinking["type"] = "disabled"
	return extConfig, nil
}
