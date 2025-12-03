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

package configs

import (
	"os"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/common"
)

func Test_loadConfigFromProjectEnv(t *testing.T) {
	mockey.PatchConvey("Test_loadConfigFromProjectEnv", t, func() {
		mockey.Mock(os.Getwd).Return("../test", nil).Build()
		_ = loadConfigFromProjectEnv()
		assert.Equal(t, "doubao-seed-1-6-250615", os.Getenv(common.MODEL_AGENT_NAME))

		os.Setenv(common.MODEL_AGENT_NAME, "test")
		_ = loadConfigFromProjectEnv()
		assert.Equal(t, "test", os.Getenv(common.MODEL_AGENT_NAME))
	})
}

func Test_loadConfigFromProjectYaml(t *testing.T) {
	mockey.PatchConvey("Test_loadConfigFromProjectYaml", t, func() {
		mockey.Mock(os.Getwd).Return("../test", nil).Build()
		_ = loadConfigFromProjectYaml()
		assert.Equal(t, "doubao-seed-1-6-250615", os.Getenv(common.MODEL_AGENT_NAME))

		os.Setenv(common.MODEL_AGENT_NAME, "test")
		_ = loadConfigFromProjectYaml()
		assert.Equal(t, "test", os.Getenv(common.MODEL_AGENT_NAME))
	})
}

func Test_getEnv(t *testing.T) {
	mockey.PatchConvey("Test_getEnv", t, func() {
		mockey.Mock(os.Getwd).Return("../test", nil).Build()
		_ = loadConfigFromProjectYaml()
		assert.Equal(t, "doubao-seed-1-6-250615", getEnv(common.MODEL_AGENT_NAME, "", false))
		assert.Equal(t, "test", getEnv("test_key", "test", false))
	})
}

func TestSetupVeADKConfig(t *testing.T) {
	mockey.PatchConvey("TestSetupVeADKConfig", t, func() {
		mockey.Mock(os.Getwd).Return("../test", nil).Build()
		_ = SetupVeADKConfig()
		assert.Equal(t, "doubao-seed-1-6-250615", os.Getenv(common.MODEL_AGENT_NAME))
	})
}
