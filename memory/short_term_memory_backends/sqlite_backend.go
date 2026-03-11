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

package short_term_memory_backends

import (
	"fmt"
	"log/slog"

	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"google.golang.org/adk/session"
	"google.golang.org/adk/session/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type SqliteBackendConfig struct {
	*configs.CommonDatabaseConfig
}

func NewSqliteSTMBackend(config *SqliteBackendConfig) (session.Service, error) {
	if config == nil {
		return nil, fmt.Errorf("sqlite config is nil")
	}
	if config.DBUrl == "" {
		config.DBUrl = "file::memory:?cache=shared"
		log.Info("SQLite DBUrl is empty, using in-memory database")
	}

	sessionService, err := database.NewSessionService(
		sqlite.Open(config.DBUrl),
		&gorm.Config{PrepareStmt: true, Logger: log.NewGormLogger(slog.LevelError)},
	)
	if err != nil {
		log.Error(fmt.Sprintf("init SQLite DatabaseSessionService failed: %v", err))
		return nil, err
	}
	if initErr := database.AutoMigrate(sessionService); initErr != nil {
		log.Error(fmt.Sprintf("AutoMigrate SQLite DatabaseSessionService failed: %v", initErr))
	}

	return sessionService, nil
}
