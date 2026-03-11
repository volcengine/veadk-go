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

func TestNewMysqlSTMBackend(t *testing.T) {
	tests := []struct {
		name      string
		config    *MysqlBackendConfig
		wantDBurl string
		wantErr   bool
	}{
		{
			name: "no db url",
			config: &MysqlBackendConfig{
				CommonDatabaseConfig: &configs.CommonDatabaseConfig{
					User:     "test@",
					Password: "test@",
					Host:     "127.0.0.1",
					Port:     "3306",
					Database: "test_veadk",
					DBUrl:    "",
				},
			},
			wantDBurl: "test%40:test%40@tcp(127.0.0.1:3306)/test_veadk?charset=utf8mb4&parseTime=True&loc=Local",
		},
		{
			name: "has db url",
			config: &MysqlBackendConfig{
				CommonDatabaseConfig: &configs.CommonDatabaseConfig{
					DBUrl: "root:password@tcp(127.0.0.1:3306)/test_veadk?charset=utf8mb4&parseTime=True&loc=Local",
				},
			},
			wantDBurl: "root:password@tcp(127.0.0.1:3306)/test_veadk?charset=utf8mb4&parseTime=True&loc=Local",
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
				sessionService, err := NewMysqlSTMBackend(tt.config)
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
