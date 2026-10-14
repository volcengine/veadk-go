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

	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/log"
	verunner "github.com/volcengine/veadk-go/v2/runner"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/session"
)

func main() {
	ctx := context.Background()
	appName := "ve_agent"
	userID := "user4567"

	sessionServer := session.InMemoryService()
	memoryServer := memory.InMemoryService()
	//memoryServer, err := vem.NewLongTermMemoryService(vem.BackendLongTermViking, nil)
	//if err != nil {
	//	log.Errorf("NewLongTermMemoryService failed: %v", err)
	//	return
	//}

	a, err := veagent.New(&veagent.Config{
		Config: llmagent.Config{
			Name:        "personal_assistant",
			Instruction: "You are a personal assistant with long-term memory capabilities. Before answering the user's questions, you must invoke the tool to retrieve memory information.",
		},
		LongTermMemory: memoryServer,
	})
	if err != nil {
		log.Errorf("NewLLMAgent failed: %v", err)
		return
	}

	r, err := verunner.New(verunner.Config{
		AppName:           appName,
		Agent:             a,
		ShortTermMemory:   sessionServer,
		LongTermMemory:    memoryServer,
		AutoCreateSession: true,
	})
	if err != nil {
		log.Errorf("create runner error %v", err)
		return
	}

	sessionID := "session123456789"

	finalResponseText, err := r.RunText(ctx, userID, sessionID, "My favorite project is Project Alpha.", agent.RunConfig{})
	if err != nil {
		log.Errorf("Agent 1 Error: %v", err)
		return
	}
	log.Infof("Agent 1 Response: %s\n", finalResponseText)

	// Add the completed session to the Memory Service
	log.Info("\n--- Adding Session 1 to Memory ---")
	if err := r.SaveSessionToLongTermMemory(ctx, userID, sessionID, nil); err != nil {
		log.Errorf("Failed to add session to memory: %v", err)
		return
	}
	log.Info("Session added to memory.")

	log.Info("\n--- Turn 2: Recalling Information ---")
	finalResponseText2, err := r.RunText(ctx, userID, "session2222", "What is my favorite project?", agent.RunConfig{})
	if err != nil {
		log.Errorf("Agent 2 Error: %v", err)
		return
	}
	log.Infof("Agent 2 Response: %s\n", finalResponseText2)
}
