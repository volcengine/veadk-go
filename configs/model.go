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

type CommonModelConfig struct {
	Name     string
	Provider string
	ApiBase  string
	ApiKey   string
}

type AgentConfig struct {
	CommonModelConfig
}

type EmbeddingModelConfig struct {
	CommonModelConfig
	Dim int
}

type ModelConfig struct {
	Agent     *AgentConfig
	Image     *CommonModelConfig
	Video     *CommonModelConfig
	Embedding *EmbeddingModelConfig
}

func (c *ModelConfig) MapEnvToConfig() {
	// Agent
	c.Agent.Name = utils.GetEnvWithDefault(common.MODEL_AGENT_NAME, common.DEFAULT_MODEL_AGENT_NAME)
	c.Agent.Provider = utils.GetEnvWithDefault(common.MODEL_AGENT_PROVIDER, common.DEFAULT_MODEL_AGENT_PROVIDER)
	c.Agent.ApiBase = utils.GetEnvWithDefault(common.MODEL_AGENT_API_BASE, common.DEFAULT_MODEL_AGENT_API_BASE)
	c.Agent.ApiKey = utils.GetEnvWithDefault(common.MODEL_AGENT_API_KEY)

	// Image
	c.Image.Name = utils.GetEnvWithDefault(common.MODEL_IMAGE_NAME, common.DEFAULT_MODEL_IMAGE_NAME)
	c.Image.ApiBase = utils.GetEnvWithDefault(common.MODEL_IMAGE_API_BASE, common.DEFAULT_MODEL_IMAGE_API_BASE)
	c.Image.ApiKey = utils.GetEnvWithDefault(common.MODEL_IMAGE_API_KEY)

	// Video
	c.Video.Name = utils.GetEnvWithDefault(common.MODEL_VIDEO_NAME, common.DEFAULT_MODEL_VIDEO_NAME)
	c.Video.ApiBase = utils.GetEnvWithDefault(common.MODEL_VIDEO_API_BASE, common.DEFAULT_MODEL_VIDEO_API_BASE)
	c.Video.ApiKey = utils.GetEnvWithDefault(common.MODEL_VIDEO_API_KEY)

	// Embedding
	c.Embedding.Name = utils.GetEnvWithDefault(common.MODEL_EMBEDDING_NAME, common.DEFAULT_MODEL_EMBEDDING_NAME)
	c.Embedding.ApiBase = utils.GetEnvWithDefault(common.MODEL_EMBEDDING_API_BASE, common.DEFAULT_MODEL_EMBEDDING_API_BASE)
	c.Embedding.ApiKey = utils.GetEnvWithDefault(common.MODEL_EMBEDDING_API_KEY)
	if dimStr := utils.GetEnvWithDefault(common.MODEL_EMBEDDING_DIM); dimStr != "" {
		c.Embedding.Dim, _ = strconv.Atoi(dimStr)
	}
	if c.Embedding.Dim == 0 {
		c.Embedding.Dim = common.DEFAULT_MODEL_EMBEDDING_DIM
	}
}
