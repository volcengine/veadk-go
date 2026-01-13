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
	"context"
	"fmt"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"
)

func LoadLongMemoryTool() (tool.Tool, error) {
	return functiontool.New(
		functiontool.Config{
			Name:        "search_past_conversations",
			Description: "Searches past conversations for relevant information.",
		},
		memorySearchToolFunc,
	)
}

type Args struct {
	Query string `json:"query" jsonschema:"The query to search for in the memory."`
}

type Result struct {
	Results []string `json:"results"`
}

func memorySearchToolFunc(tctx tool.Context, args Args) (Result, error) {

	searchResults, err := tctx.SearchMemory(context.Background(), args.Query)
	if err != nil {
		return Result{}, fmt.Errorf("failed memory search")
	}

	var results []string
	for _, res := range searchResults.Memories {
		if res.Content != nil {
			results = append(results, textParts(res.Content)...)
		}
	}
	return Result{Results: results}, nil
}

func textParts(Content *genai.Content) []string {
	var texts []string
	for _, part := range Content.Parts {
		texts = append(texts, part.Text)
	}
	return texts
}
