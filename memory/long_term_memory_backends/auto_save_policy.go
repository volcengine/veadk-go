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
	"encoding/json"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type MemoryEventType string

const (
	MemoryEventText                MemoryEventType = "text"
	MemoryEventThought             MemoryEventType = "thought"
	MemoryEventFunctionCall        MemoryEventType = "function_call"
	MemoryEventFunctionResponse    MemoryEventType = "function_response"
	MemoryEventToolCall            MemoryEventType = "tool_call"
	MemoryEventToolResponse        MemoryEventType = "tool_response"
	MemoryEventMedia               MemoryEventType = "media"
	MemoryEventExecutableCode      MemoryEventType = "executable_code"
	MemoryEventCodeExecutionResult MemoryEventType = "code_execution_result"
	MemoryEventError               MemoryEventType = "error"
)

type MemoryRole string

const (
	MemoryRoleUser      MemoryRole = "user"
	MemoryRoleAssistant MemoryRole = "assistant"
	MemoryRoleSystem    MemoryRole = "system"
)

type MemoryAutoSavePreset string

const (
	MemoryAutoSaveDefault MemoryAutoSavePreset = "default"
	MemoryAutoSaveAll     MemoryAutoSavePreset = "all"
	MemoryAutoSaveCustom  MemoryAutoSavePreset = "custom"
)

type MemoryAutoSavePolicy struct {
	Preset            MemoryAutoSavePreset
	IncludeRoles      []MemoryRole
	ExcludeRoles      []MemoryRole
	IncludeEventTypes []MemoryEventType
	ExcludeEventTypes []MemoryEventType
	TextOnly          bool
	IncludeThought    bool
	IncludeEmptyText  bool
}

type PolicyMemoryService interface {
	AddSessionToMemoryWithPolicy(context.Context, session.Session, *MemoryAutoSavePolicy) error
}

func resolveMemoryAutoSavePolicy(policy *MemoryAutoSavePolicy) MemoryAutoSavePolicy {
	if policy == nil {
		return baseMemoryAutoSavePolicy(MemoryAutoSaveDefault)
	}
	preset := policy.Preset
	if preset == "" {
		preset = MemoryAutoSaveCustom
	}
	base := baseMemoryAutoSavePolicy(preset)
	if policy.IncludeRoles != nil {
		base.IncludeRoles = policy.IncludeRoles
	}
	if policy.ExcludeRoles != nil {
		base.ExcludeRoles = policy.ExcludeRoles
	}
	if policy.IncludeEventTypes != nil {
		base.IncludeEventTypes = policy.IncludeEventTypes
	}
	if policy.ExcludeEventTypes != nil {
		base.ExcludeEventTypes = policy.ExcludeEventTypes
	}
	if policy.TextOnly {
		base.TextOnly = policy.TextOnly
	}
	if policy.IncludeThought {
		base.IncludeThought = true
	}
	if policy.IncludeEmptyText {
		base.IncludeEmptyText = true
	}
	return base
}

func baseMemoryAutoSavePolicy(preset MemoryAutoSavePreset) MemoryAutoSavePolicy {
	switch preset {
	case MemoryAutoSaveAll:
		return MemoryAutoSavePolicy{
			Preset:            MemoryAutoSaveAll,
			IncludeRoles:      []MemoryRole{MemoryRoleUser, MemoryRoleAssistant, MemoryRoleSystem},
			IncludeEventTypes: nil,
			TextOnly:          false,
			IncludeThought:    true,
			IncludeEmptyText:  true,
		}
	case MemoryAutoSaveCustom:
		return MemoryAutoSavePolicy{
			Preset:   MemoryAutoSaveCustom,
			TextOnly: true,
		}
	default:
		return MemoryAutoSavePolicy{
			Preset:            MemoryAutoSaveDefault,
			IncludeRoles:      []MemoryRole{MemoryRoleUser},
			IncludeEventTypes: []MemoryEventType{MemoryEventText},
			TextOnly:          true,
			IncludeThought:    false,
			IncludeEmptyText:  false,
		}
	}
}

func filterAndConvertEvents(s session.Session, policy *MemoryAutoSavePolicy) []string {
	resolved := resolveMemoryAutoSavePolicy(policy)
	var eventList []string
	for event := range s.Events().All() {
		content := convertEventToMemoryContent(event, resolved)
		if content == nil {
			continue
		}
		bytes, _ := json.Marshal(content)
		eventList = append(eventList, string(bytes))
	}
	return eventList
}

func convertEventToMemoryContent(event *session.Event, policy MemoryAutoSavePolicy) *genai.Content {
	if event == nil || event.Content == nil {
		return nil
	}
	role := normalizeMemoryRole(event)
	if !isRoleAllowed(role, policy) {
		return nil
	}
	eventTypes := inferEventTypes(event)
	if !hasAllowedEventType(eventTypes, policy) {
		return nil
	}
	parts := filterMemoryParts(event.Content.Parts, policy)
	if len(parts) == 0 && !policy.IncludeEmptyText {
		return nil
	}
	return &genai.Content{
		Role:  event.Content.Role,
		Parts: parts,
	}
}

