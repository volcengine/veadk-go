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

	"github.com/volcengine/veadk-go/utils"
)

const (
	// Global
	EnvOtelServiceName                   = "OTEL_SERVICE_NAME"
	EnvObservabilityEnableLocalProvider  = "OBSERVABILITY_OPENTELEMETRY_ENABLE_LOCAL_PROVIDER"
	EnvObservabilityEnableGlobalProvider = "OBSERVABILITY_OPENTELEMETRY_ENABLE_GLOBAL_PROVIDER"
	EnvObservabilityEnableMetrics        = "OBSERVABILITY_OPENTELEMETRY_ENABLE_METRICS"

	// APMPlus
	EnvObservabilityOpenTelemetryApmPlusProtocol    = "OBSERVABILITY_OPENTELEMETRY_APMPLUS_PROTOCOL"
	EnvObservabilityOpenTelemetryApmPlusEndpoint    = "OBSERVABILITY_OPENTELEMETRY_APMPLUS_ENDPOINT"
	EnvObservabilityOpenTelemetryApmPlusAPIKey      = "OBSERVABILITY_OPENTELEMETRY_APMPLUS_API_KEY"
	EnvObservabilityOpenTelemetryApmPlusServiceName = "OBSERVABILITY_OPENTELEMETRY_APMPLUS_SERVICE_NAME"

	// CozeLoop
	EnvObservabilityOpenTelemetryCozeLoopEndpoint    = "OBSERVABILITY_OPENTELEMETRY_COZELOOP_ENDPOINT"
	EnvObservabilityOpenTelemetryCozeLoopAPIKey      = "OBSERVABILITY_OPENTELEMETRY_COZELOOP_API_KEY"
	EnvObservabilityOpenTelemetryCozeLoopServiceName = "OBSERVABILITY_OPENTELEMETRY_COZELOOP_SERVICE_NAME"

	// TLS
	EnvObservabilityOpenTelemetryTLSEndpoint    = "OBSERVABILITY_OPENTELEMETRY_TLS_ENDPOINT"
	EnvObservabilityOpenTelemetryTLSServiceName = "OBSERVABILITY_OPENTELEMETRY_TLS_SERVICE_NAME"
	EnvObservabilityOpenTelemetryTLSRegion      = "OBSERVABILITY_OPENTELEMETRY_TLS_REGION"
	EnvObservabilityOpenTelemetryTLSTopicID     = "OBSERVABILITY_OPENTELEMETRY_TLS_TOPIC_ID"
	EnvObservabilityOpenTelemetryTLSAccessKey   = "OBSERVABILITY_OPENTELEMETRY_TLS_ACCESS_KEY"
	EnvObservabilityOpenTelemetryTLSSecretKey   = "OBSERVABILITY_OPENTELEMETRY_TLS_SECRET_KEY"

	// File
	EnvObservabilityOpenTelemetryFilePath = "OBSERVABILITY_OPENTELEMETRY_FILE_PATH"

	// Stdout
	EnvObservabilityOpenTelemetryStdoutEnable = "OBSERVABILITY_OPENTELEMETRY_STDOUT_ENABLE"
)

// ObservabilityConfig groups specific configurations for different platforms.
type ObservabilityConfig struct {
	OpenTelemetry *OpenTelemetryConfig `yaml:"opentelemetry"`
}

type OpenTelemetryConfig struct {
	EnableLocalProvider  bool  `yaml:"enable_local_tracer"`
	EnableGlobalProvider bool  `yaml:"enable_global_tracer"`
	EnableMetrics        *bool `yaml:"enable_metrics"`

	File     *FileConfig             `yaml:"file"`
	Stdout   *StdoutConfig           `yaml:"stdout"`
	ApmPlus  *ApmPlusConfig          `yaml:"apmplus"`
	CozeLoop *CozeLoopExporterConfig `yaml:"cozeloop"`
	TLS      *TLSExporterConfig      `yaml:"tls"`
}

