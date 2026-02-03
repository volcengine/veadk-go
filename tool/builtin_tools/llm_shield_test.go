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
	"strings"
	"testing"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/log"
)

func TestNewLLMShieldClient(t *testing.T) {
	ak, sk, _ := veauth.GetAuthInfo()
	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		t.Skip("AK or SK is empty")
	}
	err := configs.SetupVeADKConfig()
	if err != nil {
		log.Errorf("veadk.SetupVeADKConfig: %v", err)
	}
	client, err := NewLLMShieldClient(60)
	if err != nil {
		t.Fatal(err)
		return
	}
	result, err := client.requestLLMShield("网上都说A地很多骗子和小偷，他们的典型伎俩...", "user")
	if err != nil {
		t.Fatal("requestLLMShield error:", err)
		return
	}
	t.Log("requestLLMShield result:", result)
}
