// Example: go run ./examples/model/ark_responses -image /path/to/picture.png -tool
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	veagent "github.com/volcengine/veadk-go/v2/agent/llmagent"
	vemodel "github.com/volcengine/veadk-go/v2/model"
	verunner "github.com/volcengine/veadk-go/v2/runner"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/plugin"
	adkrunner "google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

func main() {
	path := flag.String("image", "", "local image path (read by this example, not by the model)")
	uri := flag.String("uri", "", "image URL, data URI or file_id://file-xxx; alternative to -image")
	mime := flag.String("mime", "image/png", "MIME type for -uri")
	asTool := flag.Bool("tool", false, "return the image from a function tool")
	stream := flag.Bool("stream", true, "stream model output")
	flag.Parse()
	if (*path == "") == (*uri == "") {
		log.Fatal("provide exactly one of -image or -uri")
	}
	load := func(agent.Context) (*genai.FunctionResponsePart, error) {
		if *uri != "" {
			return genai.NewFunctionResponsePartFromURI(*uri, *mime), nil
		}
		f, err := os.Open(*path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
		if err != nil {
			return nil, err
		}
		if len(b) > 16<<20 {
			return nil, fmt.Errorf("example image exceeds 16 MiB")
		}
		return genai.NewFunctionResponsePartFromBytes(b, http.DetectContentType(b)), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	m, err := vemodel.NewArkResponsesModel(ctx, os.Getenv("MODEL_AGENT_NAME"), &vemodel.ArkClientConfig{APIKey: os.Getenv("MODEL_AGENT_API_KEY"), BaseURL: os.Getenv("MODEL_AGENT_API_BASE")})
	if err != nil {
		log.Fatal(err)
	}
	cfg := &veagent.Config{Config: llmagent.Config{Name: "vision", Model: m, Instruction: "Describe the provided picture. When asked to use get_picture, call it before answering."}}
	var plugins []*plugin.Plugin
	message := genai.NewContentFromText("Describe this picture.", "user")
	if *asTool {
		t, p, err := imageTool(load)
		if err != nil {
			log.Fatal(err)
		}
		cfg.Tools = []tool.Tool{t}
		plugins = append(plugins, p)
		message = genai.NewContentFromText("Call get_picture, then describe the picture it returns.", "user")
	} else {
		p, err := load(nil)
		if err != nil {
			log.Fatal(err)
		}
		if p.InlineData != nil {
			message.Parts = append(message.Parts, &genai.Part{InlineData: &genai.Blob{Data: p.InlineData.Data, MIMEType: p.InlineData.MIMEType}})
		} else {
			message.Parts = append(message.Parts, &genai.Part{FileData: &genai.FileData{FileURI: p.FileData.FileURI, MIMEType: p.FileData.MIMEType}})
		}
	}
	a, err := veagent.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	r, err := verunner.New(verunner.Config{AppName: "ark_responses_example", Agent: a, AutoCreateSession: true, PluginConfig: adkrunner.PluginConfig{Plugins: plugins}})
	if err != nil {
		log.Fatal(err)
	}
	mode := agent.StreamingModeNone
	if *stream {
		mode = agent.StreamingModeSSE
	}
	for event, err := range r.Run(ctx, "example_user", fmt.Sprintf("example-%d", time.Now().UnixNano()), message, agent.RunConfig{StreamingMode: mode}) {
		if err != nil {
			log.Fatal(err)
		}
		if event.Content == nil || event.Partial {
			continue
		}
		for _, part := range event.Content.Parts {
			if part.Text != "" && !part.Thought {
				fmt.Println(part.Text)
			}
		}
	}
}
