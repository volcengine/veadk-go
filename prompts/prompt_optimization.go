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

package prompts

import (
	"bytes"
	"text/template"
)

const prompt = `Please help me to optimize the following agent prompt:
{{.OriginalPrompt}}


The following information is your references：
<agent_info>
name: {{.Agent.Name}}
model: {{.Agent.Model}}
description: {{.Agent.Description}}
</agent_info>

<agent_tools_info>
{{range .Tools}}
<tool>
name: {{.Name}}
type: {{.Type}}
description: {{.Description}}
arguments: {{.Arguments}}
</tool>
{{end}}
</agent_tools_info>

Please note that in your optimized prompt:
- the above referenced information is not necessary. For example, the tools list of agent is not necessary in the optimized prompt, because it maybe too long. You should use the tool information to optimize the original prompt rather than simply add tool list in prompt.
- The max length of optimized prompt should be less 4096 tokens.
`

const promptWithFeedback = `After you optimization, my current prompt is:
{{.Prompt}}

I did some evaluations with the optimized prompt, and the feedback is: {{.Feedback}}

Please continue to optimize the prompt based on the feedback.
`

type AgentInfo struct {
	Name        string
	Model       string
	Description string
	Instruction string
	Tools       []*ToolInfo
}

// ToolInfo 结构体定义
type ToolInfo struct {
	Name        string
	Type        string
	Description string
	Arguments   string
}

func RenderPromptFeedbackWithTemplate(agent *AgentInfo, feedback string) (string, error) {
	tmpl, err := template.New("promptWithFeedback").Parse(promptWithFeedback)
	if err != nil {
		return "", err
	}

	context := map[string]interface{}{
		"Prompt":   agent.Instruction,
		"Feedback": feedback,
	}

	// 执行模板渲染
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, context)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func RenderPromptWithTemplate(agent *AgentInfo) (string, error) {
	// 解析模板
	tmpl, err := template.New("prompt").Parse(prompt)
	if err != nil {
		return "", err
	}

	// 准备上下文数据
	context := map[string]interface{}{
		"OriginalPrompt": agent.Instruction,
		"Agent": map[string]string{
			"Name":        agent.Name,
			"Model":       agent.Model,
			"Description": agent.Description,
		},
		"Tools": agent.Tools,
	}

	// 执行模板渲染
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, context)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
