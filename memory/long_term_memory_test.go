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
	"errors"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/memory/long_term_memory_backends"
	"google.golang.org/adk/memory"
)

type mockMemoryServiceImpl struct {
	memory.Service
}

func TestNewLongTermMemory(t *testing.T) {
	tests := []struct {
		name        string
		backend     LongTermBackendType
		config      interface{}
		setupMock   func()
		wantErr     bool
		expectedErr string
	}{
		{
			name:    "default config (local)",
			backend: "",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "local backend explicit",
			backend: BackendLongTermLocal,
			config:  nil,
			wantErr: false,
		},
		{
			name:    "viking backend default config",
			backend: BackendLongTermViking,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewVikingDbMemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "viking backend valid config",
			backend: BackendLongTermViking,
			config:  &long_term_memory_backends.VikingDbMemoryConfig{},
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewVikingDbMemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "viking backend invalid config type",
			backend: BackendLongTermViking,
			config:  "invalid",
			wantErr: true,
		},
		{
			name:    "viking backend constructor error",
			backend: BackendLongTermViking,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewVikingDbMemoryBackend).Return(nil, errors.New("init error")).Build()
			},
			wantErr: true,
		},
		{
			name:    "mem0 backend default config",
			backend: BackendLongTermMem0,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewMem0MemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "mem0 backend valid config",
			backend: BackendLongTermMem0,
			config:  &long_term_memory_backends.Mem0MemoryConfig{},
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewMem0MemoryBackend).Return(nil, nil).Build()
				mockey.Mock(long_term_memory_backends.LongTermMemoryFactory).Return(&mockMemoryServiceImpl{}).Build()
			},
			wantErr: false,
		},
		{
			name:    "mem0 backend invalid config type",
			backend: BackendLongTermMem0,
			config:  "invalid",
			wantErr: true,
		},
		{
			name:    "mem0 backend constructor error",
			backend: BackendLongTermMem0,
			config:  nil,
			setupMock: func() {
				mockey.Mock(long_term_memory_backends.NewMem0MemoryBackend).Return(nil, errors.New("init error")).Build()
			},
			wantErr: true,
		},
		{
			name:    "unsupported backend",
			backend: "test",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		mockey.PatchConvey(tt.name, t, func() {
			if tt.setupMock != nil {
				tt.setupMock()
			}
			memoryService, err := NewLongTermMemoryService(tt.backend, tt.config)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, memoryService)
			}
		})
	}
}
