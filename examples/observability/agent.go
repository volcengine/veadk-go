package main

import (
	"context"
	"fmt"
	"os"

	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/common"
	"github.com/volcengine/veadk-go/v2/observability"
	"github.com/volcengine/veadk-go/v2/tool/builtin_tools/web_search"
	"github.com/volcengine/veadk-go/v2/utils"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
)

func main() {
	ctx := context.Background()
	// Shutdown to flush spans and metrics
	defer observability.Shutdown(ctx)

	cfg := &veagent.Config{
		ModelName:    common.DEFAULT_MODEL_AGENT_NAME,
		ModelAPIBase: common.DEFAULT_MODEL_AGENT_API_BASE,
		ModelAPIKey:  utils.GetEnvWithDefault(common.MODEL_AGENT_API_KEY),
	}

	webSearch, err := web_search.NewWebSearchTool(&web_search.Config{})
	if err != nil {
		fmt.Printf("NewLLMAgent failed: %v", err)
		return
	}

	cfg.Tools = []tool.Tool{webSearch}

	a, err := veagent.New(cfg)
	if err != nil {
		fmt.Printf("NewLLMAgent failed: %v", err)
		return
	}

	config := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
		PluginConfig: runner.PluginConfig{
			Plugins: []*plugin.Plugin{observability.NewPlugin()},
		},
		TelemetryOptions: observability.ADKTelemetryOptions(),
	}

	l := full.NewLauncher()
	if err = l.Execute(ctx, config, os.Args[1:]); err != nil {
		fmt.Printf("Run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
