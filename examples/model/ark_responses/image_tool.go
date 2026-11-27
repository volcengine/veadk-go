package main

import (
	"sync"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

// ADK's functiontool returns a JSON object. This example uses the existing
// OnEvent callback to attach binary data before the tool event is persisted.
// The loader owns I/O (local read, sandbox download, etc.); the model never does.
func imageTool(load func(agent.Context) (*genai.FunctionResponsePart, error)) (tool.Tool, *plugin.Plugin, error) {
	type key struct{ invocation, call string }
	var mu sync.Mutex
	pending := map[key]*genai.FunctionResponsePart{}
	t, err := functiontool.New(functiontool.Config{Name: "get_picture", Description: "Load the picture to inspect. Returns a visual attachment."}, func(ctx agent.Context, _ struct{}) (map[string]any, error) {
		p, err := load(ctx)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		pending[key{ctx.InvocationID(), ctx.FunctionCallID()}] = p
		mu.Unlock()
		return map[string]any{"status": "loaded"}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	p, err := plugin.New(plugin.Config{Name: "picture_attachments", OnEventCallback: func(ctx agent.InvocationContext, event *session.Event) (*session.Event, error) {
		if event == nil || event.Partial || event.Content == nil {
			return nil, nil
		}
		mu.Lock()
		defer mu.Unlock()
		for _, part := range event.Content.Parts {
			if part == nil || part.FunctionResponse == nil {
				continue
			}
			f := part.FunctionResponse
			k := key{ctx.InvocationID(), f.ID}
			if attachment, ok := pending[k]; ok {
				f.Parts = append(f.Parts, attachment)
				delete(pending, k)
			}
		}
		return event, nil
	}, AfterRunCallback: func(ctx agent.InvocationContext) {
		mu.Lock()
		defer mu.Unlock()
		for k := range pending {
			if k.invocation == ctx.InvocationID() {
				delete(pending, k)
			}
		}
	}})
	return t, p, err
}
