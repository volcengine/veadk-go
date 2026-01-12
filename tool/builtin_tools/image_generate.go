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
	"strings"
	"sync"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/utils"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

var imageGenerateToolDescription = `
	Generate images with Seedream 4.0.

    Commit batch image generation requests via tasks.

    Args:
		Required: 
        - tasks (list of GenerateImagesRequest)
    Per-task schema (GenerateImagesRequest)
    ---------------
    Required:
        - task_type (str):
            One of:
              * "multi_image_to_group"   # 多图生组图
              * "single_image_to_group"  # 单图生组图
              * "text_to_group"          # 文生组图
              * "multi_image_to_single"  # 多图生单图
              * "single_image_to_single" # 单图生单图
              * "text_to_single"         # 文生单图
        - prompt (str)
            Text description of the desired image(s). 中文/English 均可。
            若要指定生成图片的数量，请在prompt中添加"生成N张图片"，其中N为具体的数字。
    Optional:
        - size (str)
            指定生成图像的大小，有两种用法（二选一，不可混用）：
            方式 1：分辨率级别
                可选值: "1K", "2K", "4K"
                模型会结合 prompt 中的语义推断合适的宽高比、长宽。
            方式 2：具体宽高值
                格式: "<宽度>x<高度>"，如 "2048x2048", "2384x1728"
                约束:
                    * 总像素数范围: [1024x1024, 4096x4096]
                    * 宽高比范围: [1/16, 16]
                推荐值:
                    - 1:1   → 2048x2048
                    - 4:3   → 2384x1728
                    - 3:4   → 1728x2304
                    - 16:9  → 2560x1440
                    - 9:16  → 1440x2560
                    - 3:2   → 2496x1664
                    - 2:3   → 1664x2496
                    - 21:9  → 3024x1296
            默认值: "2048x2048"
        - response_format (str)
            Return format: "url" (default, URL 24h 过期) | "b64_json".
        - watermark (bool)
            Add watermark. Default: true.
        - image (str | list[str])   # 仅“非文生图”需要。文生图请不要提供 image
            Reference image(s) as URL or Base64.
            * 生成“单图”的任务：传入 string（exactly 1 image）。
            * 生成“组图”的任务：传入 array（2–10 images）。
        - sequential_image_generation (str)
            控制是否生成“组图”。Default: "disabled".
            * 若要生成组图：必须设为 "auto"。
        - max_images (int)
            仅当生成组图时生效。控制模型能生成的最多张数，范围 [1, 15]， 不设置默认为15。
            注意这个参数不等于生成的图片数量，而是模型最多能生成的图片数量。
            在单图组图场景最多 14；多图组图场景需满足 (len(images)+max_images ≤ 15)。
    Model 行为说明（如何由参数推断模式）
    ---------------------------------
    1) 文生单图: 不提供 image 且 (S 未设置或 S="disabled") → 1 张图。
    2) 文生组图: 不提供 image 且 S="auto" → 组图，数量由 max_images 控制。
    3) 单图生单图: image=string 且 (S 未设置或 S="disabled") → 1 张图。
    4) 单图生组图: image=string 且 S="auto" → 组图，数量 ≤14。
    5) 多图生单图: image=array (2–10) 且 (S 未设置或 S="disabled") → 1 张图。
    6) 多图生组图: image=array (2–10) 且 S="auto" → 组图，需满足总数 ≤15。
    返回结果
    --------
        Dict with generation summary.
        Example:
        {
            "status": "success",
            "success_list": [
                {"image_name": "url"}
            ],
            "error_list": ["image_name"]
        }
    Notes:
    - 组图任务必须 sequential_image_generation="auto"。
    - 如果想要指定生成组图的数量，请在prompt里添加数量说明，例如："生成3张图片"。
    - size 推荐使用 2048x2048 或表格里的标准比例，确保生成质量。
`

type ImageGenerateConfig struct {
	ModelName string
	APIKey    string
	BaseURL   string
}

type ImageGenerateToolRequest struct {
	Tasks []GenerateImagesRequest `json:"tasks"`
}

type GenerateImagesRequest struct {
	TaskType                  string      `json:"task_type"`
	Prompt                    string      `json:"prompt"`
	Size                      string      `json:"size,omitempty"`
	ResponseFormat            string      `json:"response_format,omitempty"`
	Watermark                 *bool       `json:"watermark,omitempty"`
	Image                     interface{} `json:"image,omitempty"`
	SequentialImageGeneration string      `json:"sequential_image_generation,omitempty"`
	MaxImages                 int         `json:"max_images,omitempty"`
}

type ImageGenerateToolResult struct {
	SuccessList []*ImageResult `json:"success_list,omitempty"`
	ErrorList   []*ImageResult `json:"error_list,omitempty"`
	Status      string
}

type ImageResult struct {
	ImageName string `json:"image_name"`
	Url       string `json:"url"`
}

type ImageGenerateToolChanelMessage struct {
	Status       string
	ErrorMessage string
	Result       *ImageResult
}

const (
	ImageGenerateSuccessStatus = "success"
	ImageGenerateErrorStatus   = "error"
)

