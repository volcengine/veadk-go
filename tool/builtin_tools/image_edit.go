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
	"fmt"
	"sync"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/utils"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const (
	defaultImageEditResponseFormat = "url"
	defaultImageEditGuidanceScale  = 2.5
	defaultImageEditSeed           = -1

	ImageEditSuccessStatus = "success"
	ImageEditErrorStatus   = "error"
)

var imageEditToolDescription = `
	Edit images in batch according to prompts and optional generation settings.

	Args:
		params (list of EditImagesRequest)

	Per-item schema (EditImagesRequest)
	Required:
		- origin_image (str): source image URL or Base64 data URL.
		- prompt (str): image editing instruction.

	Optional:
		- image_name (str): output image name. Defaults to generated_image_<index>.
		- response_format (str): "url" (default) or "b64_json".
		- guidance_scale (float): prompt adherence strength. Defaults to 2.5.
		- watermark (bool): whether to add watermark. Defaults to true.
		- seed (int): random seed. Defaults to -1.

	Returns:
		{
			"status": "success",
			"success_list": [{"image_name": "edited", "url": "..."}],
			"error_list": []
		}
`

type ImageEditConfig struct {
	ModelName string
	APIKey    string
	BaseURL   string
}

type ImageEditToolRequest struct {
	Params []EditImagesRequest `json:"params"`
}

type EditImagesRequest struct {
	ImageName      string   `json:"image_name,omitempty"`
	OriginImage    string   `json:"origin_image"`
	Prompt         string   `json:"prompt"`
	ResponseFormat string   `json:"response_format,omitempty"`
	GuidanceScale  *float64 `json:"guidance_scale,omitempty"`
	Watermark      *bool    `json:"watermark,omitempty"`
	Seed           *int64   `json:"seed,omitempty"`
}

type ImageEditToolResult struct {
	SuccessList []*ImageEditResult `json:"success_list,omitempty"`
	ErrorList   []*ImageEditResult `json:"error_list,omitempty"`
	Status      string             `json:"status"`
}

type ImageEditResult struct {
	ImageName string `json:"image_name"`
	Url       string `json:"url,omitempty"`
	B64Json   string `json:"b64_json,omitempty"`
	Error     string `json:"error,omitempty"`
}

type ImageEditToolChannelMessage struct {
	Status string
	Result *ImageEditResult
}

func NewImageEditTool(config *ImageEditConfig) (tool.Tool, error) {
	if config == nil {
		config = &ImageEditConfig{}
	}
	if config.ModelName == "" {
		config.ModelName = utils.GetEnvWithDefault(common.MODEL_EDIT_NAME, configs.GetGlobalConfig().Model.Edit.Name, common.DEFAULT_MODEL_EDIT_NAME)
	}
	if config.APIKey == "" {
		config.APIKey = resolveImageEditAPIKey()
	}
	if config.BaseURL == "" {
		config.BaseURL = utils.GetEnvWithDefault(common.MODEL_EDIT_API_BASE, configs.GetGlobalConfig().Model.Edit.ApiBase, common.DEFAULT_MODEL_EDIT_API_BASE)
	}

	log.Debug("Initializing image edit tool", "model", config.ModelName, "base_url", config.BaseURL)

	handler := func(ctx tool.Context, toolRequest ImageEditToolRequest) (*ImageEditToolResult, error) {
		client := arkruntime.NewClientWithApiKey(
			config.APIKey,
			arkruntime.WithBaseUrl(config.BaseURL),
		)

		result := &ImageEditToolResult{}
		var wg sync.WaitGroup
		ch := make(chan *ImageEditToolChannelMessage)
		for i, task := range toolRequest.Params {
			wg.Add(1)
			go func(index int, req EditImagesRequest) {
				defer func() {
					wg.Done()
					if r := recover(); r != nil {
						log.Error("Image edit task panic", "recover", r, "prompt", req.Prompt)
						ch <- &ImageEditToolChannelMessage{
							Status: ImageEditErrorStatus,
							Result: newImageEditErrorResult(defaultImageEditName(index), fmt.Sprintf("task panic: %v", r)),
						}
					}
				}()

				imageName := imageEditName(req, index)
				if req.Prompt == "" {
					ch <- &ImageEditToolChannelMessage{Status: ImageEditErrorStatus, Result: newImageEditErrorResult(imageName, "prompt is required")}
					return
				}
				if req.OriginImage == "" {
					ch <- &ImageEditToolChannelMessage{Status: ImageEditErrorStatus, Result: newImageEditErrorResult(imageName, "origin_image is required")}
					return
				}

				resp, err := client.GenerateImages(ctx, buildImageEditModelRequest(config.ModelName, req))
				if err != nil {
					log.Error("Failed to edit image", "error", err)
					ch <- &ImageEditToolChannelMessage{Status: ImageEditErrorStatus, Result: newImageEditErrorResult(imageName, err.Error())}
					return
				}
				if resp.Error != nil {
					ch <- &ImageEditToolChannelMessage{Status: ImageEditErrorStatus, Result: newImageEditErrorResult(imageName, resp.Error.Message)}
					return
				}

				messages := imageEditResultsFromResponse(imageName, resp.Data)
				for _, message := range messages {
					ch <- message
				}
			}(i, task)
		}

		go func() {
			wg.Wait()
			close(ch)
		}()

		for res := range ch {
			switch res.Status {
			case ImageEditSuccessStatus:
				result.SuccessList = append(result.SuccessList, res.Result)
			case ImageEditErrorStatus:
				result.ErrorList = append(result.ErrorList, res.Result)
			}
		}

		if len(result.SuccessList) == 0 {
			result.Status = ImageEditErrorStatus
		} else {
			result.Status = ImageEditSuccessStatus
		}
		return result, nil
	}

	return functiontool.New(
		functiontool.Config{
			Name:        "image_edit",
			Description: imageEditToolDescription,
		},
		handler)
}