func normalizeMemoryRole(event *session.Event) MemoryRole {
	if event.Author == genai.RoleUser || event.Content.Role == genai.RoleUser {
		return MemoryRoleUser
	}
	if event.Content.Role == "system" {
		return MemoryRoleSystem
	}
	return MemoryRoleAssistant
}

func isRoleAllowed(role MemoryRole, policy MemoryAutoSavePolicy) bool {
	if len(policy.IncludeRoles) > 0 && !containsRole(policy.IncludeRoles, role) {
		return false
	}
	return !containsRole(policy.ExcludeRoles, role)
}

func inferEventTypes(event *session.Event) map[MemoryEventType]bool {
	types := map[MemoryEventType]bool{}
	if event.Content != nil {
		for _, part := range event.Content.Parts {
			for eventType := range inferPartEventTypes(part) {
				types[eventType] = true
			}
		}
	}
	if event.ErrorCode != "" || event.ErrorMessage != "" {
		types[MemoryEventError] = true
	}
	return types
}

func inferPartEventTypes(part *genai.Part) map[MemoryEventType]bool {
	types := map[MemoryEventType]bool{}
	if part == nil {
		return types
	}
	if part.Text != "" {
		if part.Thought {
			types[MemoryEventThought] = true
		} else {
			types[MemoryEventText] = true
		}
	}
	if part.FunctionCall != nil {
		types[MemoryEventFunctionCall] = true
	}
	if part.FunctionResponse != nil {
		types[MemoryEventFunctionResponse] = true
	}
	if part.ToolCall != nil {
		types[MemoryEventToolCall] = true
	}
	if part.ToolResponse != nil {
		types[MemoryEventToolResponse] = true
	}
	if part.InlineData != nil || part.FileData != nil {
		types[MemoryEventMedia] = true
	}
	if part.ExecutableCode != nil {
		types[MemoryEventExecutableCode] = true
	}
	if part.CodeExecutionResult != nil {
		types[MemoryEventCodeExecutionResult] = true
	}
	return types
}

func filterMemoryParts(parts []*genai.Part, policy MemoryAutoSavePolicy) []*genai.Part {
	var finalParts []*genai.Part
	for _, part := range parts {
		partTypes := inferPartEventTypes(part)
		if !hasAllowedPartType(partTypes, policy) {
			continue
		}
		if partTypes[MemoryEventThought] && !policy.IncludeThought {
			if len(partTypes) == 1 {
				continue
			}
		}
		copied := sanitizeMemoryPart(part)
		if partTypes[MemoryEventThought] && !policy.IncludeThought {
			copied.Thought = false
			copied.ThoughtSignature = nil
		}
		finalParts = append(finalParts, copied)
	}
	return finalParts
}

func sanitizeMemoryPart(part *genai.Part) *genai.Part {
	if part == nil {
		return nil
	}
	copied := *part
	if part.InlineData != nil {
		inlineData := *part.InlineData
		if len(inlineData.Data) > 0 {
			size := len(inlineData.Data)
			inlineData.Data = nil
			if copied.PartMetadata == nil {
				copied.PartMetadata = map[string]any{}
			}
			copied.PartMetadata["data_omitted"] = true
			copied.PartMetadata["data_size"] = size
		}
		copied.InlineData = &inlineData
	}
	return &copied
}

func hasAllowedEventType(eventTypes map[MemoryEventType]bool, policy MemoryAutoSavePolicy) bool {
	if len(eventTypes) == 0 {
		return policy.IncludeEmptyText
	}
	for eventType := range eventTypes {
		if containsEventType(policy.ExcludeEventTypes, eventType) {
			delete(eventTypes, eventType)
		}
	}
	if len(eventTypes) == 0 {
		return false
	}
	if policy.IncludeEventTypes == nil {
		return true
	}
	for eventType := range eventTypes {
		if containsEventType(policy.IncludeEventTypes, eventType) {
			return true
		}
	}
	return false
}

func hasAllowedPartType(partTypes map[MemoryEventType]bool, policy MemoryAutoSavePolicy) bool {
	if len(partTypes) == 0 {
		return false
	}
	if policy.TextOnly && policy.IncludeEventTypes == nil {
		for eventType := range partTypes {
			if eventType != MemoryEventText && eventType != MemoryEventThought {
				return false
			}
		}
	}
	for eventType := range partTypes {
		if containsEventType(policy.ExcludeEventTypes, eventType) {
			return false
		}
	}
	if policy.IncludeEventTypes == nil {
		return true
	}
	for eventType := range partTypes {
		if containsEventType(policy.IncludeEventTypes, eventType) {
			return true
		}
	}
	return false
}

func containsRole(roles []MemoryRole, role MemoryRole) bool {
	for _, item := range roles {
		if item == role {
			return true
		}
	}
	return false
}

func containsEventType(eventTypes []MemoryEventType, eventType MemoryEventType) bool {
	for _, item := range eventTypes {
		if item == eventType {
			return true
		}
	}
	return false
}
