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

package memory

import (
	"context"
	"fmt"

	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/memory/short_term_memory_backends"
	"google.golang.org/adk/session"
)

type BackendType string

const (
	BackendLocal      BackendType = "local"
	BackendPostgreSQL BackendType = "postgresql"
)

type ShortTermMemory struct {
	// 配置字段
	config         *configs.DatabaseConfig
	sessionService session.Service
}

// NewShortTermMemory 创建ShortTermMemory实例
func NewShortTermMemory(config *configs.DatabaseConfig) (*ShortTermMemory, error) {
	if config == nil {
		config = configs.GetGlobalConfig().Database
	}

	shortTermMemory := &ShortTermMemory{
		config: config,
	}

	// 根据后端类型初始化SessionService
	switch BackendType(config.ShortTermMemoryBackend) {
	case BackendLocal:
		shortTermMemory.sessionService = session.InMemoryService()
	case BackendPostgreSQL:
		pgBackend, err := short_term_memory_backends.NewPostgreSqlSTMBackend(config.Postgresql)
		if err != nil {
			return nil, err
		}
		shortTermMemory.sessionService, err = pgBackend.SessionService()
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported backend type: %s", config.ShortTermMemoryBackend)
	}

	return shortTermMemory, nil
}

func (stm *ShortTermMemory) SessionService() session.Service {
	return stm.sessionService
}

func (stm *ShortTermMemory) CreateSession(ctx context.Context, appName, userID, sessionID string) (session.Session, error) {
	// 创建新会话
	newSession, err := stm.sessionService.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("create session failed: %w", err)
	}
	return newSession.Session, nil
}

func (stm *ShortTermMemory) GetSession(ctx context.Context, appName, userID, sessionID string) (session.Session, error) {
	getResp, err := stm.sessionService.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("get session failed: %w", err)
	}

	return getResp.Session, nil
}
