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
	"testing"

	"github.com/stretchr/testify/assert"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
)

func TestNewImageEditTool(t *testing.T) {
	tool, err := NewImageEditTool(&ImageEditConfig{
		ModelName: "doubao-seededit-3-0-i2i-250628",
		APIKey:    "test-api-key",
		BaseURL:   "https://test-api.com",
	})

	assert.NoError(t, err)
	assert.NotNil(t, tool)
}

func TestBuildImageEditModelRequest_Defaults(t *testing.T) {
	req := buildImageEditModelRequest("edit-model", EditImagesRequest{
		OriginImage: "https://example.com/input.png",
		Prompt:      "make the sky blue",
	})

	assert.Equal(t, "edit-model", req.Model)
	assert.Equal(t, "make the sky blue", req.Prompt)
	assert.Equal(t, "https://example.com/input.png", req.Image)
	assert.NotNil(t, req.ResponseFormat)
	assert.Equal(t, defaultImageEditResponseFormat, *req.ResponseFormat)
	assert.NotNil(t, req.GuidanceScale)
	assert.Equal(t, defaultImageEditGuidanceScale, *req.GuidanceScale)
	assert.NotNil(t, req.Watermark)
	assert.True(t, *req.Watermark)
	assert.NotNil(t, req.Seed)
	assert.Equal(t, int64(defaultImageEditSeed), *req.Seed)
}

func TestBuildImageEditModelRequest_Overrides(t *testing.T) {
	guidanceScale := 7.5
	watermark := false
	seed := int64(42)

	req := buildImageEditModelRequest("edit-model", EditImagesRequest{
		OriginImage:    "data:image/png;base64,abc",
		Prompt:         "remove the logo",
		ResponseFormat: "b64_json",
		GuidanceScale:  &guidanceScale,
		Watermark:      &watermark,
		Seed:           &seed,
	})

	assert.Equal(t, "data:image/png;base64,abc", req.Image)
	assert.Equal(t, "b64_json", *req.ResponseFormat)
	assert.Equal(t, guidanceScale, *req.GuidanceScale)
	assert.Equal(t, watermark, *req.Watermark)
	assert.Equal(t, seed, *req.Seed)
}

func TestImageEditResultsFromResponse(t *testing.T) {
	url := "https://example.com/output.png"
	b64 := "aW1hZ2U="

	messages := imageEditResultsFromResponse("edited", []*arkmodel.Image{
		{Url: volcengine.String(url)},
		{B64Json: volcengine.String(b64)},
		nil,
		{},
	})

	assert.Len(t, messages, 4)
	assert.Equal(t, ImageEditSuccessStatus, messages[0].Status)
	assert.Equal(t, "edited_0", messages[0].Result.ImageName)
	assert.Equal(t, url, messages[0].Result.Url)
	assert.Equal(t, ImageEditSuccessStatus, messages[1].Status)
	assert.Equal(t, "edited_1", messages[1].Result.ImageName)
	assert.Equal(t, b64, messages[1].Result.B64Json)
	assert.Equal(t, ImageEditErrorStatus, messages[2].Status)
	assert.Equal(t, "edited_2", messages[2].Result.ImageName)
	assert.Contains(t, messages[2].Result.Error, "empty image result")
	assert.Equal(t, ImageEditErrorStatus, messages[3].Status)
	assert.Equal(t, "edited_3", messages[3].Result.ImageName)
	assert.Contains(t, messages[3].Result.Error, "image url or b64_json is empty")
}

func TestImageEditResultsFromEmptyResponse(t *testing.T) {
	messages := imageEditResultsFromResponse("edited", nil)

	assert.Len(t, messages, 1)
	assert.Equal(t, ImageEditErrorStatus, messages[0].Status)
	assert.Equal(t, "edited", messages[0].Result.ImageName)
	assert.Contains(t, messages[0].Result.Error, "no images returned")
}

func TestImageEditName(t *testing.T) {
	assert.Equal(t, "custom", imageEditName(EditImagesRequest{ImageName: "custom"}, 3))
	assert.Equal(t, "generated_image_3", imageEditName(EditImagesRequest{}, 3))
}
