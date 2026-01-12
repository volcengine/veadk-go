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
	"time"

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

var videoGenerateToolDescription = `
	Generate videos in **batch** from text prompts, optionally guided by a first/last frame,
    and fine-tuned via *model text commands* (a.k.a. parameters appended to the prompt).

    This API creates video-generation tasks. Each item in params describes a single video.
    The function submits all items in one call and returns task metadata for tracking.

    Args:
        params (list[dict]):
            A list of video generation requests. Each item supports the fields below.
        batch_size (int):
            The number of videos to generate in a batch. Defaults to 10.

            Required per item:
                - video_name (str):
                    Name/identifier of the output video file.

                - prompt (str):
                    Text describing the video to generate. Supports zh/EN.
                    You may append **model text commands** after the prompt to control resolution,
                    aspect ratio, duration, fps, watermark, seed, camera lock, etc.
                    Format: ... --rs <resolution> --rt <ratio> --dur <seconds> --fps <fps> --wm <bool> --seed <int> --cf <bool>
                    Example:
                        "小猫骑着滑板穿过公园。 --rs 720p --rt 16:9 --dur 5 --fps 24 --wm true --seed 11 --cf false"

            Optional per item:
                - first_frame (str | None):
                    URL or Base64 string (data URL) for the **first frame** (role = first_frame).
                    Use when you want the clip to start from a specific video.

                - last_frame (str | None):
                    URL or Base64 string (data URL) for the **last frame** (role = last_frame).
                    Use when you want the clip to end on a specific video.

            Notes on first/last frame:
                * When both frames are provided, **match width/height** to avoid cropping; if they differ,
                  the tail frame may be auto-cropped to fit.
                * If you only need one guided frame, provide either first_frame or last_frame (not both).

            Image input constraints (for first/last frame):
                - Formats: jpeg, png, webp, bmp, tiff, gif
                - Aspect ratio (宽:高): 0.4–2.5
                - Width/Height (px): 300–6000
                - Size: < 30 MB
                - Base64 data URL example: data:video/png;base64,<BASE64>

    Model text commands (append after the prompt; unsupported keys are ignored by some models):
        --rs / --resolution <value>       Video resolution. Common values: 480p, 720p, 1080p.
                                          Default depends on model (e.g., doubao-seedance-1-0-pro: 1080p,
                                          some others default 720p).

        --rt / --ratio <value>            Aspect ratio. Typical: 16:9 (default), 9:16, 4:3, 3:4, 1:1, 2:1, 21:9.
                                          Some models support keep_ratio (keep source video ratio) or adaptive
                                          (auto choose suitable ratio).

        --dur / --duration <seconds>      Clip length in seconds. Seedance supports **3–12 s**;
                                          Wan2.1 仅支持 5 s。Default varies by model.

        --fps / --framespersecond <int>   Frame rate. Common: 16 or 24 (model-dependent; e.g., seaweed=24, wan2.1=16).

        --wm / --watermark <true|false>   Whether to add watermark. Default: **false** (per doc).

        --seed <int>                      Random seed in [-1, 2^32-1]. Default **-1** = auto seed.
                                          Same seed may yield similar (not guaranteed identical) results across runs.

        --cf / --camerafixed <true|false> Lock camera movement. Some models support this flag.
                                          true: try to keep camera fixed; false: allow movement. Default: **false**.

    Returns:
        Dict:
            API response containing task creation results for each input item. A typical shape is:
            {
                "status": "success",
                "success_list": [{"video_name": "video_url"}],
                "error_list": []
            }

    Constraints & Tips:
        - Keep prompt concise and focused (建议 ≤ 500 字); too many details may distract the model.
        - If using first/last frames, ensure their **aspect ratio matches** your chosen --rt to minimize cropping.
        - If you must reproduce results, specify an explicit --seed.
        - Unsupported parameters are ignored silently or may cause validation errors (model-specific).

    Minimal examples:
        1) Text-only batch of two 5-second clips at 720p, 16:9, 24 fps:
            params = [
                {
                    "video_name": "cat_park.mp4",
                    "prompt": "小猫骑着滑板穿过公园。 --rs 720p --rt 16:9 --dur 5 --fps 24 --wm false"
                },
                {
                    "video_name": "city_night.mp4",
                    "prompt": "霓虹灯下的城市延时摄影风。 --rs 720p --rt 16:9 --dur 5 --fps 24 --seed 7"
                },
            ]

        2) With guided first/last frame (square, 6 s, camera fixed):
            params = [
                {
                    "video_name": "logo_reveal.mp4",
                    "first_frame": "https://cdn.example.com/brand/logo_start.png",
                    "last_frame": "https://cdn.example.com/brand/logo_end.png",
                    "prompt": "品牌 Logo 从线稿到上色的变化。 --rs 1080p --rt 1:1 --dur 6 --fps 24 --cf true"
                }
            ]
`

