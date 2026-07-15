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
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
)

func TestNewTTSTool(t *testing.T) {
	tool, err := NewTTSTool(&TTSConfig{
		AppID:  "test-app-id",
		APIKey: "test-api-key",
	})

	assert.NoError(t, err)
	assert.NotNil(t, tool)
}

func TestTTSHandler(t *testing.T) {
	mockey.PatchConvey("success", t, func() {
		pcmData := []byte{0x01, 0x02, 0x03, 0x04}
		encodedPCM := base64.StdEncoding.EncodeToString(pcmData)

		mockey.Mock(doTTSRequest).To(func(_ *http.Client, r *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, ttsURL, r.URL.String())
			assert.Equal(t, "test-app-id", r.Header.Get("X-Api-App-Id"))
			assert.Equal(t, "test-api-key", r.Header.Get("X-Api-Key"))
			assert.Equal(t, "seed-tts-2.0", r.Header.Get("X-Api-Resource-Id"))
			assert.Contains(t, r.Header.Get("Content-Type"), "application/json")
			assert.Equal(t, "keep-alive", r.Header.Get("Connection"))

			var body ttsRequest
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)
			assert.Equal(t, "", body.User.UID)
			assert.Equal(t, "test text", body.ReqParams.Text)
			assert.Equal(t, "test-speaker", body.ReqParams.Speaker)
			assert.Equal(t, "pcm", body.ReqParams.AudioParams.Format)
			assert.Equal(t, 16000, body.ReqParams.AudioParams.BitRate)
			assert.Equal(t, 24000, body.ReqParams.AudioParams.SampleRate)
			assert.True(t, body.ReqParams.AudioParams.EnableTimestamp)

			var additions map[string]any
			err = json.Unmarshal([]byte(body.ReqParams.Additions), &additions)
			require.NoError(t, err)
			assert.Equal(t, "zh", additions["explicit_language"])
			assert.Equal(t, true, additions["disable_markdown_filter"])
			assert.Equal(t, true, additions["enable_timestamp"])

			return newJSONResponse(http.StatusOK, strings.Join([]string{
				`{"code":0,"data":"` + encodedPCM + `"}`,
				`{"code":20000000}`,
			}, "\n")), nil
		}).Build()

		cfg := &TTSConfig{
			AppID:      "test-app-id",
			APIKey:     "test-api-key",
			Speaker:    "test-speaker",
			OutputPath: t.TempDir(),
			HTTPClient: http.DefaultClient,
		}

		result, err := cfg.ttsHandler(nil, TTSArgs{Text: " test text "})

		require.NoError(t, err)
		require.NotEmpty(t, result.SavedAudioPath)
		t.Cleanup(func() {
			_ = os.Remove(result.SavedAudioPath)
		})

		stat, err := os.Stat(result.SavedAudioPath)
		require.NoError(t, err)
		assert.Greater(t, stat.Size(), int64(0))

		saved, err := os.ReadFile(result.SavedAudioPath)
		require.NoError(t, err)
		assert.Equal(t, pcmData, saved)
	})
}

func TestNewTTSToolConfigError(t *testing.T) {
	t.Run("missing app id", func(t *testing.T) {
		t.Setenv(common.TOOL_VESPEECH_APP_ID, "")

		tool, err := NewTTSTool(&TTSConfig{APIKey: "test-api-key"})

		assert.ErrorIs(t, err, ErrTTSConfig)
		assert.Contains(t, err.Error(), common.TOOL_VESPEECH_APP_ID)
		assert.Nil(t, tool)
	})

	t.Run("missing api key", func(t *testing.T) {
		t.Setenv(common.TOOL_VESPEECH_API_KEY, "")

		mockey.PatchConvey("empty token fallback", t, func() {
			mockey.Mock(veauth.GetSpeechToken).Return("", nil).Build()

			tool, err := NewTTSTool(&TTSConfig{AppID: "test-app-id"})

			assert.ErrorIs(t, err, ErrTTSConfig)
			assert.Contains(t, err.Error(), common.TOOL_VESPEECH_API_KEY)
			assert.Nil(t, tool)
		})
	})

	t.Run("token fallback error", func(t *testing.T) {
		t.Setenv(common.TOOL_VESPEECH_API_KEY, "")

		mockey.PatchConvey("token error", t, func() {
			mockey.Mock(veauth.GetSpeechToken).Return("", errors.New("token error")).Build()

			tool, err := NewTTSTool(&TTSConfig{AppID: "test-app-id"})

			assert.ErrorIs(t, err, ErrTTSConfig)
			assert.Contains(t, err.Error(), "token error")
			assert.Nil(t, tool)
		})
	})
}

func TestTTSHandlerEmptyText(t *testing.T) {
	cfg := &TTSConfig{
		AppID:      "test-app-id",
		APIKey:     "test-api-key",
		Speaker:    "test-speaker",
		OutputPath: t.TempDir(),
		HTTPClient: http.DefaultClient,
	}

	result, err := cfg.ttsHandler(nil, TTSArgs{Text: " "})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "text is empty")
	assert.Empty(t, result.SavedAudioPath)
}
