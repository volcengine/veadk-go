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
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/utils"
	"gopkg.in/yaml.v3"
)

func Test_loadConfigFromProjectEnv(t *testing.T) {
	fd, _ := os.Create(".env")
	_, _ = fd.WriteString("MODEL_AGENT_NAME=doubao-seed-1-6-250615")
	_ = fd.Close()
	defer func() {
		_ = os.Remove(".env")
	}()

	_ = loadConfigFromProjectEnv()
	assert.Equal(t, "doubao-seed-1-6-250615", os.Getenv(common.MODEL_AGENT_NAME))

	_ = os.Setenv(common.MODEL_AGENT_NAME, "test")
	defer func() {
		_ = os.Unsetenv(common.MODEL_AGENT_NAME)
	}()
	_ = loadConfigFromProjectEnv()
	assert.Equal(t, "test", os.Getenv(common.MODEL_AGENT_NAME))
}

func Test_loadConfigFromProjectYaml(t *testing.T) {
	fd, _ := os.Create("config.yaml")
	_, _ = fd.WriteString(`model:
  agent:
    name: "doubao-seed-1-6-250615"
    api_base: "test"`)
	_ = fd.Close()
	defer func() {
		_ = os.Remove("config.yaml")
	}()
	_ = loadConfigFromProjectYaml()
	assert.Equal(t, "doubao-seed-1-6-250615", os.Getenv(common.MODEL_AGENT_NAME))

	_ = os.Setenv(common.MODEL_AGENT_NAME, "test")
	defer func() {
		_ = os.Unsetenv(common.MODEL_AGENT_NAME)
	}()
	_ = loadConfigFromProjectYaml()
	assert.Equal(t, "test", os.Getenv(common.MODEL_AGENT_NAME))
}

func Test_getEnv(t *testing.T) {
	fd, _ := os.Create("config.yaml")
	_, _ = fd.WriteString(`model:
  agent:
    name: "doubao-seed-1-6-250615"
    api_base: "test"`)
	_ = fd.Close()
	defer func() {
		_ = os.Remove("config.yaml")
	}()
	_ = loadConfigFromProjectYaml()
	assert.Equal(t, "doubao-seed-1-6-250615", utils.GetEnvWithDefault(common.MODEL_AGENT_NAME))
	assert.Equal(t, "test", utils.GetEnvWithDefault("test_key", "test"))
}

func TestSetupVeADKConfig(t *testing.T) {
	fd, _ := os.Create("config.yaml")
	_, _ = fd.WriteString(`model:
  agent:
    name: "doubao-seed-1-6-250615"
    api_base: "test"`)
	_ = fd.Close()
	defer func() {
		_ = os.Remove("config.yaml")
	}()
	_ = SetupVeADKConfig()
	assert.Equal(t, "doubao-seed-1-6-250615", os.Getenv(common.MODEL_AGENT_NAME))
}

func TestObservabilityConfig_YamlMapping(t *testing.T) {
	yamlData := `
opentelemetry:
  apmplus:
    endpoint: "http://apmplus-example.com"
    api_key: "test-key"
    service_name: "test-service"
  enable_global_tracer: true
`
	var config ObservabilityConfig
	err := yaml.Unmarshal([]byte(yamlData), &config)
	assert.NoError(t, err)

	assert.NotNil(t, config.OpenTelemetry)
	assert.NotNil(t, config.OpenTelemetry.ApmPlus)
	assert.Equal(t, "http://apmplus-example.com", config.OpenTelemetry.ApmPlus.Endpoint)
	assert.Equal(t, "test-key", config.OpenTelemetry.ApmPlus.APIKey)
	assert.Equal(t, "test-service", config.OpenTelemetry.ApmPlus.ServiceName)
	assert.True(t, config.OpenTelemetry.EnableGlobalProvider)

	assert.Equal(t, "test-service", config.OpenTelemetry.ApmPlus.ServiceName)
	assert.True(t, config.OpenTelemetry.EnableGlobalProvider)
}

func TestObservabilityConfig_EnvMapping(t *testing.T) {
	os.Setenv("OBSERVABILITY_OPENTELEMETRY_APMPLUS_ENDPOINT", "http://env-endpoint")
	os.Setenv("OBSERVABILITY_OPENTELEMETRY_ENABLE_GLOBAL_PROVIDER", "true")
	defer func() {
		os.Unsetenv("OBSERVABILITY_OPENTELEMETRY_APMPLUS_ENDPOINT")
		os.Unsetenv("OBSERVABILITY_OPENTELEMETRY_ENABLE_GLOBAL_PROVIDER")
	}()

	config := &ObservabilityConfig{}
	config.MapEnvToConfig()

	assert.NotNil(t, config.OpenTelemetry)
	assert.NotNil(t, config.OpenTelemetry.ApmPlus)
	assert.Equal(t, "http://env-endpoint", config.OpenTelemetry.ApmPlus.Endpoint)
	assert.True(t, config.OpenTelemetry.EnableGlobalProvider)
}

func TestObservabilityConfig_Priority(t *testing.T) {
	// Nested priority check: CozeLoop > APMPlus
	config := &ObservabilityConfig{
		OpenTelemetry: &OpenTelemetryConfig{
			ApmPlus: &ApmPlusConfig{
				Endpoint: "apm-endpoint",
			},
			CozeLoop: &CozeLoopExporterConfig{
				Endpoint: "coze-endpoint",
			},
		},
	}
	assert.NotNil(t, config.OpenTelemetry.CozeLoop)
}
