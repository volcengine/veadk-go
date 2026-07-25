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

package viking_knowledge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/integrations/ve_viking"
)

func TestBuildDocFilterQuery(t *testing.T) {
	client := &Client{}

	t.Run("single scalar condition", func(t *testing.T) {
		filter := client.buildDocFilterQuery(map[string]any{
			"doc_id": "tos_doc_id_123",
		})

		assert.Equal(t, map[string]any{
			"op":    "must",
			"field": "doc_id",
			"conds": []any{"tos_doc_id_123"},
		}, filter)
	})

	t.Run("single typed string slice condition", func(t *testing.T) {
		filter := client.buildDocFilterQuery(map[string]any{
			"metric_field": []string{"a1_a1_dcnt", "a1_a1_rate", "a1_a2_dcnt", "a1_a2_rate"},
		})

		assert.Equal(t, map[string]any{
			"op":    "must",
			"field": "metric_field",
			"conds": []any{"a1_a1_dcnt", "a1_a1_rate", "a1_a2_dcnt", "a1_a2_rate"},
		}, filter)
	})

	t.Run("single json array condition", func(t *testing.T) {
		filter := client.buildDocFilterQuery(map[string]any{
			"doc_id": []any{"tos_doc_id_123", "tos_doc_id_456"},
		})

		assert.Equal(t, map[string]any{
			"op":    "must",
			"field": "doc_id",
			"conds": []any{"tos_doc_id_123", "tos_doc_id_456"},
		}, filter)
	})

	t.Run("single numeric condition", func(t *testing.T) {
		filter := client.buildDocFilterQuery(map[string]any{
			"type": 1,
		})

		assert.Equal(t, map[string]any{
			"op":    "must",
			"field": "type",
			"conds": []any{1},
		}, filter)
	})

	t.Run("single typed numeric slice condition", func(t *testing.T) {
		filter := client.buildDocFilterQuery(map[string]any{
			"type": []int{1, 2},
		})

		assert.Equal(t, map[string]any{
			"op":    "must",
			"field": "type",
			"conds": []any{1, 2},
		}, filter)
	})

	t.Run("multiple conditions", func(t *testing.T) {
		filter := client.buildDocFilterQuery(map[string]any{
			"type":   1,
			"doc_id": []string{"tos_doc_id_123", "tos_doc_id_456"},
		})

		assert.Equal(t, "and", filter["op"])
		conds, ok := filter["conds"].([]map[string]any)
		assert.True(t, ok)
		assert.Len(t, conds, 2)
		assert.Contains(t, conds, map[string]any{
			"op":    "must",
			"field": "type",
			"conds": []any{1},
		})
		assert.Contains(t, conds, map[string]any{
			"op":    "must",
			"field": "doc_id",
			"conds": []any{"tos_doc_id_123", "tos_doc_id_456"},
		})
	})
}

func TestGenerateSearchKnowledgeReqParamsWithEmptyMetadata(t *testing.T) {
	client := &Client{ClientConfig: &ve_viking.ClientConfig{}}

	req := client.generateSearchKnowledgeReqParams("query", 3, map[string]any{}, true, 0)

	assert.Nil(t, req.QueryParam)
}
