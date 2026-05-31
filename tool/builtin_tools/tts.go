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
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/utils"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const (
	ttsURL            = "https://openspeech.bytedance.com/api/v3/tts/unidirectional"
	defaultTTSSpeaker = "zh_female_vv_uranus_bigtts"
	defaultTTSTimeout = 60 * time.Second
)

var ErrTTSConfig = errors.New("tts config error")

//go:noinline
func doTTSRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}

var ttsToolDescription = `
	文本转语音，输出 pcm 音频文件。

	Args:
		text (str): The text to convert to speech.

	Returns:
		The saved pcm audio file path.
`

type TTSConfig struct {
	AppID      string
	APIKey     string
	Speaker    string
	OutputPath string
	Region     string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type TTSArgs struct {
	Text string `json:"text" jsonschema:"The text to convert to speech"`
}

type TTSResult struct {
	SavedAudioPath string `json:"saved_audio_path,omitempty"`
}

type ttsRequest struct {
	User      ttsUser      `json:"user"`
	ReqParams ttsReqParams `json:"req_params"`
}

type ttsUser struct {
	UID string `json:"uid"`
}

type ttsReqParams struct {
	Text        string         `json:"text"`
	Speaker     string         `json:"speaker"`
	AudioParams ttsAudioParams `json:"audio_params"`
	Additions   string         `json:"additions"`
}

type ttsAudioParams struct {
	Format          string `json:"format"`
	BitRate         int    `json:"bit_rate"`
	SampleRate      int    `json:"sample_rate"`
	EnableTimestamp bool   `json:"enable_timestamp"`
}

type ttsStreamResponse struct {
	Code int    `json:"code"`
	Data string `json:"data"`
}

type userIDContext interface {
	UserID() string
}

func NewTTSTool(cfg *TTSConfig) (tool.Tool, error) {
	if cfg == nil {
		cfg = &TTSConfig{}
	}
	if err := cfg.applyDefaults(); err != nil {
		return nil, err
	}
	return functiontool.New(
		functiontool.Config{
			Name:        "text_to_speech",
			Description: ttsToolDescription,
		},
		cfg.ttsHandler)
}

func (c *TTSConfig) ttsHandler(ctx tool.Context, args TTSArgs) (TTSResult, error) {
	text := strings.TrimSpace(args.Text)
	if text == "" {
		return TTSResult{}, fmt.Errorf("tts text is empty")
	}

	executeCtx := context.Background()
	if ctx != nil {
		executeCtx = ctx
	}
	savedAudioPath, err := c.execute(executeCtx, text)
	if err != nil {
		return TTSResult{}, err
	}
	return TTSResult{SavedAudioPath: savedAudioPath}, nil
}

func (c *TTSConfig) applyDefaults() error {
	c.AppID = strings.TrimSpace(c.AppID)
	if c.AppID == "" {
		c.AppID = strings.TrimSpace(utils.GetEnvWithDefault(common.TOOL_VESPEECH_APP_ID))
	}
	if c.AppID == "" {
		return fmt.Errorf("%w: %s is required", ErrTTSConfig, common.TOOL_VESPEECH_APP_ID)
	}

	c.Speaker = strings.TrimSpace(c.Speaker)
	if c.Speaker == "" {
		c.Speaker = strings.TrimSpace(utils.GetEnvWithDefault(common.TOOL_VESPEECH_SPEAKER))
	}
	if c.Speaker == "" {
		c.Speaker = defaultTTSSpeaker
	}

	c.OutputPath = strings.TrimSpace(c.OutputPath)
	if c.OutputPath == "" {
		c.OutputPath = strings.TrimSpace(utils.GetEnvWithDefault(common.TOOL_VESPEECH_AUDIO_OUTPUT_PATH))
	}
	if c.OutputPath == "" {
		c.OutputPath = os.TempDir()
	}

	c.APIKey = strings.TrimSpace(c.APIKey)
	if c.APIKey == "" {
		apiKey, err := resolveTTSAPIKey(c.Region)
		if err != nil {
			return err
		}
		c.APIKey = strings.TrimSpace(apiKey)
	}
	if c.APIKey == "" {
		return fmt.Errorf("%w: %s is required or Volcano Engine credentials must be available", ErrTTSConfig, common.TOOL_VESPEECH_API_KEY)
	}

	if c.Timeout <= 0 {
		c.Timeout = defaultTTSTimeout
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: c.Timeout}
	}
	return nil
}

