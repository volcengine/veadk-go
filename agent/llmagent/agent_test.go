// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0

package llmagent

import (
	"context"
	"iter"
	"sync/atomic"
	"testing"

	"github.com/volcengine/veadk-go/v2/knowledgebase"
	_interface "github.com/volcengine/veadk-go/v2/knowledgebase/interface"
	"github.com/volcengine/veadk-go/v2/knowledgebase/ktypes"
	"github.com/volcengine/veadk-go/v2/tool/builtin_tools"
	adkllmagent "google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/memory"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/loadmemorytool"
)

type testAPIKeyProvider struct {
	calls atomic.Int32
}

func (p *testAPIKeyProvider) APIKey(context.Context) (string, error) {
	p.calls.Add(1)
	return "test-key", nil
}

type testLLM struct{}

func (testLLM) Name() string { return "test-model" }

func (testLLM) GenerateContent(context.Context, *adkmodel.LLMRequest, bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {}
}

type testKnowledgeBackend struct{}

func (testKnowledgeBackend) Index() string { return "test-index" }

func (testKnowledgeBackend) Search(query string, opts ...map[string]any) ([]ktypes.KnowledgeEntry, error) {
	return []ktypes.KnowledgeEntry{{Content: query}}, nil
}

func (testKnowledgeBackend) AddFromText([]string, ...map[string]any) error    { return nil }
func (testKnowledgeBackend) AddFromFiles([]string, ...map[string]any) error   { return nil }
func (testKnowledgeBackend) AddFromDirectory(string, ...map[string]any) error { return nil }

func TestNewDoesNotResolveLazyAPIKey(t *testing.T) {
	for _, modelProvider := range []string{"openai", "ark"} {
		t.Run(modelProvider, func(t *testing.T) {
			provider := &testAPIKeyProvider{}
			_, err := New(&Config{
				Config:              adkllmagent.Config{Name: "lazy-agent-" + modelProvider},
				ModelName:           "test-model",
				ModelProvider:       modelProvider,
				ModelAPIBase:        "http://model.invalid/v1",
				ModelAPIKeyProvider: provider,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if got := provider.calls.Load(); got != 0 {
				t.Fatalf("provider calls during New() = %d, want 0", got)
			}
		})
	}
}

func TestNewDoesNotInjectLongMemoryToolWithoutMemory(t *testing.T) {
	cfg := &Config{
		Config: adkllmagent.Config{
			Name:  "no-memory-agent",
			Model: testLLM{},
		},
	}

	if _, err := New(cfg); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if len(cfg.Tools) != 0 {
		t.Fatalf("tools = %d, want 0", len(cfg.Tools))
	}
}

func TestNewInjectsStandardMemoryToolWithMemory(t *testing.T) {
	cfg := &Config{
		Config: adkllmagent.Config{
			Name:  "memory-agent",
			Model: testLLM{},
		},
		LongTermMemory: memory.InMemoryService(),
	}

	if _, err := New(cfg); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !hasTool(cfg, "load_memory") {
		t.Fatalf("expected load_memory tool, got %v", toolNames(cfg))
	}
}

func TestNewKeepsManualLegacyLongMemoryTool(t *testing.T) {
	longMemoryTool, err := builtin_tools.LoadLongMemoryTool()
	if err != nil {
		t.Fatalf("LoadLongMemoryTool() error = %v", err)
	}
	cfg := &Config{
		Config: adkllmagent.Config{
			Name:  "manual-memory-agent",
			Model: testLLM{},
			Tools: []tool.Tool{longMemoryTool},
		},
		LongTermMemory: memory.InMemoryService(),
	}

	if _, err := New(cfg); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := countTool(cfg, "search_past_conversations"); got != 1 {
		t.Fatalf("search_past_conversations tools = %d, want 1", got)
	}
	if !hasTool(cfg, "load_memory") {
		t.Fatalf("expected load_memory tool, got %v", toolNames(cfg))
	}
}

func TestNewDoesNotDuplicateManualStandardMemoryTool(t *testing.T) {
	cfg := &Config{
		Config: adkllmagent.Config{
			Name:  "manual-standard-memory-agent",
			Model: testLLM{},
			Tools: []tool.Tool{loadmemorytool.New()},
		},
		LongTermMemory: memory.InMemoryService(),
	}

	if _, err := New(cfg); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := countTool(cfg, "load_memory"); got != 1 {
		t.Fatalf("load_memory tools = %d, want 1", got)
	}
}

func TestNewKeepsKnowledgeBaseInjectionWithLongMemory(t *testing.T) {
	var backend _interface.KnowledgeBackend = testKnowledgeBackend{}
	kb, err := knowledgebase.NewKnowledgeBase(backend)
	if err != nil {
		t.Fatalf("NewKnowledgeBase() error = %v", err)
	}

	cfg := &Config{
		Config: adkllmagent.Config{
			Name:  "knowledge-memory-agent",
			Model: testLLM{},
		},
		KnowledgeBase:  kb,
		LongTermMemory: memory.InMemoryService(),
	}

	if _, err := New(cfg); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !hasTool(cfg, knowledgebase.DefaultName) {
		t.Fatalf("expected %s tool, got %v", knowledgebase.DefaultName, toolNames(cfg))
	}
	if !hasTool(cfg, "load_memory") {
		t.Fatalf("expected load_memory tool, got %v", toolNames(cfg))
	}
}

func hasTool(cfg *Config, name string) bool {
	for _, t := range cfg.Tools {
		if t.Name() == name {
			return true
		}
	}
	return false
}

func toolNames(cfg *Config) []string {
	var names []string
	for _, t := range cfg.Tools {
		names = append(names, t.Name())
	}
	return names
}

func countTool(cfg *Config, name string) int {
	var count int
	for _, t := range cfg.Tools {
		if t.Name() == name {
			count++
		}
	}
	return count
}
