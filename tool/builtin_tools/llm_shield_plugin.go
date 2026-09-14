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

package builtin_tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// LLMShieldFailurePolicy controls service failures, not moderation decisions.
type LLMShieldFailurePolicy string

const (
	// LLMShieldFailOpen matches Python: an unavailable service allows execution.
	LLMShieldFailOpen  LLMShieldFailurePolicy = ""
	LLMShieldFailClose LLMShieldFailurePolicy = "fail_close"
)

type LLMShieldCallback string

const (
	LLMShieldBeforeModel LLMShieldCallback = "before_model"
	LLMShieldAfterModel  LLMShieldCallback = "after_model"
	LLMShieldBeforeTool  LLMShieldCallback = "before_tool"
	LLMShieldAfterTool   LLMShieldCallback = "after_tool"
)

// LLMShieldConfig is explicit configuration; it does not read environment or
// config.yaml. A nil Callbacks slice selects only before-model, as in the Python
// Sandbox. Other scopes must be listed explicitly. An empty non-nil slice
// installs no callbacks. The default failure policy is Python's fail-open.
type LLMShieldConfig struct {
	Enabled          bool
	AppID            string
	APIKey           string
	Region           string
	URL              string
	Timeout          time.Duration
	HTTPClient       *http.Client
	CredentialSource veauth.CredentialSource
	Callbacks        []LLMShieldCallback
	FailurePolicy    LLMShieldFailurePolicy
}

// NewLLMShieldPlugin returns nil without validation or initialization if disabled.
// Add only a non-nil result to runner.PluginConfig.Plugins.
func NewLLMShieldPlugin(cfg LLMShieldConfig) (*plugin.Plugin, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	c, err := NewLLMShieldClientWithConfig(cfg)
	if err != nil {
		return nil, err
	}
	callbacks := cfg.Callbacks
	if callbacks == nil {
		callbacks = []LLMShieldCallback{LLMShieldBeforeModel}
	}
	pc := plugin.Config{Name: "llm_shield"}
	for _, callback := range callbacks {
		switch callback {
		case LLMShieldBeforeModel:
			pc.BeforeModelCallback = c.beforeModelCallBack
		case LLMShieldAfterModel:
			pc.AfterModelCallback = c.afterModelCallBack
		case LLMShieldBeforeTool:
			pc.BeforeToolCallback = c.beforeToolCallback
		case LLMShieldAfterTool:
			pc.AfterToolCallback = c.afterToolCallback
		default:
			return nil, errors.New("LLM Shield invalid callback scope")
		}
	}
	return plugin.New(pc)
}

// LLMShieldPluginsFromEnv is opt-in Sandbox assembly. ENABLE_LLM_SHIELD accepts
// the same values as Python: 1, true, yes, on (case/whitespace insensitive).
// Disabled returns an empty slice without accessing config or credentials.
func LLMShieldPluginsFromEnv() ([]*plugin.Plugin, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ENABLE_LLM_SHIELD"))) {
	case "1", "true", "yes", "on":
	default:
		return nil, nil
	}
	// Python's Sandbox validates the environment value before loading the plugin.
	if strings.TrimSpace(os.Getenv(common.TOOL_LLM_SHIELD_APP_ID)) == "" {
		return nil, ErrInvalidAppID
	}
	cfg := shieldEnvironmentConfig()
	cfg.Enabled = true
	p, err := NewLLMShieldPlugin(cfg)
	if err != nil {
		return nil, err
	}
	return []*plugin.Plugin{p}, nil
}

func shieldEnvironmentConfig() LLMShieldConfig {
	cfg := configs.GetGlobalConfig().Tool.LLMShield
	return LLMShieldConfig{
		AppID:  utils.GetEnvWithDefault(common.TOOL_LLM_SHIELD_APP_ID, cfg.AppId),
		APIKey: utils.GetEnvWithDefault(common.TOOL_LLM_SHIELD_API_KEY, cfg.ApiKey),
		URL:    utils.GetEnvWithDefault(common.TOOL_LLM_SHIELD_URL, cfg.Url),
		Region: utils.GetEnvWithDefault(common.TOOL_LLM_SHIELD_REGION, os.Getenv("REGION"), cfg.Region, "cn-beijing"),
	}
}

