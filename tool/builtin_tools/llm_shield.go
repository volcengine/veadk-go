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

package builtin_tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/integrations/ve_sign"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

const (
	name           = "LLMShield"
	path           = "/v2/moderate"
	service        = "llmshield"
	action         = "Moderate"
	version        = "2025-08-31"
	defaultTimeout = 60
)

var (
	ErrInvalidAppID  = errors.New("LLM Shield App ID is not configured. Please configure it via environment TOOL_LLM_SHIELD_APP_ID")
	ErrInvalidApiKey = errors.New("LLM Shield auth invalid, Please configure it via environment TOOL_LLM_SHIELD_API_KEY or VOLCENGINE_ACCESS_KEY and VOLCENGINE_SECRET_KEY")
)

var CategoryMap = map[string]string{
	"101": "Model Misuse",
	"103": "Sensitive Information",
	"104": "Prompt Injection",
	"106": "General Topic Control",
	"107": "Computational Resource Consumption",
}

type LLMShieldClient struct {
	URL     string
	Region  string
	AppID   string
	APIKey  string
	Timeout int
}

type LLMShieldResult struct {
	ResponseMetadata *ResponseMetadata `json:"ResponseMetadata"`
	Result           *LLMShieldData    `json:"Result"`
}
type ResponseMetadata struct {
	RequestID string `json:"RequestId"`
	Service   string `json:"Service"`
	Region    string `json:"Region"`
	Action    string `json:"Action"`
	Version   string `json:"Version"`
}
type Matches struct {
	Word   string `json:"Word"`
	Source int    `json:"Source"`
}
type Risks struct {
	Category string     `json:"Category"`
	Label    string     `json:"Label"`
	Prob     float64    `json:"Prob,omitempty"`
	Matches  []*Matches `json:"Matches,omitempty"`
}
type RiskInfo struct {
	Risks []*Risks `json:"Risks"`
}

type ReplaceDetail struct {
	Replacement interface{} `json:"Replacement"`
}
type DecisionDetail struct {
	BlockDetail   map[string]interface{} `json:"BlockDetail"`
	ReplaceDetail *ReplaceDetail         `json:"ReplaceDetail"`
}
type Decision struct {
	DecisionType   int             `json:"DecisionType"`
	DecisionDetail *DecisionDetail `json:"DecisionDetail"`
	HitStrategyIDs []string        `json:"HitStrategyIDs"`
}
type PermitInfo struct {
	Permits interface{} `json:"Permits"`
}
type LLMShieldData struct {
	MsgID         string      `json:"MsgID"`
	RiskInfo      *RiskInfo   `json:"RiskInfo"`
	Decision      *Decision   `json:"Decision"`
	PermitInfo    *PermitInfo `json:"PermitInfo"`
	ContentInfo   string      `json:"ContentInfo"`
	Degraded      bool        `json:"Degraded"`
	DegradeReason string      `json:"DegradeReason"`
}

func NewLLMShieldClient(timeout int) (*LLMShieldClient, error) {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	region := utils.GetEnvWithDefault(common.TOOL_LLM_SHIELD_REGION, configs.GetGlobalConfig().Tool.LLMShield.Region, common.DEFAULT_LLM_SHIELD_REGION)
	shieldURL := utils.GetEnvWithDefault(common.TOOL_LLM_SHIELD_URL, configs.GetGlobalConfig().Tool.LLMShield.Url, fmt.Sprintf("https://%s.sdk.access.llm-shield.omini-shield.com", region))
	appId := utils.GetEnvWithDefault(common.TOOL_LLM_SHIELD_APP_ID, configs.GetGlobalConfig().Tool.LLMShield.AppId)
	if strings.TrimSpace(appId) == "" {
		return nil, ErrInvalidAppID
	}
	apiKey := utils.GetEnvWithDefault(common.TOOL_LLM_SHIELD_API_KEY, configs.GetGlobalConfig().Tool.LLMShield.ApiKey)
	if strings.TrimSpace(apiKey) == "" {
		ak, sk, _ := veauth.GetAuthInfo()
		if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
			return nil, ErrInvalidApiKey
		}
	}
	return &LLMShieldClient{
		AppID:   appId,
		APIKey:  apiKey,
		Region:  region,
		URL:     shieldURL,
		Timeout: timeout,
	}, nil
}

