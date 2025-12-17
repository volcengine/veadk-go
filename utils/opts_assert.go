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

package utils

import (
	"errors"
)

var (
	ErrOptsNil        = errors.New("opts is nil")
	ErrOptsInvalidKey = errors.New("the key not in opts")
	ErrOptsAssertType = errors.New("opts assert type error")
)

func ExtractOptsValue[T any](key string, opts ...map[string]any) (T, error) {
	var t T
	if len(opts) == 0 {
		return t, ErrOptsNil
	}
	for _, opt := range opts {
		val, ok := opt[key]
		if !ok {
			continue
		}
		res, ok := val.(T)
		if !ok {
			return t, ErrOptsAssertType
		} else {
			return res, nil
		}
	}
	return t, ErrOptsInvalidKey
}

func ExtractOptsValueWithDefault[T any](key string, defaultVal T, opts ...map[string]any) T {
	if len(opts) == 0 {
		return defaultVal
	}
	for _, opt := range opts {
		val, ok := opt[key]
		if !ok {
			continue
		}
		res, ok := val.(T)
		if !ok {
			return defaultVal
		} else {
			return res
		}
	}
	return defaultVal
}
