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
	BackendLongTermMem0   LongTermBackendType = "mem0"
	DefaultTopK                               = 5
)

// NewLongTermMemoryService creates a new long term memory service.
// If backend is empty, it will use the default backend.
// If config is nil, it will use the default config with backend
// If config is not nil, it will use the config.
func NewLongTermMemoryService(backend LongTermBackendType, config interface{}, topK ...int) (memory.Service, error) {
	var memoryService memory.Service

	if backend == "" {
		backend = BackendLongTermLocal
	}
	if len(topK) == 0 {
		topK = append(topK, DefaultTopK)
	}

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
		vikingBackend, err := long_term_memory_backends.NewVikingDbMemoryBackend(vikingDBMemoryConfig)
		if err != nil {
			return nil, err
		}
		return long_term_memory_backends.LongTermMemoryFactory(vikingBackend, topK[0]), nil
	case BackendLongTermMem0:
		var mem0MemoryConfig *long_term_memory_backends.Mem0MemoryConfig
		if config == nil {
			mem0MemoryConfig = long_term_memory_backends.NewDefaultMem0MemoryConfig()
		} else {
			var ok bool
			mem0MemoryConfig, ok = config.(*long_term_memory_backends.Mem0MemoryConfig)
			if !ok {
				return nil, fmt.Errorf("mem0 backend requires *Mem0MemoryConfig, got %T", config)
			}
		}
		mem0Backend, err := long_term_memory_backends.NewMem0MemoryBackend(mem0MemoryConfig)
		if err != nil {
			return nil, err
		}
		return long_term_memory_backends.LongTermMemoryFactory(mem0Backend, topK[0]), nil
	default:
		return nil, fmt.Errorf("unsupported backend type: %s", backend)
	}

	return memoryService, nil
}
