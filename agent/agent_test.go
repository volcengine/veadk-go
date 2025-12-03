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

package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/memory"
	"github.com/volcengine/veadk-go/model"
	model2 "google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func TestRun(t *testing.T) {
	stm, err := memory.NewShortTermMemory()
	if err != nil {
		panic(err)
	}

	//_, err = stm.CreateSession(context.Background(), "test", "1", "3")
	//if err != nil {
	//	panic(err)
	//}
	get, err := stm.SessionService().Get(context.Background(), &session.GetRequest{
		AppName:   "test",
		UserID:    "1",
		SessionID: "3",
	})
	if err != nil {
		panic(err)
	}
	log.Info("sessionid", "id", get.Session.ID())

	llm, err := model.NewModel(context.Background(), "doubao-seed-1-6-250615", &model.ClientConfig{
		APIKey:  "9f0af936-9b89-4d3e-8ec7-242834dea341",
		BaseURL: "https://ark.cn-beijing.volces.com/api/v3/",
	})
	if err != nil {
		panic(err)
	}
	for got, err := range llm.GenerateContent(context.Background(), &model2.LLMRequest{
		Contents: []*genai.Content{
			{
				Parts: []*genai.Part{
					{
						Text: "你好",
					},
				},
				Role: "user",
			},
		},
	}, false) {
		if err != nil {
			panic(err)
		}
		for _, v := range got.Content.Parts {
			fmt.Println(v.Text)
		}

	}
}
