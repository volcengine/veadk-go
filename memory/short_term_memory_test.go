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

package memory

import (
	"os"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/memory/short_term_memory_backends"
	"google.golang.org/adk/session"
)

type mockSessionServiceImpl struct {
	session.Service
}

// mockSessionImpl 是 session.Session 接口的 mock 实现
type mockSessionImpl struct {
	session.Session
}

func TestNewShortTermMemory(t *testing.T) {
	tests := []struct {
		name       string
		config     *configs.DatabaseConfig
		wantConfig *configs.DatabaseConfig
		wantErr    bool
	}{
		{
			name: "has user config",
			config: &configs.DatabaseConfig{
				ShortTermMemoryBackend: "local",
			},
			wantConfig: &configs.DatabaseConfig{
				ShortTermMemoryBackend: "local",
			},
			wantErr: false,
		},
		{
			name: "default config",
			wantConfig: &configs.DatabaseConfig{
				ShortTermMemoryBackend: "postgresql",
			},
			wantErr: false,
		},
		{
			name: "unsupported backend",
			config: &configs.DatabaseConfig{
				ShortTermMemoryBackend: "test",
			},
			wantErr: true,
		},
	}

	mockey.Mock(os.Getwd).Return("../test", nil).Build()
	configs.SetupVeADKConfig()
	for _, tt := range tests {
		mockey.PatchConvey(tt.name, t, func() {
			mockey.Mock((*short_term_memory_backends.PostgreSqlSTMBackend).SessionService).Return(mockSessionServiceImpl{}, nil).Build()
			t.Run(tt.name, func(t *testing.T) {
				got, err := NewShortTermMemory(tt.config)
				assert.True(t, tt.wantErr == (err != nil))
				if err == nil {
					assert.Equal(t, tt.wantConfig.ShortTermMemoryBackend, got.config.ShortTermMemoryBackend)
					assert.NotNil(t, got.sessionService)
				}
			})
		})
	}
}