// requestLLMShield 向 LLM Shield 服务发送请求进行内容审核
func (p *LLMShieldClient) requestLLMShield(message string, role string) (string, error) {

	body := map[string]interface{}{
		"Message": map[string]interface{}{
			"Role":        role,
			"Content":     message,
			"ContentType": 1,
		},
		"Scene": p.AppID,
	}

	var respBody []byte

	if p.APIKey != "" {
		bodyBytes, _ := json.Marshal(body)
		req, err := http.NewRequest("POST", p.URL+path, strings.NewReader(string(bodyBytes)))
		if err != nil {
			return "", err
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", p.APIKey)

		q := req.URL.Query()
		q.Add("Action", action)
		q.Add("Version", version)
		req.URL.RawQuery = q.Encode()

		client := &http.Client{Timeout: time.Duration(p.Timeout) * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != 200 {
			return "", fmt.Errorf("LLM Shield HTTP error: %d", resp.StatusCode)
		}
		respBody, _ = io.ReadAll(resp.Body)

	} else {
		ak, sk, sessionToken := veauth.GetAuthInfo()
		if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
			return "", ErrInvalidApiKey
		}

		header := map[string]string{
			"X-Top-Service": service,
			"X-Top-Region":  p.Region,
		}

		if strings.TrimSpace(sessionToken) != "" {
			header["X-Session-Token"] = sessionToken
		}

		parsedURL, err := url.Parse(p.URL)
		if err != nil {
			return "", fmt.Errorf("invalid URL: %v", err)
		}

		veReq := ve_sign.VeRequest{
			AK:      ak,
			SK:      sk,
			Method:  "POST",
			Scheme:  parsedURL.Scheme,
			Host:    parsedURL.Host,
			Path:    path,
			Service: service,
			Region:  p.Region,
			Action:  action,
			Version: version,
			Body:    body,
			Timeout: uint(p.Timeout),
			Header:  header,
		}

		respBody, err = veReq.DoRequest()
		if err != nil {
			return "", fmt.Errorf("LLM Shield request failed: %v", err)
		}
	}
	// 解析响应
	var response LLMShieldResult

	if err := json.Unmarshal(respBody, &response); err != nil {
		return "", fmt.Errorf("JSON decode failed: %v", err)
	}

	if response.Result != nil && response.Result.Decision != nil {

		if response.Result.Decision.DecisionType == 2 && response.Result.RiskInfo != nil {
			risks := response.Result.RiskInfo.Risks
			if len(risks) > 0 {
				var riskReasons []string
				seen := make(map[string]bool)

				for _, risk := range risks {
					catName, ok := CategoryMap[risk.Category]
					if !ok {
						catName = fmt.Sprintf("Category %s", risk.Category)
					}
					if !seen[catName] {
						riskReasons = append(riskReasons, catName)
						seen[catName] = true
					}
				}

				reasonText := "security policy violation"
				if len(riskReasons) > 0 {
					reasonText = strings.Join(riskReasons, ", ")
				}

				return fmt.Sprintf("Your request has been blocked due to: %s. Please modify your input and try again.", reasonText), nil
			}
		}
	}

	return "", nil
}

// -------------------- Callbacks --------------------

func NewLLMShieldPlugins() (*plugin.Plugin, error) {
	c, err := NewLLMShieldClient(defaultTimeout)
	if err != nil {
		return nil, err
	}
	plugins, _ := plugin.New(plugin.Config{
		Name:                "llm_shield",
		BeforeModelCallback: c.beforeModelCallBack,
		AfterModelCallback:  c.afterModelCallBack,
		BeforeToolCallback:  c.beforeToolCallback,
		AfterToolCallback:   c.afterToolCallback,
	})
	return plugins, nil
}

// BeforeModelCallback 在发送给模型前检查用户输入
func (p *LLMShieldClient) beforeModelCallBack(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
	var lastUserMessage string
	var messageBuilder strings.Builder

	if len(req.Contents) > 0 {
		lastContent := req.Contents[len(req.Contents)-1]
		if lastContent.Role == "user" && len(lastContent.Parts) > 0 {
			for _, part := range lastContent.Parts {
				messageBuilder.WriteString(part.Text)
			}
		}
	}

	lastUserMessage = messageBuilder.String()
	if lastUserMessage == "" {
		return nil, nil
	}

	log.Printf("agent %s beforeModelCallBack lastUserMessage is %s\n", ctx.AgentName(), lastUserMessage)

	blockMsg, err := p.requestLLMShield(lastUserMessage, "user")
	if err != nil {
		log.Printf("LLM Shield beforeModelCallBack error: %v\n", err)
		return nil, nil
	}

	if blockMsg != "" {
		return &model.LLMResponse{
			Content: &genai.Content{
				Role: "model",
				Parts: []*genai.Part{
					{Text: blockMsg},
				},
			},
			Partial:      false,
			FinishReason: "STOP",
		}, nil
	}

	return nil, nil
}

// AfterModelCallback 在返回给用户前检查模型输出
func (p *LLMShieldClient) afterModelCallBack(ctx agent.CallbackContext, resp *model.LLMResponse, llmResponseError error) (*model.LLMResponse, error) {
	var lastModelMessage string
	if resp.Content.Role == "model" && len(resp.Content.Parts) > 0 {
		lastModelMessage = resp.Content.Parts[0].Text
	}

	if lastModelMessage == "" {
		return nil, nil
	}

	log.Printf("agent %s afterModelCallBack lastUserMessage is %s\n", ctx.AgentName(), lastModelMessage)

	blockMsg, err := p.requestLLMShield(lastModelMessage, "assistant")
	if err != nil {
		log.Printf("LLM Shield afterModelCallBack error: %v\n", err)
		return nil, nil
	}

	log.Printf("agent %s beforeModelCallBack blockMsg is %s\n", ctx.AgentName(), blockMsg)

	if blockMsg != "" {
		return &model.LLMResponse{
			Content: &genai.Content{
				Role: "model",
				Parts: []*genai.Part{
					{Text: blockMsg},
				},
			},
			Partial:      false,
			FinishReason: "STOP",
		}, nil
	}

	return nil, nil
}

// BeforeToolCallback 在工具执行前检查参数
func (p *LLMShieldClient) beforeToolCallback(ctx tool.Context, tool tool.Tool, args map[string]any) (map[string]any, error) {
	var argsList []string
	for k, v := range args {
		argsList = append(argsList, fmt.Sprintf("%s: %v", k, v))
	}
	message := strings.Join(argsList, "\n")

	blockMsg, err := p.requestLLMShield(message, "user")
	if err != nil {
		log.Printf("LLM Shield beforeToolCallback error: %v\n", err)
		return nil, nil
	}

	if blockMsg != "" {
		return map[string]interface{}{"result": blockMsg}, nil
	}
	return nil, nil
}

// AfterToolCallback 在工具执行后检查结果
func (p *LLMShieldClient) afterToolCallback(ctx tool.Context, tool tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
	if err != nil {
		return result, err
	}
	var message string

	for _, item := range result {
		message += fmt.Sprintf("%v\n", item)
	}

	blockMsg, err := p.requestLLMShield(message, "assistant")
	if err != nil {
		log.Printf("LLM Shield beforeToolCallback error: %v\n", err)
		return nil, nil
	}

	if blockMsg != "" {
		return map[string]interface{}{"result": blockMsg}, nil
	}
	return result, nil
}
