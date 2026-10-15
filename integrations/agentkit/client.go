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

package agentkit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/volcengine/veadk-go/v2/auth/veauth"
	"github.com/volcengine/veadk-go/v2/common"
	"github.com/volcengine/veadk-go/v2/configs"
	"github.com/volcengine/veadk-go/v2/integrations/ve_sign"
	"github.com/volcengine/veadk-go/v2/utils"
	"google.golang.org/adk/v2/agent"
)

const (
	DefaultScheme = "https"
	APIVersion    = "2025-10-30"
)

type DoRequestFunc func(ve_sign.VeRequest) ([]byte, error)

type Client struct {
	AK      string
	SK      string
	Header  map[string]string
	Service string
	Region  string
	Host    string
	Scheme  string

	DoRequest DoRequestFunc
}

type Option func(*Client)

func WithDoRequest(fn DoRequestFunc) Option {
	return func(c *Client) {
		if fn != nil {
			c.DoRequest = fn
		}
	}
}

func NewClient(ctx agent.Context, opts ...Option) (*Client, error) {
	ak, sk, header, err := Credentials(ctx)
	if err != nil {
		return nil, err
	}
	service, region, host, scheme := EndpointConfig()
	client := &Client{
		AK:        ak,
		SK:        sk,
		Header:    header,
		Service:   service,
		Region:    region,
		Host:      host,
		Scheme:    scheme,
		DoRequest: func(req ve_sign.VeRequest) ([]byte, error) { return req.DoRequest() },
	}
	for _, opt := range opts {
		opt(client)
	}
	return client, nil
}

func (c *Client) Do(action string, body map[string]any) (map[string]any, error) {
	raw, err := c.Raw(action, body)
	if err != nil {
		return nil, err
	}
	var response map[string]any
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("AgentKit %s unmarshal response %s failed: %w", action, raw, err)
	}
	return response, nil
}

func (c *Client) Raw(action string, body map[string]any) ([]byte, error) {
	return c.DoRequest(ve_sign.VeRequest{
		AK:      c.AK,
		SK:      c.SK,
		Method:  http.MethodPost,
		Scheme:  c.Scheme,
		Host:    c.Host,
		Path:    "/",
		Service: c.Service,
		Region:  c.Region,
		Action:  action,
		Version: APIVersion,
		Header:  c.Header,
		Body:    body,
	})
}

func EndpointConfig() (service, region, host, scheme string) {
	defaultRegion := common.DEFAULT_AGENTKIT_TOOL_REGION
	cloudProvider := strings.ToLower(strings.TrimSpace(os.Getenv("CLOUD_PROVIDER")))
	if cloudProvider == "byteplus" {
		defaultRegion = "ap-southeast-1"
	} else if envRegion := strings.TrimSpace(os.Getenv("REGION")); envRegion != "" {
		defaultRegion = envRegion
	}
	service = utils.GetEnvWithDefault(common.AGENTKIT_TOOL_SERVICE_CODE, configs.GetGlobalConfig().Tool.RunCode.ServiceCode, common.DEFAULT_AGENTKIT_TOOL_SERVICE_CODE)
	region = utils.GetEnvWithDefault(common.AGENTKIT_TOOL_REGION, configs.GetGlobalConfig().Tool.RunCode.Region, defaultRegion)
	defaultHost := "open.volcengineapi.com"
	if cloudProvider == "byteplus" {
		defaultHost = fmt.Sprintf("%s.%s.byteplusapi.com", service, region)
	}
	host = utils.GetEnvWithDefault(common.AGENTKIT_TOOL_HOST, configs.GetGlobalConfig().Tool.RunCode.Host, defaultHost)
	scheme = strings.ToLower(utils.GetEnvWithDefault(common.AGENTKIT_TOOL_SCHEME, DefaultScheme))
	if scheme != "http" && scheme != "https" {
		scheme = DefaultScheme
	}
	return service, region, host, scheme
}

func Credentials(ctx agent.Context) (ak, sk string, header map[string]string, err error) {
	if ctx != nil {
		ak = utils.GetStringFromToolContext(ctx, common.VOLCENGINE_ACCESS_KEY)
		sk = utils.GetStringFromToolContext(ctx, common.VOLCENGINE_SECRET_KEY)
	}
	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		ak = utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
		sk = utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	}
	if strings.TrimSpace(ak) != "" && strings.TrimSpace(sk) != "" {
		if token := strings.TrimSpace(os.Getenv(common.VOLCENGINE_SESSION_TOKEN)); token != "" {
			header = map[string]string{"X-Security-Token": token}
		} else if token := strings.TrimSpace(os.Getenv(common.VOLC_SESSIONTOKEN)); token != "" {
			header = map[string]string{"X-Security-Token": token}
		}
		return ak, sk, header, nil
	}
	iam, err := veauth.GetCredentialFromVeFaaSIAM()
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve AgentKit credentials: %w", err)
	}
	header = map[string]string{}
	if strings.TrimSpace(iam.SessionToken) != "" {
		header["X-Security-Token"] = iam.SessionToken
	}
	return iam.AccessKeyID, iam.SecretAccessKey, header, nil
}