func resolveTTSAPIKey(region string) (string, error) {
	if key := strings.TrimSpace(utils.GetEnvWithDefault(common.TOOL_VESPEECH_API_KEY)); key != "" {
		return key, nil
	}
	key, err := veauth.GetSpeechToken(region)
	if err != nil {
		return "", fmt.Errorf("%w: failed to resolve API key: %v", ErrTTSConfig, err)
	}
	return key, nil
}

func (c *TTSConfig) execute(ctx context.Context, text string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	additions, err := json.Marshal(map[string]any{
		"explicit_language":       "zh",
		"disable_markdown_filter": true,
		"enable_timestamp":        true,
	})
	if err != nil {
		return "", err
	}

	body := ttsRequest{
		User: ttsUser{UID: userIDFromContext(ctx)},
		ReqParams: ttsReqParams{
			Text:    text,
			Speaker: c.Speaker,
			AudioParams: ttsAudioParams{
				Format:          "pcm",
				BitRate:         16000,
				SampleRate:      24000,
				EnableTimestamp: true,
			},
			Additions: string(additions),
		},
	}

	reqBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ttsURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("X-Api-App-Id", c.AppID)
	httpReq.Header.Set("X-Api-Key", c.APIKey)
	httpReq.Header.Set("X-Api-Resource-Id", "seed-tts-2.0")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Connection", "keep-alive")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := doTTSRequest(httpClient, httpReq)
	if err != nil {
		return "", fmt.Errorf("tts request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return "", fmt.Errorf("read tts response failed: %w", readErr)
		}
		if len(respBody) == 0 {
			return "", fmt.Errorf("tts HTTP error: status=%d", resp.StatusCode)
		}
		return "", fmt.Errorf("tts HTTP error: %s", string(respBody))
	}

	audioData, err := readTTSAudioData(resp.Body)
	if err != nil {
		return "", err
	}
	return c.saveAudioData(audioData)
}

func readTTSAudioData(r io.Reader) ([]byte, error) {
	var audioData bytes.Buffer
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var parsed ttsStreamResponse
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			return nil, fmt.Errorf("unmarshal tts stream response failed: %w, line: %s", err, line)
		}
		if parsed.Code == 0 {
			if parsed.Data == "" {
				continue
			}
			chunkAudio, err := base64.StdEncoding.DecodeString(parsed.Data)
			if err != nil {
				return nil, fmt.Errorf("decode tts audio data failed: %w", err)
			}
			_, _ = audioData.Write(chunkAudio)
			continue
		}
		if parsed.Code == 20000000 {
			break
		}
		if parsed.Code > 0 {
			return nil, fmt.Errorf("tts response error: %s", line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read tts stream response failed: %w", err)
	}
	return audioData.Bytes(), nil
}

func (c *TTSConfig) saveAudioData(audioData []byte) (string, error) {
	if err := os.MkdirAll(c.OutputPath, 0o755); err != nil {
		return "", fmt.Errorf("create tts output path failed: %w", err)
	}

	audioFile, err := os.CreateTemp(c.OutputPath, "tts_*.pcm")
	if err != nil {
		return "", fmt.Errorf("create tts audio file failed: %w", err)
	}
	audioPath := audioFile.Name()
	if _, err = audioFile.Write(audioData); err != nil {
		_ = audioFile.Close()
		_ = os.Remove(audioPath)
		return "", fmt.Errorf("write tts audio file failed: %w", err)
	}
	if err = audioFile.Close(); err != nil {
		_ = os.Remove(audioPath)
		return "", fmt.Errorf("close tts audio file failed: %w", err)
	}
	return audioPath, nil
}

func userIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if userCtx, ok := ctx.(userIDContext); ok {
		return userCtx.UserID()
	}
	return ""
}
