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

package ve_viking

import (
	"os"
	"testing"

	"github.com/volcengine/veadk-go/v2/common"
)

func TestNew_NilConfig(t *testing.T) {
	if _, err := NewConfig(nil); err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestNew_MissingResourceAndIndexProject(t *testing.T) {
	t.Setenv(common.DATABASE_VIKING_PROJECT, "default")
	t.Setenv(common.DATABASE_VIKING_REGION, "cn-beijing")
	_, err := NewConfig(&ClientConfig{})
	if err == nil {
		t.Fatal("expected error when ResourceID and Index/Project missing")
	}
}

func TestNew_InvalidIndexNaming(t *testing.T) {
	_, err := NewConfig(&ClientConfig{Index: "1bad", Project: "default"})
	if err == nil {
		t.Fatal("expected invalid index naming error")
	}
	_, err = NewConfig(&ClientConfig{Index: "bad-name", Project: "default"})
	if err == nil {
		t.Fatal("expected invalid index naming error")
	}
}

func TestNew_DefaultsFromEnv(t *testing.T) {
	t.Setenv(common.DATABASE_VIKING_PROJECT, "default")
	t.Setenv(common.DATABASE_VIKING_REGION, "cn-beijing")
	ak := os.Getenv(common.VOLCENGINE_ACCESS_KEY)
	sk := os.Getenv(common.VOLCENGINE_SECRET_KEY)
	if ak == "" || sk == "" {
		t.Skip("missing VOLCENGINE_ACCESS_KEY or VOLCENGINE_SECRET_KEY")
	}
	t.Setenv(common.VOLCENGINE_ACCESS_KEY, ak)
	t.Setenv(common.VOLCENGINE_SECRET_KEY, sk)
	cfg := &ClientConfig{Index: "ValidIndex", Project: ""}
	cli, err := NewConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cli.Project == "" || cli.Region == "" || cli.AK == "" || cli.SK == "" {
		t.Fatal("expected defaults populated from env")
	}
}