type ApmPlusConfig struct {
	Protocol    string `yaml:"protocol"` // grpc by default
	Endpoint    string `yaml:"endpoint"`
	APIKey      string `yaml:"api_key"`
	ServiceName string `yaml:"service_name"`
}

type CozeLoopExporterConfig struct {
	Endpoint    string `yaml:"endpoint"`
	APIKey      string `yaml:"api_key"`
	ServiceName string `yaml:"service_name"`
}

type TLSExporterConfig struct {
	Endpoint    string `yaml:"endpoint"`
	ServiceName string `yaml:"service_name"`
	Region      string `yaml:"region"`
	TopicID     string `yaml:"topic_id"`
	AccessKey   string `yaml:"access_key"`
	SecretKey   string `yaml:"secret_key"`
}

type FileConfig struct {
	Path string `yaml:"path"`
}

type StdoutConfig struct {
	Enable bool `yaml:"enable"`
}

func (c *ObservabilityConfig) MapEnvToConfig() {
	if c.OpenTelemetry == nil {
		c.OpenTelemetry = &OpenTelemetryConfig{}
	}
	ot := c.OpenTelemetry

	// APMPlus
	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryApmPlusEndpoint); v != "" {
		if ot.ApmPlus == nil {
			ot.ApmPlus = &ApmPlusConfig{}
		}

		ot.ApmPlus.Endpoint = v

		if ot.EnableMetrics == nil {
			ot.EnableMetrics = new(bool)
			*ot.EnableMetrics = true
		}
	}

	// APMPlus Protocol
	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryApmPlusProtocol); v != "" {
		if ot.ApmPlus == nil {
			ot.ApmPlus = &ApmPlusConfig{}
		}
		ot.ApmPlus.Protocol = v
	}

	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryApmPlusAPIKey); v != "" {
		if ot.ApmPlus == nil {
			ot.ApmPlus = &ApmPlusConfig{}
		}
		if ot.ApmPlus.APIKey == "" {
			ot.ApmPlus.APIKey = v
		}
	}

	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryApmPlusServiceName); v != "" {
		if ot.ApmPlus == nil {
			ot.ApmPlus = &ApmPlusConfig{}
		}
		ot.ApmPlus.ServiceName = v
		if os.Getenv(EnvOtelServiceName) == "" {
			os.Setenv(EnvOtelServiceName, v)
		}
	}

	// CozeLoop
	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryCozeLoopEndpoint); v != "" {
		if ot.CozeLoop == nil {
			ot.CozeLoop = &CozeLoopExporterConfig{}
		}
		ot.CozeLoop.Endpoint = v
	}
	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryCozeLoopAPIKey); v != "" {
		if ot.CozeLoop == nil {
			ot.CozeLoop = &CozeLoopExporterConfig{}
		}
		ot.CozeLoop.APIKey = v
	}

	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryCozeLoopServiceName); v != "" {
		if ot.CozeLoop == nil {
			ot.CozeLoop = &CozeLoopExporterConfig{}
		}
		ot.CozeLoop.ServiceName = v
		if os.Getenv(EnvOtelServiceName) == "" {
			os.Setenv(EnvOtelServiceName, v)
		}
	}

	// TLS
	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryTLSEndpoint); v != "" {
		if ot.TLS == nil {
			ot.TLS = &TLSExporterConfig{}
		}
		ot.TLS.Endpoint = v
	}

	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryTLSServiceName); v != "" {
		if ot.TLS == nil {
			ot.TLS = &TLSExporterConfig{}
		}
		ot.TLS.ServiceName = v
		if os.Getenv(EnvOtelServiceName) == "" {
			os.Setenv(EnvOtelServiceName, v)
		}
	}

	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryTLSRegion); v != "" {
		if ot.TLS == nil {
			ot.TLS = &TLSExporterConfig{}
		}
		ot.TLS.Region = v
	}

	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryTLSTopicID); v != "" {
		if ot.TLS == nil {
			ot.TLS = &TLSExporterConfig{}
		}
		ot.TLS.TopicID = v
	}

	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryTLSAccessKey); v != "" {
		if ot.TLS == nil {
			ot.TLS = &TLSExporterConfig{}
		}
		ot.TLS.AccessKey = v
	}
	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryTLSSecretKey); v != "" {
		if ot.TLS == nil {
			ot.TLS = &TLSExporterConfig{}
		}
		ot.TLS.SecretKey = v
	}

	// File
	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryFilePath); v != "" {
		if ot.File == nil {
			ot.File = &FileConfig{}
		}
		ot.File.Path = v
	}

	if v := utils.GetEnvWithDefault(EnvObservabilityOpenTelemetryStdoutEnable); v != "" {
		if ot.Stdout == nil {
			ot.Stdout = &StdoutConfig{}
		}
		ot.Stdout.Enable = v == "true"
	}

	// Global Tracer
	if v := utils.GetEnvWithDefault(EnvObservabilityEnableGlobalProvider); v != "" {
		ot.EnableGlobalProvider = v == "true"
	}

	// Local Tracer
	if v := utils.GetEnvWithDefault(EnvObservabilityEnableLocalProvider); v != "" {
		ot.EnableLocalProvider = v == "true"
	}

	// Meter Provider
	if v := utils.GetEnvWithDefault(EnvObservabilityEnableMetrics); v != "" {
		if ot.EnableMetrics == nil {
			ot.EnableMetrics = new(bool)
		}
		*ot.EnableMetrics = v == "true"
	}
}

