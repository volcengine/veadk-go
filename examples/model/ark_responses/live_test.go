package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	"github.com/volcengine/veadk-go/v2/integrations/agentkit"
	vemodel "github.com/volcengine/veadk-go/v2/model"
	verunner "github.com/volcengine/veadk-go/v2/runner"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model/file"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/plugin"
	adkrunner "google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// Explicit opt-in: this test calls Ark and creates/deletes a real AIO sandbox.
// Set VEADK_ARK_RESPONSES_LIVE=1 and optionally VEADK_LIVE_ENV_FILE.
func TestArkResponsesLive(t *testing.T) {
	if os.Getenv("VEADK_ARK_RESPONSES_LIVE") != "1" {
		t.Skip("set VEADK_ARK_RESPONSES_LIVE=1 to run real model/sandbox acceptance")
	}
	if path := os.Getenv("VEADK_LIVE_ENV_FILE"); path != "" {
		if err := godotenv.Load(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"MODEL_AGENT_NAME", "MODEL_AGENT_API_KEY", "AGENTKIT_TOOL_ID"} {
		if os.Getenv(key) == "" {
			t.Fatalf("missing %s", key)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	hc := &http.Client{Transport: &http.Transport{}, Timeout: 2 * time.Minute}
	defer hc.CloseIdleConnections()
	cfg := &vemodel.ArkClientConfig{APIKey: os.Getenv("MODEL_AGENT_API_KEY"), BaseURL: os.Getenv("MODEL_AGENT_API_BASE"), HTTPClient: hc}
	m, err := vemodel.NewArkResponsesModel(ctx, os.Getenv("MODEL_AGENT_NAME"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	pic := image.NewRGBA(image.Rect(0, 0, 256, 192))
	for y := 0; y < 192; y++ {
		for x := 0; x < 256; x++ {
			c := color.RGBA{255, 255, 255, 255}
			if x >= 16 && x <= 116 && y >= 16 && y <= 176 {
				c = color.RGBA{255, 0, 0, 255}
			}
			if (x-186)*(x-186)+(y-106)*(y-106) <= 2500 {
				c = color.RGBA{0, 0, 255, 255}
			}
			pic.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, pic); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	localPath := filepath.Join(t.TempDir(), "shapes.png")
	if err := os.WriteFile(localPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	opts := []arkruntime.ConfigOption{arkruntime.WithHTTPClient(hc)}
	if cfg.BaseURL != "" {
		opts = append(opts, arkruntime.WithBaseUrl(cfg.BaseURL))
	}
	files := arkruntime.NewClientWithApiKey(cfg.APIKey, opts...)
	f, err := os.Open(localPath)
	if err != nil {
		t.Fatal(err)
	}
	uploaded, err := files.UploadFile(ctx, &file.UploadFileRequest{File: f, Purpose: file.PurposeUserData})
	_ = f.Close()
	if err != nil {
		t.Fatalf("Files API upload: %v", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := files.DeleteFile(cleanup, uploaded.ID); err != nil {
			t.Errorf("Files API cleanup: %v", err)
		} else {
			t.Log("Files API test resource deleted")
		}
	}()
	for i := 0; i < 60; i++ {
		meta, err := files.RetrieveFile(ctx, uploaded.ID)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Status == file.StatusActive {
			break
		}
		if meta.Status == file.StatusFailed || i == 59 {
			t.Fatal("Files API processing failed/timed out")
		}
		time.Sleep(time.Second)
	}
	for _, mode := range []string{"none", "local", "aio"} {
		t.Run(mode, func(t *testing.T) {
			app := "ark_responses_live"
			user := "acceptance"
			sid := uuid.NewString()
			store := session.InMemoryService()
			var lifecycle *agentkit.AIOLifecycle
			loadBytes := func(context.Context) ([]byte, error) { return os.ReadFile(localPath) }
			if mode == "aio" {
				lifecycle, err = agentkit.NewAIOLifecycle(agentkit.AIOLifecycleConfig{SessionService: store})
				if err != nil {
					t.Fatal(err)
				}
				s, err := lifecycle.Create(ctx, app, user, sid, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					cleanup, cancel := context.WithTimeout(context.Background(), time.Minute)
					defer cancel()
					if err := lifecycle.Delete(cleanup, app, user, sid); err != nil {
						t.Errorf("sandbox cleanup: %v", err)
					} else {
						t.Log("AIO test sandbox deleted")
					}
				}()
				client, err := agentkit.NewClient(nil)
				if err != nil {
					t.Fatal(err)
				}
				endpoint, err := agentkit.ResolveAIOEndpointFor(ctx, client, s.State(), app, user, sid)
				if err != nil {
					t.Fatal(err)
				}
				sandbox, err := agentkit.NewAIOFiles(endpoint)
				if err != nil {
					t.Fatal(err)
				}
				defer sandbox.Close()
				const path = "/workspace/vision-acceptance.png"
				if _, err := sandbox.Upload(ctx, path, io.NopCloser(bytes.NewReader(data))); err != nil {
					t.Fatal(err)
				}
				loadBytes = func(ctx context.Context) ([]byte, error) {
					u, err := url.Parse(endpoint)
					if err != nil {
						return nil, fmt.Errorf("invalid sandbox endpoint")
					}
					u.Path = strings.TrimRight(u.Path, "/") + "/v1/file/download"
					q := u.Query()
					q.Set("path", path)
					u.RawQuery = q.Encode()
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
					if err != nil {
						return nil, fmt.Errorf("sandbox request failed")
					}
					if k := os.Getenv("SANDBOX_API_KEY"); k != "" {
						req.Header.Set("X-API-Key", k)
					}
					res, err := hc.Do(req)
					if err != nil {
						return nil, fmt.Errorf("sandbox download transport failed")
					}
					defer res.Body.Close()
					if res.StatusCode != 200 {
						return nil, fmt.Errorf("sandbox download HTTP %d", res.StatusCode)
					}
					b, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
					if err == nil && !bytes.Equal(b, data) {
						return nil, fmt.Errorf("sandbox bytes differ")
					}
					return b, err
				}
			}
			skillDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: vision-acceptance\ndescription: Inspect pictures using get_picture when requested.\n---\nUse the provided image attachment to answer visual questions.\n"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, source := range []string{"inline", "file_id"} {
				for _, asTool := range []bool{false, true} {
					for _, stream := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/tool=%v/stream=%v", source, asTool, stream), func(t *testing.T) {
							var calls atomic.Int32
							load := func(c agent.Context) (*genai.FunctionResponsePart, error) {
								calls.Add(1)
								if source == "file_id" {
									return genai.NewFunctionResponsePartFromURI("file_id://"+uploaded.ID, "image/png"), nil
								}
								requestCtx := ctx
								if c != nil {
									requestCtx = c
								}
								b, err := loadBytes(requestCtx)
								if err != nil {
									return nil, err
								}
								return genai.NewFunctionResponsePartFromBytes(b, "image/png"), nil
							}
							tool, p, err := imageTool(load)
							if err != nil {
								t.Fatal(err)
							}
							config := &veagent.Config{Config: llmagent.Config{Name: "vision", Model: m, Instruction: "Answer in English. Identify the two colored shapes in the latest picture and their positions. When asked to call get_picture, call it even if earlier pictures exist; do not use other tools."}}
							if mode == "local" {
								config.SkillsMode = "local"
								config.Skills = []string{skillDir}
							} else if mode == "aio" {
								config.SkillsMode = "aio_sandbox"
							}
							var plugins []*plugin.Plugin
							message := genai.NewContentFromText("Identify the colored shapes in this picture in English.", "user")
							if asTool {
								config.Tools = append(config.Tools, tool)
								plugins = []*plugin.Plugin{p}
								message = genai.NewContentFromText("Call get_picture now; identify the colored shapes and positions in the picture it returns. Do not answer before calling it.", "user")
							} else {
								attachment, err := load(nil)
								if err != nil {
									t.Fatal(err)
								}
								if attachment.InlineData != nil {
									message.Parts = append(message.Parts, &genai.Part{InlineData: &genai.Blob{Data: attachment.InlineData.Data, MIMEType: attachment.InlineData.MIMEType}})
								} else {
									message.Parts = append(message.Parts, &genai.Part{FileData: &genai.FileData{FileURI: attachment.FileData.FileURI, MIMEType: attachment.FileData.MIMEType}})
								}
							}
							a, err := veagent.New(config)
							if err != nil {
								t.Fatal(err)
							}
							r, err := verunner.New(verunner.Config{AppName: app, Agent: a, SessionService: store, AIOSandboxLifecycle: lifecycle, AutoCreateSession: mode != "aio", PluginConfig: adkrunner.PluginConfig{Plugins: plugins}})
							if err != nil {
								t.Fatal(err)
							}
							runID := sid
							if mode != "aio" {
								runID = uuid.NewString()
							}
							streaming := agent.StreamingModeNone
							if stream {
								streaming = agent.StreamingModeSSE
							}
							answer := ""
							attachments := 0
							for ev, err := range r.Run(ctx, user, runID, message, agent.RunConfig{StreamingMode: streaming}) {
								if err != nil {
									t.Fatal(err)
								}
								if ev.Content == nil || ev.Partial {
									continue
								}
								for _, part := range ev.Content.Parts {
									if !part.Thought {
										answer += part.Text
									}
									if f := part.FunctionResponse; f != nil {
										attachments += len(f.Parts)
									}
								}
							}
							lower := strings.ToLower(answer)
							for _, want := range []string{"red", "blue", "rectangle", "circle"} {
								if !strings.Contains(lower, want) {
									t.Fatalf("missing %s in model answer: %s", want, answer)
								}
							}
							if asTool && (calls.Load() == 0 || attachments == 0) {
								t.Fatal("no actual tool call/image attachment")
							}
							saved, err := store.Get(ctx, &session.GetRequest{AppName: app, UserID: user, SessionID: runID})
							if err != nil {
								t.Fatal(err)
							}
							if asTool {
								found := false
								for ev := range saved.Session.Events().All() {
									if ev.Content != nil {
										for _, part := range ev.Content.Parts {
											if part.FunctionResponse != nil && len(part.FunctionResponse.Parts) > 0 {
												found = true
											}
										}
									}
								}
								if !found {
									t.Fatal("tool image not persisted")
								}
							}
							t.Logf("PASS recognized red rectangle and blue circle; tool attachments=%d", attachments)
						})
					}
				}
			}
		})
	}
}
