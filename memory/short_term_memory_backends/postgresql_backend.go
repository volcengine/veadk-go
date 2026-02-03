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
	"net/url"
	"strings"

	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"google.golang.org/adk/session"
	"google.golang.org/adk/session/database"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type PostgresqlBackendConfig struct {
	*configs.CommonDatabaseConfig
}

func NewPostgreSqlSTMBackend(config *PostgresqlBackendConfig) (session.Service, error) {
	if config == nil {
		return nil, fmt.Errorf("postgresql config is nil")
	}
	if config.DBUrl != "" {
		log.Info("DbURL is set, ignore backend option")
		if strings.Count(config.DBUrl, "@") > 1 || strings.Count(config.DBUrl, ":") > 3 {
			log.Warn(
				"Multiple `@` or `:` symbols detected in the database URL. " +
					"Please encode `username` or `password` with url.QueryEscape. " +
					"Examples: p@ssword→p%40ssword.",
			)
		}
	} else {
		encodedUsername := url.QueryEscape(config.User)
		encodedPassword := url.QueryEscape(config.Password)

		config.DBUrl = fmt.Sprintf(
			"postgresql://%s:%s@%s:%s/%s",
			encodedUsername, encodedPassword,
			config.Host, config.Port, config.Database,
		)
	}

	sessionService, err := database.NewSessionService(
		postgres.Open(config.DBUrl),
		&gorm.Config{PrepareStmt: true, Logger: log.NewGormLogger(slog.LevelError)},
	)
	if err != nil {
		log.Error(fmt.Sprintf("init DatabaseSessionService failed: %v", err))
		return nil, err
	}
	if initErr := database.AutoMigrate(sessionService); initErr != nil {
		log.Error(fmt.Sprintf("AutoMigrate DatabaseSessionService failed: %v", initErr))
	}

	return sessionService, nil
}
