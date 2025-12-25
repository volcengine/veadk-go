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
	"fmt"
	"testing"

	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/utils"
)

func TestRunCodeHandler(t *testing.T) {
	ak := utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, "")
	sk := utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, "")
	if ak == "" || sk == "" {
		t.Skip()
	}

	require := RunCodeArgs{
		Code:     `print("test run code")`,
		Language: "python3.10",
		Timeout:  30,
	}

	result, err := runCodeHandler(nil, require)
	if err != nil {
		t.Fatal(err)
		return
	}
	fmt.Println(result)
}
