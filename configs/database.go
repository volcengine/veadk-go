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

package configs

import (
	"github.com/volcengine/veadk-go/common"
)

type CommonDatabaseConfig struct {
	UserName     string
	Password     string
	Host         string
	Port         string
	Schema       string
	DBUrl        string
	GormLogLevel string
}
type DatabaseConfig struct {
	ShortTermMemoryBackend string
	Postgresql             *CommonDatabaseConfig
}

func (c *DatabaseConfig) MapEnvToConfig() {
	c.ShortTermMemoryBackend = getEnv(common.DATABASE_SHORT_TERM_MEMORY_BACKEND, common.DEFAULT_SHORT_TERM_MEMORY_BACKEND, true)
	switch c.ShortTermMemoryBackend {
	case "postgresql":
		c.Postgresql.UserName = getEnv(common.DATABASE_POSTGRESQL_USERNAME, "", true)
		c.Postgresql.Password = getEnv(common.DATABASE_POSTGRESQL_PASSWORD, "", true)
		c.Postgresql.Host = getEnv(common.DATABASE_POSTGRESQL_HOST, "", true)
		c.Postgresql.Port = getEnv(common.DATABASE_POSTGRESQL_PORT, "", true)
		c.Postgresql.Schema = getEnv(common.DATABASE_POSTGRESQL_SCHEMA, "", true)
		c.Postgresql.DBUrl = getEnv(common.DATABASE_POSTGRESQL_DBURL, "", true)
		c.Postgresql.GormLogLevel = getEnv(common.DATABASE_POSTGRESQL_GORMLOGLEVEL, common.DEFAULT_GORM_LOG_LEVEL, true)
	}
}
