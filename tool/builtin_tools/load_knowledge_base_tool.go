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

package builtin_tools

import (
	"github.com/volcengine/veadk-go/v2/knowledgebase"
	"github.com/volcengine/veadk-go/v2/knowledgebase/ktypes"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

type QueryKnowledgeReq struct {
	Query string `json:"query" jsonschema:"The query for loading the knowledge base for this tools"`
}

type KnowledgeBaseResult struct {
	Result []ktypes.KnowledgeEntry `json:"result,omitempty"`
}

// LoadKnowledgeBaseTool
// Loads the knowledgebase tools from a backend.
// Args:
// query: The query to load the knowledgebase for.
// Returns:
// A list of knowledge base results.
func LoadKnowledgeBaseTool(knowledge *knowledgebase.KnowledgeBase) (tool.Tool, error) {
	handler := func(ctx agent.Context, req *QueryKnowledgeReq) (KnowledgeBaseResult, error) {
		result, err := knowledge.Backend.Search(req.Query)
		if err != nil {
			return KnowledgeBaseResult{}, err
		}
		return KnowledgeBaseResult{
			Result: result,
		}, nil

	}
	return functiontool.New(
		functiontool.Config{
			Name:        knowledge.Name,
			Description: knowledge.Description,
		},
		handler)
}