type VideoGenerateConfig struct {
	ModelName string
	APIKey    string
	BaseURL   string
}

type VideoGenerateToolRequest struct {
	Params    []GenerateVideosRequest `json:"params"`
	BatchSize int                     `json:"batch_size,omitempty"`
}

type GenerateVideosRequest struct {
	VideoName  string  `json:"video_name"`
	Prompt     string  `json:"prompt"`
	FirstFrame *string `json:"first_frame,omitempty"`
	LastFrame  *string `json:"last_frame,omitempty"`
}

type VideoGenerateResult struct {
	SuccessList []*VideoResult `json:"success_list,omitempty"`
	ErrorList   []*VideoResult `json:"error_list,omitempty"`
	Status      string
}

type VideoResult struct {
	VideoName string `json:"video_name"`
	Url       string `json:"video_url"`
}

type VideoGenerateToolChanelMessage struct {
	Status       string
	ErrorMessage string
	Result       *VideoResult
}

const (
	VideoGenerateSuccessStatus = "success"
	VideoGenerateErrorStatus   = "error"
)

func NewVideoGenerateTool(config *VideoGenerateConfig) (tool.Tool, error) {
	if config == nil {
		config = &VideoGenerateConfig{}
	}
	if config.ModelName == "" {
		config.ModelName = utils.GetEnvWithDefault(common.MODEL_VIDEO_NAME, configs.GetGlobalConfig().Model.Video.Name, common.DEFAULT_MODEL_VIDEO_NAME)
	}
	if config.APIKey == "" {
		config.APIKey = utils.GetEnvWithDefault(common.MODEL_VIDEO_API_KEY, configs.GetGlobalConfig().Model.Video.ApiKey, utils.Must(veauth.GetArkToken(common.DEFAULT_MODEL_REGION)))
	}
	if config.BaseURL == "" {
		config.BaseURL = utils.GetEnvWithDefault(common.MODEL_VIDEO_API_BASE, configs.GetGlobalConfig().Model.Video.ApiBase, common.DEFAULT_MODEL_VIDEO_API_BASE)
	}

	log.Debug("Initializing video generation tool", "model", config.ModelName, "base_url", config.BaseURL)

	handler := func(ctx tool.Context, toolRequest VideoGenerateToolRequest) (*VideoGenerateResult, error) {
		client := arkruntime.NewClientWithApiKey(
			config.APIKey,
			arkruntime.WithBaseUrl(config.BaseURL),
		)

		if toolRequest.BatchSize == 0 {
			toolRequest.BatchSize = 10
		}

		result := &VideoGenerateResult{}
		var wg sync.WaitGroup
		ch := make(chan *VideoGenerateToolChanelMessage)
		for startIdx := 0; startIdx < len(toolRequest.Params); startIdx += toolRequest.BatchSize {
			var batch []GenerateVideosRequest
			if startIdx+toolRequest.BatchSize > len(toolRequest.Params) {
				batch = toolRequest.Params[startIdx:]
			} else {
				batch = toolRequest.Params[startIdx : startIdx+toolRequest.BatchSize]
			}
			for _, item := range batch {
				var resp model.CreateContentGenerationTaskResponse
				var err error
				if item.FirstFrame == nil {
					resp, err = client.CreateContentGenerationTask(ctx, model.CreateContentGenerationTaskRequest{
						Model: config.ModelName,
						Content: []*model.CreateContentGenerationContentItem{
							{
								Type: model.ContentGenerationContentItemTypeText,
								Text: volcengine.String(item.Prompt),
							},
						},
					})
				} else if item.LastFrame == nil {
					resp, err = client.CreateContentGenerationTask(ctx, model.CreateContentGenerationTaskRequest{
						Model: config.ModelName,
						Content: []*model.CreateContentGenerationContentItem{
							{
								Type: model.ContentGenerationContentItemTypeText,
								Text: volcengine.String(item.Prompt),
							},
							{
								Type: model.ContentGenerationContentItemTypeImage,
								ImageURL: &model.ImageURL{
									URL: *item.FirstFrame,
								},
							},
						},
					})
				} else {
					resp, err = client.CreateContentGenerationTask(ctx, model.CreateContentGenerationTaskRequest{
						Model: config.ModelName,
						Content: []*model.CreateContentGenerationContentItem{
							{
								Type: model.ContentGenerationContentItemTypeText,
								Text: volcengine.String(item.Prompt),
							},
							{
								Type: model.ContentGenerationContentItemTypeImage,
								ImageURL: &model.ImageURL{
									URL: *item.FirstFrame,
								},
								Role: volcengine.String("first_frame"),
							},
							{
								Type: model.ContentGenerationContentItemTypeImage,
								ImageURL: &model.ImageURL{
									URL: *item.LastFrame,
								},
								Role: volcengine.String("last_frame"),
							},
						},
					})
				}
				if err != nil {
					log.Error("Failed to create videos", "error", err)
					result.ErrorList = append(result.ErrorList, &VideoResult{VideoName: item.VideoName})
					continue
				}
				wg.Add(1)
				go func(videoName string, taskId string) {
					defer func() {
						wg.Done()
						if r := recover(); r != nil {
							log.Error("get video url panic", "recover", r, "video", videoName)
							ch <- &VideoGenerateToolChanelMessage{
								Status:       VideoGenerateErrorStatus,
								ErrorMessage: fmt.Sprintf("task panic: %v", r),
								Result:       &VideoResult{VideoName: videoName},
							}
						}
					}()
					for {
						getResp, err := client.GetContentGenerationTask(ctx, model.GetContentGenerationTaskRequest{
							ID: taskId,
						})
						if err != nil {
							log.Error("Failed to get video url", "error", err)
							ch <- &VideoGenerateToolChanelMessage{
								Status:       VideoGenerateErrorStatus,
								ErrorMessage: err.Error(),
								Result:       &VideoResult{VideoName: videoName},
							}
							return
						}
						if getResp.Error != nil {
							log.Error("Failed to get video url", "error", getResp.Error)
							ch <- &VideoGenerateToolChanelMessage{
								Status:       VideoGenerateErrorStatus,
								ErrorMessage: getResp.Error.Message,
								Result:       &VideoResult{VideoName: videoName},
							}
							return
						}
						switch getResp.Status {
						case model.StatusSucceeded:
							ch <- &VideoGenerateToolChanelMessage{Status: VideoGenerateSuccessStatus, Result: &VideoResult{VideoName: videoName, Url: getResp.Content.VideoURL}}
							return
						case model.StatusFailed:
							ch <- &VideoGenerateToolChanelMessage{Status: VideoGenerateErrorStatus, ErrorMessage: "video generation failed", Result: &VideoResult{VideoName: videoName}}
							return
						default:
							time.Sleep(10 * time.Second)
						}
					}
				}(item.VideoName, resp.ID)
			}
		}

		go func() {
			wg.Wait()
			close(ch)
		}()

		for res := range ch {
			if res.Status == VideoGenerateSuccessStatus {
				result.SuccessList = append(result.SuccessList, res.Result)
			}
			if res.Status == VideoGenerateErrorStatus {
				result.ErrorList = append(result.ErrorList, res.Result)
			}
		}

		if len(result.SuccessList) == 0 {
			result.Status = VideoGenerateErrorStatus
		} else {
			result.Status = VideoGenerateSuccessStatus
		}

		return result, nil
	}

	return functiontool.New(
		functiontool.Config{
			Name:        "video_generate",
			Description: videoGenerateToolDescription,
		},
		handler)
}
