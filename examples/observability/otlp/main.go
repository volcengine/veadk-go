// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

// A self-contained Agent server for testing an OTLP collector without model keys.
package main

import (
	"context"
	"flag"
	"fmt"
	"iter"
	"os"

	"github.com/volcengine/veadk-go/apps"
	"github.com/volcengine/veadk-go/apps/simple_app"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

type fixtureModel struct{}

func (fixtureModel) Name() string { return "otlp-fixture" }
func (fixtureModel) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{Content: genai.NewContentFromText("otlp-agent-ok", "model"), TurnComplete: true}, nil)
	}
}
func main() {
	port := flag.Int("port", 8000, "local HTTP port")
	flag.Parse()
	a, err := llmagent.New(llmagent.Config{Name: "otlp_agent", Model: fixtureModel{}})
	if err == nil {
		cfg := apps.DefaultApiConfig().SetHost("127.0.0.1").SetPort(*port)
		err = apps.Run(context.Background(), &apps.RunConfig{AgentLoader: agent.NewSingleLoader(a)}, simple_app.NewAgentkitSimpleApp(cfg))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "OTLP example failed")
		os.Exit(1)
	}
}