// NewLLMShieldPlugins retains the original four-callback behavior for existing
// callers. New integrations should use NewLLMShieldPlugin or LLMShieldPluginsFromEnv.
func NewLLMShieldPlugins() (*plugin.Plugin, error) {
	cfg := shieldEnvironmentConfig()
	cfg.Enabled = true
	cfg.Callbacks = []LLMShieldCallback{LLMShieldBeforeModel, LLMShieldAfterModel, LLMShieldBeforeTool, LLMShieldAfterTool}
	return NewLLMShieldPlugin(cfg)
}

func (p *LLMShieldClient) check(ctx context.Context, message, role string) (string, error) {
	block, err := p.Moderate(ctx, message, role)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil && p.failurePolicy == LLMShieldFailOpen {
		return "", nil
	}
	return block, err
}

func shieldBlockResponse(message string) *model.LLMResponse {
	if message == "" {
		return nil
	}
	// Go ADK needs a final response here to finish a blocked A2A task.
	return &model.LLMResponse{Content: genai.NewContentFromText(message, "model"), FinishReason: genai.FinishReasonStop}
}

func (p *LLMShieldClient) beforeModelCallBack(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
	if req == nil || len(req.Contents) == 0 {
		return nil, nil
	}
	last := req.Contents[len(req.Contents)-1]
	message := shieldFirstText(last, "user")
	if message == "" {
		return nil, nil
	}
	block, err := p.check(ctx, message, "user")
	return shieldBlockResponse(block), err
}

func (p *LLMShieldClient) afterModelCallBack(ctx agent.CallbackContext, resp *model.LLMResponse, responseErr error) (*model.LLMResponse, error) {
	if responseErr != nil {
		return nil, responseErr
	}
	if resp == nil {
		return nil, nil
	}
	message := shieldFirstText(resp.Content, "model")
	if message == "" {
		return nil, nil
	}
	block, err := p.check(ctx, message, "assistant")
	return shieldBlockResponse(block), err
}

// Match Python's first-text-part semantics. This is not a full transcript scan.
func shieldFirstText(content *genai.Content, role string) string {
	if content == nil || content.Role != role || len(content.Parts) == 0 || content.Parts[0] == nil {
		return ""
	}
	return content.Parts[0].Text
}

func (p *LLMShieldClient) beforeToolCallback(ctx tool.Context, _ tool.Tool, args map[string]any) (map[string]any, error) {
	message, err := shieldToolMessage(args, true)
	if err != nil {
		return nil, p.toolEncodingError()
	}
	block, err := p.check(ctx, message, "user")
	if block != "" {
		return map[string]any{"result": block}, err
	}
	return nil, err
}

func (p *LLMShieldClient) afterToolCallback(ctx tool.Context, _ tool.Tool, _, result map[string]any, toolErr error) (map[string]any, error) {
	if toolErr != nil {
		return result, toolErr
	}
	message, err := shieldToolMessage(result, false)
	if err != nil {
		return nil, p.toolEncodingError()
	}
	block, err := p.check(ctx, message, "assistant")
	if block != "" {
		return map[string]any{"result": block}, err
	}
	return nil, err
}

func (p *LLMShieldClient) toolEncodingError() error {
	if p.failurePolicy == LLMShieldFailOpen {
		return nil
	}
	return ErrShieldUnavailable
}

// Go maps do not preserve insertion order. Sort keys for repeatable moderation;
// use JSON for structured values instead of Go's map[...] representation.
func shieldToolMessage(values map[string]any, includeKeys bool) (string, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var lines []string
	for _, key := range keys {
		value, ok := values[key].(string)
		if !ok {
			data, err := json.Marshal(values[key])
			if err != nil {
				return "", ErrShieldUnavailable
			}
			value = string(data)
		}
		if includeKeys {
			value = key + ": " + value
		}
		lines = append(lines, value)
	}
	message := strings.Join(lines, "\n")
	if !includeKeys && len(lines) > 0 {
		message += "\n"
	}
	return message, nil
}
