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
	"strconv"

	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/utils"
)

type CommonDatabaseConfig struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Host     string `yaml:"host"`
	Port     string `yaml:"port"`
	Database string `yaml:"database"`
	DBUrl    string `yaml:"db_url"`
}
type DatabaseConfig struct {
	Postgresql *CommonDatabaseConfig `yaml:"postgresql"`
	Viking     *VikingConfig         `yaml:"viking"`
	TOS        *TosClientConf        `yaml:"tos"`
	Mem0       *Mem0Config           `yaml:"mem0"`
	Redis      *RedisConfig          `yaml:"redis"`
	OpenSearch *OpenSearchConfig     `yaml:"opensearch"`
}

func (c *DatabaseConfig) MapEnvToConfig() {
	c.Postgresql.User = utils.GetEnvWithDefault(common.DATABASE_POSTGRESQL_USER)
	c.Postgresql.Password = utils.GetEnvWithDefault(common.DATABASE_POSTGRESQL_PASSWORD)
	c.Postgresql.Host = utils.GetEnvWithDefault(common.DATABASE_POSTGRESQL_HOST)
	c.Postgresql.Port = utils.GetEnvWithDefault(common.DATABASE_POSTGRESQL_PORT)
	c.Postgresql.Database = utils.GetEnvWithDefault(common.DATABASE_POSTGRESQL_DATABASE)
	c.Postgresql.DBUrl = utils.GetEnvWithDefault(common.DATABASE_POSTGRESQL_DBURL)

	c.Viking.MapEnvToConfig()
	c.TOS.MapEnvToConfig()
	c.Mem0.MapEnvToConfig()
	c.Redis.MapEnvToConfig()
	c.OpenSearch.MapEnvToConfig()
}

// RedisConfig holds Redis connection configuration.
type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

func (r *RedisConfig) MapEnvToConfig() {
	r.Host = utils.GetEnvWithDefault(common.DATABASE_REDIS_HOST)
	if portStr := utils.GetEnvWithDefault(common.DATABASE_REDIS_PORT); portStr != "" {
		r.Port, _ = strconv.Atoi(portStr)
	}
	r.Username = utils.GetEnvWithDefault(common.DATABASE_REDIS_USERNAME)
	r.Password = utils.GetEnvWithDefault(common.DATABASE_REDIS_PASSWORD)
	if dbStr := utils.GetEnvWithDefault(common.DATABASE_REDIS_DB); dbStr != "" {
		r.DB, _ = strconv.Atoi(dbStr)
	}
}

// OpenSearchConfig holds OpenSearch connection configuration.
type OpenSearchConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	UseSSL   bool   `yaml:"use_ssl"`
	CertPath string `yaml:"cert_path"`
}

func (o *OpenSearchConfig) MapEnvToConfig() {
	o.Host = utils.GetEnvWithDefault(common.DATABASE_OPENSEARCH_HOST)
	if portStr := utils.GetEnvWithDefault(common.DATABASE_OPENSEARCH_PORT); portStr != "" {
		o.Port, _ = strconv.Atoi(portStr)
	}
	o.Username = utils.GetEnvWithDefault(common.DATABASE_OPENSEARCH_USERNAME)
	o.Password = utils.GetEnvWithDefault(common.DATABASE_OPENSEARCH_PASSWORD)
	if sslStr := utils.GetEnvWithDefault(common.DATABASE_OPENSEARCH_USE_SSL); sslStr != "" {
		o.UseSSL, _ = strconv.ParseBool(sslStr)
	}
	o.CertPath = utils.GetEnvWithDefault(common.DATABASE_OPENSEARCH_CERT_PATH)
}

// Mem0Config
type Mem0Config struct {
	BaseUrl   string `yaml:"base_url"`
	ApiKey    string `yaml:"api_key"`
	ProjectId string `yaml:"project_id"`
	Region    string `yaml:"region"`
}

func (v *Mem0Config) MapEnvToConfig() {
	v.BaseUrl = utils.GetEnvWithDefault(common.DATABASE_MEM0_BASE_URL)
	v.ApiKey = utils.GetEnvWithDefault(common.DATABASE_MEM0_API_KEY)
	v.Region = utils.GetEnvWithDefault(common.DATABASE_MEM0_REGION)
}
