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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"sync"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type VeADKConfig struct {
	Volcengine     *Volcengine          `yaml:"volcengine"`
	Model          *ModelConfig         `yaml:"model"`
	Tool           *BuiltinToolConfigs  `yaml:"tools"`
	PromptPilot    *PromptPilotConfig   `yaml:"prompt_pilot"`
	CozeLoopConfig *CozeLoopConfig      `yaml:"coze_loop"`
	TlsConfig      *TLSConfig           `yaml:"tls_config"`
	Veidentity     *VeIdentityConfig    `yaml:"veidentity"`
	Database       *DatabaseConfig      `yaml:"database"`
	LOGGING        *Logging             `yaml:"LOGGING"`
	Observability  *ObservabilityConfig `yaml:"observability"`
}

type EnvConfigMaptoStruct interface {
	MapEnvToConfig() // 用于映射环境变量到结构体字段
}

var (
	globalConfig *VeADKConfig
	configOnce   sync.Once
)

func GetGlobalConfig() *VeADKConfig {
	configOnce.Do(func() {
		if err := SetupVeADKConfig(); err != nil {
			panic(err)
		}
	})
	return globalConfig
}

func SetupVeADKConfig() error {
	if err := loadConfigFromProjectEnv(); err != nil {
		return err
	}
	if err := loadConfigFromProjectYaml(); err != nil {
		return err
	}
	// 3. 从环境变量构建最终配置
	globalConfig = &VeADKConfig{
		Volcengine: &Volcengine{},
		Model: &ModelConfig{
			Agent: &AgentConfig{},
			Image: &CommonModelConfig{},
			Video: &CommonModelConfig{},
		},
		Tool: &BuiltinToolConfigs{
			MCPRouter: &MCPRouter{},
			RunCode:   &RunCode{},
			LLMShield: &LLMShield{},
		},
		PromptPilot:    &PromptPilotConfig{},
		CozeLoopConfig: &CozeLoopConfig{},
		TlsConfig:      &TLSConfig{},
		Veidentity:     &VeIdentityConfig{},
		LOGGING:        &Logging{},
		Database: &DatabaseConfig{
			Postgresql: &CommonDatabaseConfig{},
			Viking:     &VikingConfig{},
			TOS:        &TosClientConf{},
			Mem0:       &Mem0Config{},
		},
		Observability: &ObservabilityConfig{
			OpenTelemetry: &OpenTelemetryConfig{
				EnableGlobalProvider: true,  // use global trace provider by default, like veadk-python
				EnableLocalProvider:  false, // disable adk-go's local provider
			},
		},
	}
	globalConfig.Model.MapEnvToConfig()
	globalConfig.Tool.MapEnvToConfig()
	globalConfig.PromptPilot.MapEnvToConfig()
	globalConfig.CozeLoopConfig.MapEnvToConfig()
	globalConfig.LOGGING.MapEnvToConfig()
	globalConfig.Database.MapEnvToConfig()
	globalConfig.Volcengine.MapEnvToConfig()
	globalConfig.Observability.MapEnvToConfig()
	return nil
}

func loadConfigFromProjectEnv() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	envFilePath := filepath.Join(dir, ".env")
	if _, err := os.Stat(envFilePath); err == nil {
		// godotenv.Load 默认不会覆盖已存在的环境变量
		if err := godotenv.Load(envFilePath); err != nil {
			return fmt.Errorf("加载 .env 文件失败: %v", err)
		}
	}
	return nil
}

func loadConfigFromProjectYaml() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	// 2. 加载 config.yaml（优先级最低）
	var yamlConfig map[string]interface{}
	configYamlPath := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(configYamlPath); err == nil {
		data, err := os.ReadFile(configYamlPath)
		if err != nil {
			return fmt.Errorf("读取 config.yaml 失败: %v", err)
		}
		if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
			return fmt.Errorf("解析 config.yaml 失败: %v", err)
		}

		// 将 yaml 配置转换为环境变量格式（如 model.name -> MODEL_NAME），但不覆盖已有变量
		setYamlToEnv(yamlConfig, "")
	}
	return nil
}

func setYamlToEnv(data map[string]interface{}, prefix string) {
	for key, val := range data {
		fullKey := key
		if prefix != "" {
			fullKey = fmt.Sprintf("%s_%s", prefix, key)
		}
		fullKey = strings.ToUpper(fullKey)

		switch v := val.(type) {
		case map[string]interface{}:
			setYamlToEnv(v, fullKey)
		case string:
			// 仅在环境变量不存在时设置
			if os.Getenv(fullKey) == "" {
				_ = os.Setenv(fullKey, v)
			}
		case int:
			if os.Getenv(fullKey) == "" {
				_ = os.Setenv(fullKey, strconv.Itoa(v))
			}
		case bool:
			if os.Getenv(fullKey) == "" {
				_ = os.Setenv(fullKey, strconv.FormatBool(v))
			}
		}
	}
}
