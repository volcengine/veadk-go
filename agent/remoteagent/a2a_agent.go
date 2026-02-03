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

package remoteagent

import (
	"context"
	"errors"
	"fmt"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2aclient"
	"github.com/a2aproject/a2a-go/a2aclient/agentcard"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/remoteagent"
)

var (
	ErrBaseUrlInvalid = errors.New("BaseURL can't be empty")
	ErrNameInvalid    = errors.New("agent name can't be empty")
)

type Config struct {
	remoteagent.A2AConfig
	BaseUrl string
	ApiKey  string
}

func NewDefaultConfig() *Config {
	return &Config{}
}

func (c *Config) SetBaseUrl(url string) *Config {
	c.BaseUrl = url
	return c
}

func (c *Config) SetApiKey(apiKey string) *Config {
	c.ApiKey = apiKey
	return c
}

func (c *Config) SetName(name string) *Config {
	c.Name = name
	return c
}

func (c *Config) SetDescription(description string) *Config {
	c.Description = description
	return c
}

func (c *Config) SetAgentCard(agentCard *a2a.AgentCard) *Config {
	c.AgentCard = agentCard
	return c
}

func (c *Config) SetAgentCardSource(agentCardSource string) *Config {
	c.AgentCardSource = agentCardSource
	return c
}

func (c *Config) SetCardResolveOptions(cardResolveOptions []agentcard.ResolveOption) *Config {
	c.CardResolveOptions = cardResolveOptions
	return c
}

func (c *Config) SetBeforeAgentCallbacks(beforeAgentCallbacks []agent.BeforeAgentCallback) *Config {
	c.BeforeAgentCallbacks = beforeAgentCallbacks
	return c
}

func (c *Config) SetBeforeRequestCallbacks(beforeRequestCallbacks []remoteagent.BeforeA2ARequestCallback) *Config {
	c.BeforeRequestCallbacks = beforeRequestCallbacks
	return c
}

func (c *Config) SetConverter(converter remoteagent.A2AEventConverter) *Config {
	c.Converter = converter
	return c
}

func (c *Config) SetAfterRequestCallbacks(afterRequestCallbacks []remoteagent.AfterA2ARequestCallback) *Config {
	c.AfterRequestCallbacks = afterRequestCallbacks
	return c
}

func (c *Config) SetAfterAgentCallbacks(afterAgentCallbacks []agent.AfterAgentCallback) *Config {
	c.AfterAgentCallbacks = afterAgentCallbacks
	return c
}

func (c *Config) SetClientFactory(clientFactory *a2aclient.Factory) *Config {
	c.ClientFactory = clientFactory
	return c
}

func (c *Config) SetMessageSendConfig(messageSendConfig *a2a.MessageSendConfig) *Config {
	c.MessageSendConfig = messageSendConfig
	return c
}

type AuthInterceptor struct {
	a2aclient.PassthroughInterceptor
	Token string
}

// Before implements a before request callback.
func (a *AuthInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, error) {
	if req.Meta == nil {
		req.Meta = make(a2aclient.CallMeta)
	}
	// Add the authorization header.
	req.Meta["Authorization"] = []string{"Bearer " + a.Token}
	return ctx, nil
}

func NewVeRemoteAgent(config *Config) (agent.Agent, error) {
	if config.BaseUrl == "" {
		return nil, ErrBaseUrlInvalid
	}

	config.SetAgentCardSource(config.BaseUrl)

	if config.Name == "" {
		return nil, ErrNameInvalid
	}

	ctx := context.Background()
	if config.ApiKey != "" {
		resolveOptions := agentcard.WithRequestHeader("Authorization", fmt.Sprintf("Bearer %s", config.ApiKey))
		// Resolve an AgentCard
		card, err := agentcard.DefaultResolver.Resolve(ctx, config.BaseUrl, resolveOptions)
		if err != nil {
			return nil, fmt.Errorf("veadk: failed to resolve veadk card: %w", err)
		}

		card.URL = config.BaseUrl

		config.SetAgentCard(card)

		clientFactory := a2aclient.NewFactory(
			a2aclient.WithInterceptors(&AuthInterceptor{Token: config.ApiKey}),
		)
		config.SetClientFactory(clientFactory)
	}

	return remoteagent.NewA2A(config.A2AConfig)

}
