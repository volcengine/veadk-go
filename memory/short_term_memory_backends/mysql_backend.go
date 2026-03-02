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

	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"google.golang.org/adk/session"
	"google.golang.org/adk/session/database"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type MysqlBackendConfig struct {
	*configs.CommonDatabaseConfig
}

func NewMysqlSTMBackend(config *MysqlBackendConfig) (session.Service, error) {
	if config == nil {
		return nil, fmt.Errorf("mysql config is nil")
	}
	if config.DBUrl != "" {
		log.Info("DbURL is set, ignore backend option")
	} else {
		encodedUsername := url.QueryEscape(config.User)
		encodedPassword := url.QueryEscape(config.Password)

		config.DBUrl = fmt.Sprintf(
			"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			encodedUsername, encodedPassword,
			config.Host, config.Port, config.Database,
		)
	}

	sessionService, err := database.NewSessionService(
		mysql.Open(config.DBUrl),
		&gorm.Config{PrepareStmt: true, Logger: log.NewGormLogger(slog.LevelError)},
	)
	if err != nil {
		log.Error(fmt.Sprintf("init MySQL DatabaseSessionService failed: %v", err))
		return nil, err
	}
	if initErr := database.AutoMigrate(sessionService); initErr != nil {
		log.Error(fmt.Sprintf("AutoMigrate MySQL DatabaseSessionService failed: %v", initErr))
	}

	return sessionService, nil
}
