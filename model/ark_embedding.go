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

package model

import (
	"context"
	"fmt"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// ArkEmbeddingConfig holds configuration for the ARK embedding model.
type ArkEmbeddingConfig struct {
	APIKey     string
	AK         string // Volcengine Access Key (alternative to APIKey)
	SK         string // Volcengine Secret Key (alternative to APIKey)
	BaseURL    string
	Region     string
	Dimensions int
}

type arkEmbeddingModel struct {
	name   string
	config *ArkEmbeddingConfig
	client *arkruntime.Client
}

// NewArkEmbeddingModel creates an Embedder backed by the Volcengine ARK SDK.
// Auth is resolved as: APIKey > AK/SK. At least one must be provided.
func NewArkEmbeddingModel(ctx context.Context, modelName string, config *ArkEmbeddingConfig) (Embedder, error) {
	_ = ctx

	if config == nil {
		config = &ArkEmbeddingConfig{}
	}

	var opts []arkruntime.ConfigOption
	if config.BaseURL != "" {
		opts = append(opts, arkruntime.WithBaseUrl(config.BaseURL))
	}
	if config.Region != "" {
		opts = append(opts, arkruntime.WithRegion(config.Region))
	}

	var client *arkruntime.Client
	switch {
	case config.APIKey != "":
		client = arkruntime.NewClientWithApiKey(config.APIKey, opts...)
	case config.AK != "" && config.SK != "":
		client = arkruntime.NewClientWithAkSk(config.AK, config.SK, opts...)
	default:
		return nil, fmt.Errorf("ark embedding: API key or AK/SK pair is required")
	}

	return &arkEmbeddingModel{
		name:   modelName,
		config: config,
		client: client,
	}, nil
}

func (m *arkEmbeddingModel) EmbedTexts(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	if req == nil || len(req.Texts) == 0 {
		return nil, fmt.Errorf("ark embedding: at least one text input is required")
	}

	arkReq := arkmodel.EmbeddingRequestStrings{
		Input: req.Texts,
		Model: m.name,
	}

	dim := req.Dimensions
	if dim == 0 {
		dim = m.config.Dimensions
	}
	if dim > 0 {
		arkReq.Dimensions = dim
	}

	resp, err := m.client.CreateEmbeddings(ctx, arkReq)
	if err != nil {
		return nil, fmt.Errorf("ark embedding: %w", err)
	}

	embeddings := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		embeddings[i] = d.Embedding
	}

	return &EmbeddingResponse{
		Embeddings: embeddings,
		Model:      resp.Model,
		Usage: &EmbeddingUsage{
			PromptTokens: resp.Usage.PromptTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}, nil
}
