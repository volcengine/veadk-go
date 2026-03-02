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

package short_term_memory_backends

import (
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/configs"
	"google.golang.org/adk/session/database"
)

func TestNewSqliteSTMBackend(t *testing.T) {
	tests := []struct {
		name    string
		config  *SqliteBackendConfig
		wantErr bool
	}{
		{
			name: "with db url",
			config: &SqliteBackendConfig{
				CommonDatabaseConfig: &configs.CommonDatabaseConfig{
					DBUrl: "/tmp/test_veadk.db",
				},
			},
		},
		{
			name: "empty db url uses in-memory",
			config: &SqliteBackendConfig{
				CommonDatabaseConfig: &configs.CommonDatabaseConfig{
					DBUrl: "",
				},
			},
		},
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		mockey.PatchConvey(tt.name, t, func() {
			mockey.Mock(database.NewSessionService).Return(&mockSessionServiceImpl{}, nil).Build()
			mockey.Mock(database.AutoMigrate).Return(nil).Build()
			t.Run(tt.name, func(t *testing.T) {
				sessionService, err := NewSqliteSTMBackend(tt.config)
				if tt.wantErr {
					assert.NotNil(t, err)
					assert.Nil(t, sessionService)
				} else {
					assert.Nil(t, err)
					assert.NotNil(t, sessionService)
				}
			})
		})
	}
}
