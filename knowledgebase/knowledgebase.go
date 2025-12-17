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

package knowledgebase

import (
	"errors"
	"fmt"

	"github.com/volcengine/veadk-go/knowledgebase/backend/viking_knowledge_backend"
	_interface "github.com/volcengine/veadk-go/knowledgebase/interface"
	"github.com/volcengine/veadk-go/knowledgebase/ktypes"
)

var (
	ErrInvalidKnowledgeBackend       = errors.New("invalid knowledge backend type")
	ErrInvalidKnowledgeBackendConfig = errors.New("invalid knowledge backend config type")
)

const (
	DefaultName        = "knowledge_base"
	DefaultDescription = `This is a knowledge base. You can use it to answer questions. If any questions need you to look up the knowledge base, you should call knowledge_base function with a query.`
)

type KnowledgeBase struct {
	Name          string
	Description   string
	Backend       _interface.KnowledgeBackend
	BackendConfig any
}

func getKnowledgeBackend(backend string, backendConfig any) (_interface.KnowledgeBackend, error) {
	switch backend {
	case ktypes.VikingBackend:
		if config, ok := backendConfig.(*viking_knowledge_backend.Config); ok {
			return viking_knowledge_backend.NewVikingKnowledgeBackend(config)
		}
		return nil, ErrInvalidKnowledgeBackendConfig
	case ktypes.RedisBackend, ktypes.LocalBackend, ktypes.OpensearchBackend:
		return nil, fmt.Errorf("%w: %s", ErrInvalidKnowledgeBackend, backend)
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidKnowledgeBackend, backend)
	}
}

func NewKnowledgeBase(backend any, opts ...Option) (*KnowledgeBase, error) {
	var err error

	knowledge := &KnowledgeBase{}
	for _, o := range opts {
		o(knowledge)
	}
	if knowledge.Name == "" {
		knowledge.Name = DefaultName
	}
	if knowledge.Description == "" {
		knowledge.Description = DefaultDescription
	}
	switch b := backend.(type) {
	case _interface.KnowledgeBackend:
		knowledge.Backend = b
	case string:
		knowledge.Backend, err = getKnowledgeBackend(b, knowledge.BackendConfig)
		if err != nil {
			return nil, err
		}
	default:
		return nil, ErrInvalidKnowledgeBackend
	}
	return knowledge, nil
}
