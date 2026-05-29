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
	"testing"

	"github.com/bytedance/mockey"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/common"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

func TestNewLasToolsetMissingURL(t *testing.T) {
	t.Setenv(common.TOOL_LAS_URL, "")

	toolset, err := NewLasToolset()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), common.TOOL_LAS_URL)
	assert.Nil(t, toolset)
}

func TestNewLasToolsetSSEUnsupported(t *testing.T) {
	t.Setenv(common.TOOL_LAS_URL, "https://las.example.com/sse")

	toolset, err := NewLasToolset()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "暂不支持")
	assert.Nil(t, toolset)
}

func TestNewLasToolsetStreamableHTTP(t *testing.T) {
	t.Setenv(common.TOOL_LAS_URL, "https://las.example.com/mcp")
	t.Setenv(common.TOOL_LAS_API_KEY, "test-api-key")

	mockey.PatchConvey("streamable http transport", t, func() {
		mockey.Mock(mcptoolset.New).To(func(cfg mcptoolset.Config) (tool.Toolset, error) {
			transport, ok := cfg.Transport.(*mcp.StreamableClientTransport)
			require.True(t, ok)
			assert.Equal(t, "https://las.example.com/mcp", transport.Endpoint)
			assert.NotNil(t, transport.HTTPClient)
			return nil, nil
		}).Build()

		toolset, err := NewLasToolset()

		assert.NoError(t, err)
		assert.Nil(t, toolset)
	})
}
