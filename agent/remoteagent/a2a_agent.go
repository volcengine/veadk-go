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

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"google.golang.org/adk/v2/agent"
	remoteagentv2 "google.golang.org/adk/v2/agent/remoteagent/v2"
)

var (
	ErrBaseUrlInvalid = errors.New("BaseURL can't be empty")
	ErrNameInvalid    = errors.New("agent name can't be empty")
)

type Config struct {
	remoteagentv2.A2AConfig
	BaseUrl            string
	ApiKey             string
	AgentCardSource    string
	CardResolveOptions []agentcard.ResolveOption
	ClientFactory      *a2aclient.Factory
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
	c.AgentCardProvider = remoteagentv2.NewAgentCardProvider(agentCardSource, c.CardResolveOptions...)
	return c
}

func (c *Config) SetCardResolveOptions(cardResolveOptions []agentcard.ResolveOption) *Config {
	c.CardResolveOptions = cardResolveOptions
	if c.AgentCardSource != "" {
		c.AgentCardProvider = remoteagentv2.NewAgentCardProvider(c.AgentCardSource, c.CardResolveOptions...)
	}
	return c
}

func (c *Config) SetBeforeAgentCallbacks(beforeAgentCallbacks []agent.BeforeAgentCallback) *Config {
	c.BeforeAgentCallbacks = beforeAgentCallbacks
	return c
}

func (c *Config) SetBeforeRequestCallbacks(beforeRequestCallbacks []remoteagentv2.BeforeA2ARequestCallback) *Config {
	c.BeforeRequestCallbacks = beforeRequestCallbacks
	return c
}

func (c *Config) SetConverter(converter remoteagentv2.A2AEventConverter) *Config {
	c.Converter = converter
	return c
}

func (c *Config) SetAfterRequestCallbacks(afterRequestCallbacks []remoteagentv2.AfterA2ARequestCallback) *Config {
	c.AfterRequestCallbacks = afterRequestCallbacks
	return c
}

func (c *Config) SetAfterAgentCallbacks(afterAgentCallbacks []agent.AfterAgentCallback) *Config {
	c.AfterAgentCallbacks = afterAgentCallbacks
	return c
}

func (c *Config) SetClientFactory(clientFactory *a2aclient.Factory) *Config {
	c.ClientFactory = clientFactory
	c.ClientProvider = remoteagentv2.NewA2AClientProvider(clientFactory)
	return c
}

func (c *Config) SetMessageSendConfig(messageSendConfig *a2a.SendMessageConfig) *Config {
	c.MessageSendConfig = messageSendConfig
	return c
}

type AuthInterceptor struct {
	a2aclient.PassthroughInterceptor
	Token string
}

// Before implements a before request callback.
func (a *AuthInterceptor) Before(ctx context.Context, req *a2aclient.Request) (context.Context, any, error) {
	if req.ServiceParams == nil {
		req.ServiceParams = make(a2aclient.ServiceParams)
	}
	// Add the authorization header.
	req.ServiceParams["authorization"] = []string{"Bearer " + a.Token}
	return ctx, nil, nil
}

func NewVeRemoteAgent(config *Config) (agent.Agent, error) {
	if config.BaseUrl == "" {
		return nil, ErrBaseUrlInvalid
	}

	if config.AgentCard == nil {
		config.SetAgentCardSource(config.BaseUrl)
	}

	if config.Name == "" {
		return nil, ErrNameInvalid
	}

	if config.ApiKey != "" {
		resolveOptions := agentcard.WithRequestHeader("Authorization", fmt.Sprintf("Bearer %s", config.ApiKey))
		// Resolve an AgentCard
		config.SetCardResolveOptions(append(config.CardResolveOptions, resolveOptions))

		clientFactory := a2aclient.NewFactory(
			a2aclient.WithCallInterceptors(&AuthInterceptor{Token: config.ApiKey}),
		)
		config.SetClientFactory(clientFactory)
	}

	return remoteagentv2.NewA2A(config.A2AConfig)

}
