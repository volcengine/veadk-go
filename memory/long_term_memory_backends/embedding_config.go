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

package long_term_memory_backends

import (
	"context"

	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/model"
)

// EmbeddingConfig holds configuration for creating an embedding model.
type EmbeddingConfig struct {
	ModelName  string
	APIKey     string
	AK         string
	SK         string
	BaseURL    string
	Dimensions int
}

// NewDefaultEmbeddingConfig creates an EmbeddingConfig from global config / env vars.
func NewDefaultEmbeddingConfig() *EmbeddingConfig {
	cfg := configs.GetGlobalConfig()
	return &EmbeddingConfig{
		ModelName:  cfg.Model.Embedding.Name,
		APIKey:     cfg.Model.Embedding.ApiKey,
		BaseURL:    cfg.Model.Embedding.ApiBase,
		Dimensions: cfg.Model.Embedding.Dim,
	}
}

// CreateEmbedder creates a model.Embedder instance from this config.
func (c *EmbeddingConfig) CreateEmbedder(ctx context.Context) (model.Embedder, error) {
	return model.NewArkEmbeddingModel(ctx, c.ModelName, &model.ArkEmbeddingConfig{
		APIKey:     c.APIKey,
		AK:         c.AK,
		SK:         c.SK,
		BaseURL:    c.BaseURL,
		Dimensions: c.Dimensions,
	})
}
