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
	"fmt"

	"github.com/volcengine/veadk-go/memory/long_term_memory_backends"
	"google.golang.org/adk/memory"
)

type LongTermBackendType string

const (
	BackendLongTermLocal  LongTermBackendType = "local"
	BackendLongTermViking LongTermBackendType = "viking"
)

// NewLongTermMemoryService creates a new long term memory service.
// If backend is empty, it will use the default backend.
// If config is nil, it will use the default config with backend
// If config is not nil, it will use the config.
func NewLongTermMemoryService(backend LongTermBackendType, config interface{}) (memory.Service, error) {
	if backend == "" {
		backend = BackendLongTermLocal
	}
	var (
		err           error
		memoryService memory.Service
	)
	switch backend {
	case BackendLongTermLocal:
		memoryService = memory.InMemoryService()
	case BackendLongTermViking:
		var vikingDBMemoryConfig *long_term_memory_backends.VikingDbMemoryConfig
		if config == nil {
			// use all default config in VikingDbMemoryBackend
			vikingDBMemoryConfig = &long_term_memory_backends.VikingDbMemoryConfig{}
		} else {
			var ok bool
			vikingDBMemoryConfig, ok = config.(*long_term_memory_backends.VikingDbMemoryConfig)
			if !ok {
				return nil, fmt.Errorf("viking backend requires *VikingDbMemoryConfig, got %T", config)
			}
		}
		memoryService, err = long_term_memory_backends.NewVikingDbMemoryBackend(vikingDBMemoryConfig)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported backend type: %s", backend)
	}

	return memoryService, nil
}
