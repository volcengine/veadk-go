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

package runner

import (
	"bytes"
	"testing"

	"google.golang.org/genai"
)

func TestUserText(t *testing.T) {
	content := UserText("hello")
	if content.Role != genai.RoleUser {
		t.Fatalf("Role = %q, want %q", content.Role, genai.RoleUser)
	}
	if got := content.Parts[0].Text; got != "hello" {
		t.Fatalf("Text = %q, want %q", got, "hello")
	}
}

func TestUserParts(t *testing.T) {
	textPart := genai.NewPartFromText("hello")
	bytesPart := genai.NewPartFromBytes([]byte("image"), "image/png")
	content := UserParts(textPart, bytesPart)

	if content.Role != genai.RoleUser {
		t.Fatalf("Role = %q, want %q", content.Role, genai.RoleUser)
	}
	if len(content.Parts) != 2 {
		t.Fatalf("len(Parts) = %d, want 2", len(content.Parts))
	}
	if content.Parts[0] != textPart || content.Parts[1] != bytesPart {
		t.Fatal("UserParts did not preserve part order")
	}
}

func TestUserBytes(t *testing.T) {
	data := []byte("image")
	content := UserBytes(data, "image/png")
	if content.Role != genai.RoleUser {
		t.Fatalf("Role = %q, want %q", content.Role, genai.RoleUser)
	}
	inlineData := content.Parts[0].InlineData
	if inlineData == nil {
		t.Fatal("InlineData = nil")
	}
	if inlineData.MIMEType != "image/png" {
		t.Fatalf("MIMEType = %q, want %q", inlineData.MIMEType, "image/png")
	}
	if !bytes.Equal(inlineData.Data, data) {
		t.Fatalf("Data = %q, want %q", inlineData.Data, data)
	}
}

func TestUserURI(t *testing.T) {
	content := UserURI("gs://bucket/object.png", "image/png")
	if content.Role != genai.RoleUser {
		t.Fatalf("Role = %q, want %q", content.Role, genai.RoleUser)
	}
	fileData := content.Parts[0].FileData
	if fileData == nil {
		t.Fatal("FileData = nil")
	}
	if fileData.FileURI != "gs://bucket/object.png" {
		t.Fatalf("FileURI = %q, want %q", fileData.FileURI, "gs://bucket/object.png")
	}
	if fileData.MIMEType != "image/png" {
		t.Fatalf("MIMEType = %q, want %q", fileData.MIMEType, "image/png")
	}
}
