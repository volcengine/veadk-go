// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0

package agentkit_server_app

import (
	"context"
	"github.com/gorilla/mux"
	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/apps"
	"github.com/volcengine/veadk-go/v2/apps/simple_app"
	"github.com/volcengine/veadk-go/v2/integrations/agentkit"
	"google.golang.org/adk/v2/agent"
	adkllm "google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"iter"
	"testing"
)

type configOnlyModel struct{}

func (configOnlyModel) Name() string { return "test" }
func (configOnlyModel) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(func(*model.LLMResponse, error) bool) {}
}
func TestAIORejectsSimpleAPIBeforeCreatingSessions(t *testing.T) {
	root, err := veagent.New(&veagent.Config{Config: adkllm.Config{Name: "aio", Model: configOnlyModel{}}, SkillsMode: "aio_sandbox"})
	if err != nil {
		t.Fatal(err)
	}
	store := session.InMemoryService()
	manager, err := agentkit.NewAIOLifecycle(agentkit.AIOLifecycleConfig{SessionService: store})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &apps.RunConfig{AgentLoader: agent.NewSingleLoader(root), SessionService: store}
	for _, app := range []apps.BasicApp{NewAgentkitServerApp(apps.DefaultApiConfig().SetSimpleAPIEnabled(true), WithAIOLifecycle(manager)), simple_app.NewAgentkitSimpleApp(apps.DefaultApiConfig())} {
		if err := app.SetupRouters(mux.NewRouter(), cfg); err == nil {
			t.Fatal("AIO accepted Simple API")
		}
	}
	listed, err := store.List(context.Background(), &session.ListRequest{AppName: "agentkit_simple_app"})
	if err != nil || len(listed.Sessions) != 0 {
		t.Fatal("configuration failure created a session")
	}
}