func NewImageGenerateTool(config *ImageGenerateConfig) (tool.Tool, error) {
	if config == nil {
		config = &ImageGenerateConfig{}
	}
	if config.ModelName == "" {
		config.ModelName = utils.GetEnvWithDefault(common.MODEL_IMAGE_NAME, configs.GetGlobalConfig().Model.Image.Name, common.DEFAULT_MODEL_IMAGE_NAME)
	}
	if config.APIKey == "" {
		config.APIKey = utils.GetEnvWithDefault(common.MODEL_IMAGE_API_KEY, configs.GetGlobalConfig().Model.Image.ApiKey, utils.Must(veauth.GetArkToken(common.DEFAULT_MODEL_REGION)))
	}
	if config.BaseURL == "" {
		config.BaseURL = utils.GetEnvWithDefault(common.MODEL_IMAGE_API_BASE, configs.GetGlobalConfig().Model.Image.ApiBase, common.DEFAULT_MODEL_IMAGE_API_BASE)
	}

	if strings.HasPrefix(config.ModelName, "doubao-seedream-3-0") {
		err := fmt.Errorf("image generation by Doubao Seedream 3.0 %s is deprecated. Please use Doubao Seedream 4.0 (e.g., doubao-seedream-4-0-251128) instead", config.ModelName)
		log.Error(err.Error())
		return nil, err
	}

	log.Debug("Initializing image generation tool", "model", config.ModelName, "base_url", config.BaseURL)

	handler := func(ctx tool.Context, toolRequest ImageGenerateToolRequest) (*ImageGenerateToolResult, error) {
		client := arkruntime.NewClientWithApiKey(
			config.APIKey,
			arkruntime.WithBaseUrl(config.BaseURL),
		)

		result := &ImageGenerateToolResult{}
		var wg sync.WaitGroup
		ch := make(chan *ImageGenerateToolChanelMessage)
		for i, task := range toolRequest.Tasks {
			wg.Add(1)
			go func(req GenerateImagesRequest) {
				defer func() {
					wg.Done()
					if r := recover(); r != nil {
						log.Error("Task goroutine panic", "recover", r, "prompt", req.Prompt)
						ch <- &ImageGenerateToolChanelMessage{
							Status:       ImageGenerateErrorStatus,
							ErrorMessage: fmt.Sprintf("task panic: %v", r),
							Result:       &ImageResult{ImageName: "task_image_panic"},
						}
					}
				}()

				modelReq := model.GenerateImagesRequest{
					Model:  config.ModelName,
					Prompt: req.Prompt,
				}
				if req.Size != "" {
					modelReq.Size = volcengine.String(req.Size)
				}
				if req.ResponseFormat != "" {
					modelReq.ResponseFormat = volcengine.String(req.ResponseFormat)
				}
				if req.Watermark != nil {
					modelReq.Watermark = req.Watermark
				}
				if req.Image != nil {
					modelReq.Image = req.Image
				}
				if req.SequentialImageGeneration != "" {
					seq := model.SequentialImageGeneration(req.SequentialImageGeneration)
					modelReq.SequentialImageGeneration = &seq
				}
				if req.MaxImages > 0 {
					modelReq.SequentialImageGenerationOptions = &model.SequentialImageGenerationOptions{
						MaxImages: volcengine.Int(req.MaxImages),
					}
				}

				resp, err := client.GenerateImages(ctx, modelReq)
				if err != nil {
					log.Error("Failed to generate images", "error", err)
					ch <- &ImageGenerateToolChanelMessage{Status: ImageGenerateErrorStatus, ErrorMessage: err.Error(), Result: &ImageResult{ImageName: "task_image"}}
					return
				}
				if resp.Error != nil {
					ch <- &ImageGenerateToolChanelMessage{Status: ImageGenerateErrorStatus, ErrorMessage: resp.Error.Message, Result: &ImageResult{ImageName: "task_image"}}
					return
				}

				for index, imageData := range resp.Data {
					imageName := fmt.Sprintf("task_%d_image_%d", i, index)
					imageUrl := ""
					if imageData.Url != nil {
						imageUrl = *imageData.Url
					} else if imageData.B64Json != nil {
						// 上传到tos
					} else {
						ch <- &ImageGenerateToolChanelMessage{Status: ImageGenerateSuccessStatus, ErrorMessage: "image url or b64_json is empty", Result: &ImageResult{ImageName: imageName}}
						continue
					}

					ch <- &ImageGenerateToolChanelMessage{Status: ImageGenerateSuccessStatus, Result: &ImageResult{ImageName: imageName, Url: imageUrl}}
				}
			}(task)
		}

		go func() {
			wg.Wait()
			close(ch)
		}()

		for res := range ch {
			if res.Status == ImageGenerateSuccessStatus {
				result.SuccessList = append(result.SuccessList, res.Result)
			}
			if res.Status == ImageGenerateErrorStatus {
				result.ErrorList = append(result.ErrorList, res.Result)
			}
		}

		if len(result.SuccessList) == 0 {
			result.Status = ImageGenerateErrorStatus
		} else {
			result.Status = ImageGenerateSuccessStatus
		}

		return result, nil
	}

	return functiontool.New(
		functiontool.Config{
			Name:        "image_generate",
			Description: imageGenerateToolDescription,
		},
		handler)

}
