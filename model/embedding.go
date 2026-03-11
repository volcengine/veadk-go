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

import "context"

// EmbeddingRequest represents a request to generate text embeddings.
type EmbeddingRequest struct {
	Texts      []string
	Dimensions int
}

// EmbeddingResponse contains the embedding vectors and metadata.
type EmbeddingResponse struct {
	Embeddings [][]float32
	Model      string
	Usage      *EmbeddingUsage
}

// EmbeddingUsage contains token usage information for an embedding request.
type EmbeddingUsage struct {
	PromptTokens int
	TotalTokens  int
}

// Embedder is the interface for text embedding models.
type Embedder interface {
	// EmbedTexts converts text strings into embedding vectors.
	EmbedTexts(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
}
