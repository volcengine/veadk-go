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
	"net/url"
	"strings"
	"sync"

	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"go.uber.org/zap/zapcore"
	"google.golang.org/adk/session"
	"google.golang.org/adk/session/database"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type PostgreSqlSTMBackend struct {
	// 配置字段
	PostgresqlConfig *configs.CommonDatabaseConfig

	sessionService session.Service
	once           sync.Once
}

func NewPostgreSqlSTMBackend(config *configs.CommonDatabaseConfig) (*PostgreSqlSTMBackend, error) {
	if config == nil {
		return nil, fmt.Errorf("postgresql config is nil")
	}
	backend := &PostgreSqlSTMBackend{
		PostgresqlConfig: config,
	}

	if config.DBUrl != "" {
		log.Info("DbURL is set, ignore backend option")
		// 检查DbURL格式（多@/:符号警告）
		if strings.Count(config.DBUrl, "@") > 1 || strings.Count(config.DBUrl, ":") > 3 {
			log.Warn(
				"Multiple `@` or `:` symbols detected in the database URL. " +
					"Please encode `username` or `password` with url.QueryEscape. " +
					"Examples: p@ssword→p%40ssword.",
			)
		}
	} else {
		encodedUsername := url.QueryEscape(config.UserName)
		encodedPassword := url.QueryEscape(config.Password)

		backend.PostgresqlConfig.DBUrl = fmt.Sprintf(
			"postgresql://%s:%s@%s:%s/%s",
			encodedUsername, encodedPassword,
			config.Host, config.Port, config.Schema,
		)
	}

	return backend, nil
}

func (b *PostgreSqlSTMBackend) SessionService() (session.Service, error) {
	var initErr error
	b.once.Do(func() {
		// 初始化DatabaseSessionService（仅执行一次）
		level, err := zapcore.ParseLevel(b.PostgresqlConfig.GormLogLevel)
		if err != nil {
			level = zapcore.InfoLevel
		}
		b.sessionService, initErr = database.NewSessionService(
			postgres.Open(b.PostgresqlConfig.DBUrl),
			&gorm.Config{PrepareStmt: true, Logger: log.NewLogger(level)},
		)
		if initErr != nil {
			log.Error(fmt.Sprintf("init DatabaseSessionService failed: %v", initErr))
		} else {
			log.Info(fmt.Sprintf("PostgreSQL SessionService initialized with URL: %s", b.PostgresqlConfig.DBUrl))
		}
		if initErr = database.AutoMigrate(b.sessionService); initErr != nil {
			log.Error(fmt.Sprintf("AutoMigrate DatabaseSessionService failed: %v", initErr))
		}
	})

	if initErr != nil {
		return nil, initErr
	}
	return b.sessionService, nil
}

type BaseShortTermMemoryBackend interface {
	SessionService() (session.Service, error)
}
