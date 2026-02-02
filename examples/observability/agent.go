package main

import (
	"context"
	"fmt"
	"os"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/observability"
	"github.com/volcengine/veadk-go/tool/builtin_tools/web_search"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

func main() {
	ctx := context.Background()

	// Important: Always call Shutdown to flush spans and metrics
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
	}

	l := full.NewLauncher()
	if err = l.Execute(ctx, config, os.Args[1:]); err != nil {
		fmt.Printf("Run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
