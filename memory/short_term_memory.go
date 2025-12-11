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
	"fmt"

	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/memory/short_term_memory_backends"
	"google.golang.org/adk/session"
)

type ShortTermBackendType string

const (
	BackendShortTermLocal      ShortTermBackendType = "local"
	BackendShortTermPostgreSQL ShortTermBackendType = "postgresql"
)

// NewShortTermMemoryService creates a new short term memory service.
// If backend is empty, it will use the default backend.
// If config is nil, it will use the default config with backend
// If config is not nil, it will use the config.
func NewShortTermMemoryService(backend ShortTermBackendType, config interface{}) (session.Service, error) {
	if backend == "" {
		backend = BackendShortTermLocal
	}

	switch backend {
	case BackendShortTermLocal:
		return session.InMemoryService(), nil
	case BackendShortTermPostgreSQL:
		pgCfg := &short_term_memory_backends.PostgresqlBackendConfig{}
		if config == nil {
			pgCfg = &short_term_memory_backends.PostgresqlBackendConfig{
				CommonDatabaseConfig: configs.GetGlobalConfig().Database.Postgresql,
			}
		} else {
			var ok bool
			pgCfg, ok = config.(*short_term_memory_backends.PostgresqlBackendConfig)
			if !ok {
				return nil, fmt.Errorf("postgresql backend requires *PostgresqlBackendConfig, got %T", config)
			}
		}
		sessionService, err := short_term_memory_backends.NewPostgreSqlSTMBackend(pgCfg)
		if err != nil {
			return nil, err
		}
		return sessionService, nil
	default:
		return nil, fmt.Errorf("unsupported backend type: %s", backend)
	}
}