func (c *ObservabilityConfig) Clone() *ObservabilityConfig {
	if c == nil {
		return nil
	}
	return &ObservabilityConfig{
		OpenTelemetry: c.OpenTelemetry.Clone(),
	}
}

func (c *OpenTelemetryConfig) Clone() *OpenTelemetryConfig {
	if c == nil {
		return nil
	}

	return &OpenTelemetryConfig{
		EnableGlobalProvider: c.EnableGlobalProvider,
		EnableLocalProvider:  c.EnableLocalProvider,
		EnableMetrics:        c.EnableMetrics,
		ApmPlus:              c.ApmPlus.Clone(),
		CozeLoop:             c.CozeLoop.Clone(),
		TLS:                  c.TLS.Clone(),
		File:                 c.File.Clone(),
		Stdout:               c.Stdout.Clone(),
	}
}

func (c *ApmPlusConfig) Clone() *ApmPlusConfig {
	if c == nil {
		return nil
	}
	return &ApmPlusConfig{
		Endpoint:    c.Endpoint,
		Protocol:    c.Protocol,
		APIKey:      c.APIKey,
		ServiceName: c.ServiceName,
	}
}

func (c *CozeLoopExporterConfig) Clone() *CozeLoopExporterConfig {
	if c == nil {
		return nil
	}
	return &CozeLoopExporterConfig{
		Endpoint:    c.Endpoint,
		ServiceName: c.ServiceName,
		APIKey:      c.APIKey,
	}
}

func (c *TLSExporterConfig) Clone() *TLSExporterConfig {
	if c == nil {
		return nil
	}
	return &TLSExporterConfig{
		Endpoint:    c.Endpoint,
		ServiceName: c.ServiceName,
		Region:      c.Region,
		TopicID:     c.TopicID,
		AccessKey:   c.AccessKey,
		SecretKey:   c.SecretKey,
	}
}

func (c *FileConfig) Clone() *FileConfig {
	if c == nil {
		return nil
	}
	return &FileConfig{
		Path: c.Path,
	}
}

func (c *StdoutConfig) Clone() *StdoutConfig {
	if c == nil {
		return nil
	}
	return &StdoutConfig{
		Enable: c.Enable,
	}
}
