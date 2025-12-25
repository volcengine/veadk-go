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

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	veagent "github.com/volcengine/veadk-go/agent/llmagent"
	"github.com/volcengine/veadk-go/tool/builtin_tools"
	"github.com/volcengine/veadk-go/tool/builtin_tools/web_search"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

func main() {
	ctx := context.Background()
	cfg := veagent.Config{
		Config: llmagent.Config{
			Name:        "data_analysis_agent",
			Description: "A data analysis for stock marketing",
			Instruction: `你是一个资深软件工程师，在沙箱里执行生产的代码, 可以使用 import subprocess
import sys
subprocess.check_call([sys.executable, "-m", "pip", "install", "akshare", "ipywidgets"])
import akshare as ak
realtime_df = ak.stock_zh_a_spot_em()
target_df = realtime_df[realtime_df['代码'] == stock_code]
print(target_df) 下载相关的股票数据，#只需在上述代码中增加真实stock_code赋值，禁止修改其他代码#，超时时间设为5分钟。可以通过web_search工具搜索相关公司的经营数据。`,
		},
		ModelExtraConfig: map[string]any{
			"extra_body": map[string]any{
				"thinking": map[string]string{
					"type": "disabled",
				},
			},
		},
		ModelName: "deepseek-v3-2-251201",
	}

	webSearch, err := web_search.NewWebSearchTool(&web_search.Config{})
	if err != nil {
		fmt.Printf("NewWebSearchTool failed: %v", err)
		return
	}

	runCode, err := builtin_tools.NewRunCodeSandboxTool()
	if err != nil {
		fmt.Printf("NewRunCodeSandboxTool failed: %v", err)
		return
	}

	cfg.Tools = []tool.Tool{webSearch, runCode}

	a, err := veagent.New(&cfg)
	if err != nil {
		fmt.Printf("NewLLMAgent failed: %v", err)
		return
	}

	config := &launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
	}

	l := full.NewLauncher()
	if err = l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("Run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}
