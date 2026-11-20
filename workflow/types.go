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

// Package workflow provides a VeADK-friendly wrapper over ADK workflow.
//
// It keeps ADK's scheduler, persistence and resume semantics, while exposing a
// smaller API centered on business nodes.
package workflow

import (
	"time"

	adkwf "google.golang.org/adk/v2/workflow"
)

type (
	Node        = adkwf.Node
	Edge        = adkwf.Edge
	Route       = adkwf.Route
	NodeConfig  = adkwf.NodeConfig
	RetryConfig = adkwf.RetryConfig
)

var (
	Start   = adkwf.Start
	Default = adkwf.Default
)

type (
	StringRoute = adkwf.StringRoute
	IntRoute    = adkwf.IntRoute
	BoolRoute   = adkwf.BoolRoute
)

func MultiRoute[T comparable](values ...T) Route {
	return adkwf.MultiRoute[T](values)
}

func DefaultRetryConfig() *RetryConfig {
	return adkwf.DefaultRetryConfig()
}

type nodeOptions struct {
	cfg NodeConfig
}

type NodeOption func(*nodeOptions)

func WithNodeConfig(cfg NodeConfig) NodeOption {
	return func(o *nodeOptions) {
		o.cfg = cfg
	}
}

func WithRetry(cfg *RetryConfig) NodeOption {
	return func(o *nodeOptions) {
		o.cfg.RetryConfig = cfg
	}
}

func WithTimeout(timeout time.Duration) NodeOption {
	return func(o *nodeOptions) {
		o.cfg.Timeout = timeout
	}
}

func WithRerunOnResume(enabled bool) NodeOption {
	return func(o *nodeOptions) {
		o.cfg.RerunOnResume = &enabled
	}
}

func WithWaitForOutput(enabled bool) NodeOption {
	return func(o *nodeOptions) {
		o.cfg.WaitForOutput = &enabled
	}
}

func resolveNodeOptions(opts []NodeOption) nodeOptions {
	var out nodeOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}

type Field struct {
	Key   string
	Value any
}

func Any(key string, value any) Field {
	return Field{Key: key, Value: value}
}

func String(key, value string) Field {
	return Any(key, value)
}

func Int(key string, value int) Field {
	return Any(key, value)
}

func Bool(key string, value bool) Field {
	return Any(key, value)
}
