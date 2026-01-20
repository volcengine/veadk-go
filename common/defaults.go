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

package common

const (
	// Agent
	DEFAULT_MODEL_REGION         = "cn-beijing"
	DEFAULT_MODEL_AGENT_NAME     = "doubao-seed-1-6-250615"
	DEFAULT_MODEL_AGENT_PROVIDER = "openai"
	DEFAULT_MODEL_AGENT_API_BASE = "https://ark.cn-beijing.volces.com/api/v3/"

	// Image
	DEFAULT_MODEL_IMAGE_NAME     = "doubao-seedream-4-5-251128"
	DEFAULT_MODEL_IMAGE_API_BASE = "https://ark.cn-beijing.volces.com/api/v3/"

	// Video
	DEFAULT_MODEL_VIDEO_NAME     = "doubao-seedance-1-0-pro-250528"
	DEFAULT_MODEL_VIDEO_API_BASE = "https://ark.cn-beijing.volces.com/api/v3/"
)

// LOGGING
const (
	DEFAULT_LOGGING_LEVER = "info"
)

const (
	DEFAULT_LLMAGENT_NAME        = "veAgent"
	DEFAULT_LOOPAGENT_NAME       = "veLoopAgent"
	DEFAULT_PARALLELAGENT_NAME   = "veParallelAgent"
	DEFAULT_SEQUENTIALAGENT_NAME = "veSequentialAgent"
)

const DEFAULT_REGION = "cn-beijing"

const VEFAAS_IAM_CRIDENTIAL_PATH = "/var/run/secrets/iam/credential"

// VikingKnowledgeBase
const (
	DEFAULT_DATABASE_VIKING_PROJECT = "default"
	DEFAULT_DATABASE_VIKING_REGION  = "cn-beijing"
)

// TOS
const (
	DEFAULT_DATABASE_TOS_REGION = "cn-beijing"
	DEFAULT_DATABASE_TOS_BUCKET = "veadk-go-bucket"
)

// WebSearch
const (
	DEFAULT_WEB_SEARCH_REGION = "cn-beijing"
)

// AGENTKIT TOOL
const (
	DEFAULT_AGENTKIT_TOOL_REGION       = "cn-beijing"
	DEFAULT_AGENTKIT_TOOL_SERVICE_CODE = "agentkit"
)

const (
	DEFAULT_AGENTPILOT_API_URL = "https://prompt-pilot.cn-beijing.volces.com"
)
