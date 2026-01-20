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

package ve_prompt_pilot

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type Usage struct {
	TotalTokens int `json:"total_tokens"`
}

type GeneratePromptChunk struct {
	Content string `json:"content,omitempty"`
	Usage   *Usage `json:"usage,omitempty"`
	Error   string `json:"error,omitempty"`
}

type GeneratePromptStreamResponseChunk struct {
	Event string               `json:"event"`
	Data  *GeneratePromptChunk `json:"data,omitempty"`
}

var (
	dataMessageRegex = regexp.MustCompile(`^data: "(?P<data>.*)"$`)
	dataGenericRegex = regexp.MustCompile(`^data: (?P<data>.*)$`)
	eventRegex       = regexp.MustCompile(`^event: (?P<event>[^:]+)$`)
)

func parseEventStreamLine(line string, promptChunk *GeneratePromptStreamResponseChunk) *GeneratePromptStreamResponseChunk {
	if promptChunk != nil && promptChunk.Event == "message" && promptChunk.Data.Content == "" {
		if strings.HasPrefix(line, "data: ") {
			match := dataMessageRegex.FindStringSubmatch(line)
			if len(match) > 1 {
				content := match[1]
				var decodedContent string
				jsonStr := fmt.Sprintf(`"%s"`, content)
				if err := json.Unmarshal([]byte(jsonStr), &decodedContent); err == nil {
					promptChunk.Data.Content = decodedContent
					return promptChunk
				}
			}
		}
	} else if promptChunk != nil && promptChunk.Event == "usage" && promptChunk.Data.Usage == nil {
		if strings.HasPrefix(line, "data: ") {
			match := dataGenericRegex.FindStringSubmatch(line)
			if len(match) > 1 {
				dataStr := match[1]
				var usage *Usage
				// usage 是 JSON 对象
				if err := json.Unmarshal([]byte(dataStr), &usage); err == nil {
					promptChunk.Data.Usage = usage
					return promptChunk
				}
			}
		}
	} else if promptChunk != nil && promptChunk.Event == "error" && promptChunk.Data.Error == "" {
		if strings.HasPrefix(line, "data: ") {
			match := dataGenericRegex.FindStringSubmatch(line)
			if len(match) > 1 {
				// error 直接作为字符串处理
				promptChunk.Data.Error = match[1]
				return promptChunk
			}
		}
	} else {
		// 检查是否是新事件的开始
		if strings.HasPrefix(line, "event:") {
			match := eventRegex.FindStringSubmatch(line)
			if len(match) > 1 {
				return &GeneratePromptStreamResponseChunk{
					Event: strings.TrimSpace(match[1]),
					Data:  &GeneratePromptChunk{},
				}
			}
		}
	}
	return nil
}