func buildImageEditModelRequest(modelName string, req EditImagesRequest) model.GenerateImagesRequest {
	responseFormat := req.ResponseFormat
	if responseFormat == "" {
		responseFormat = defaultImageEditResponseFormat
	}

	guidanceScale := defaultImageEditGuidanceScale
	if req.GuidanceScale != nil {
		guidanceScale = *req.GuidanceScale
	}

	watermark := true
	if req.Watermark != nil {
		watermark = *req.Watermark
	}

	seed := int64(defaultImageEditSeed)
	if req.Seed != nil {
		seed = *req.Seed
	}

	return model.GenerateImagesRequest{
		Model:          modelName,
		Prompt:         req.Prompt,
		Image:          req.OriginImage,
		ResponseFormat: &responseFormat,
		GuidanceScale:  &guidanceScale,
		Watermark:      &watermark,
		Seed:           &seed,
	}
}

func resolveImageEditAPIKey() string {
	if key := utils.GetEnvWithDefault(common.MODEL_EDIT_API_KEY, configs.GetGlobalConfig().Model.Edit.ApiKey); key != "" {
		return key
	}
	if key := utils.GetEnvWithDefault(common.MODEL_AGENT_API_KEY, configs.GetGlobalConfig().Model.Agent.ApiKey); key != "" {
		return key
	}
	return utils.Must(veauth.GetArkToken(common.DEFAULT_MODEL_REGION))
}

func imageEditResultsFromResponse(imageName string, images []*model.Image) []*ImageEditToolChannelMessage {
	if len(images) == 0 {
		return []*ImageEditToolChannelMessage{{
			Status: ImageEditErrorStatus,
			Result: newImageEditErrorResult(imageName, "no images returned"),
		}}
	}

	messages := make([]*ImageEditToolChannelMessage, 0, len(images))
	for i, image := range images {
		resultName := imageName
		if len(images) > 1 {
			resultName = fmt.Sprintf("%s_%d", imageName, i)
		}

		switch {
		case image == nil:
			messages = append(messages, &ImageEditToolChannelMessage{
				Status: ImageEditErrorStatus,
				Result: newImageEditErrorResult(resultName, "empty image result"),
			})
		case image.Url != nil:
			messages = append(messages, &ImageEditToolChannelMessage{
				Status: ImageEditSuccessStatus,
				Result: &ImageEditResult{ImageName: resultName, Url: *image.Url},
			})
		case image.B64Json != nil:
			messages = append(messages, &ImageEditToolChannelMessage{
				Status: ImageEditSuccessStatus,
				Result: &ImageEditResult{ImageName: resultName, B64Json: *image.B64Json},
			})
		default:
			messages = append(messages, &ImageEditToolChannelMessage{
				Status: ImageEditErrorStatus,
				Result: newImageEditErrorResult(resultName, "image url or b64_json is empty"),
			})
		}
	}
	return messages
}

func imageEditName(req EditImagesRequest, index int) string {
	if req.ImageName != "" {
		return req.ImageName
	}
	return defaultImageEditName(index)
}

func defaultImageEditName(index int) string {
	return fmt.Sprintf("generated_image_%d", index)
}

func newImageEditErrorResult(imageName, errMsg string) *ImageEditResult {
	return &ImageEditResult{ImageName: imageName, Error: errMsg}
}
