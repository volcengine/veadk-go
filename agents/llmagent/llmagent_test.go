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
	"log"
	"os"
	"testing"

	"github.com/volcengine/veadk-go/common"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
)

func TestNewLLMAgent(t *testing.T) {
	ctx := context.Background()
	fmt.Println(os.Getenv("MODEL_API_KEY"))
	cfg := Config{
		ModelName:    common.DEFAULT_MODEL_AGENT_NAME,
		ModelApiBase: common.DEFAULT_MODEL_AGENT_API_BASE,
		ModelApiKey:  os.Getenv("MODEL_API_KEY"),
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("NewLLMAgent failed: %v", err)
	}

	config := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(a),
	}

	l := full.NewLauncher()
	if err = l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("Run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}

	//fmt.Println(a.Name())
	//fmt.Println(a.Description())
	//time.Sleep(1 * time.Second)
}
