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

package ve_viking_knowledge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/volcengine/veadk-go/common"
)

func TestClient_SearchKnowledge(t *testing.T) {
	client := getClientOrSkip(t, "sjy_test_coffee_kg")
	fmt.Println("Search knowledge by knowledge")
	result, err := client.SearchKnowledge("拿铁", 5, 1, nil, true)
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Println(result.Data.ResultList[0].DocInfo)
	t.Log("result = ", result)
}

func TestNew_NilConfig(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestNew_MissingResourceAndIndexProject(t *testing.T) {
	t.Setenv(common.DATABASE_VIKING_PROJECT, "default")
	t.Setenv(common.DATABASE_VIKING_REGION, "cn-beijing")
	_, err := New(&Client{})
	if err == nil {
		t.Fatal("expected error when ResourceID and Index/Project missing")
	}
}

func TestNew_InvalidIndexNaming(t *testing.T) {
	_, err := New(&Client{Index: "1bad", Project: "default"})
	if err == nil {
		t.Fatal("expected invalid index naming error")
	}
	_, err = New(&Client{Index: "bad-name", Project: "default"})
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
	cfg := &Client{Index: "ValidIndex", Project: ""}
	cli, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cli.Project == "" || cli.Region == "" || cli.AK == "" || cli.SK == "" {
		t.Fatal("expected defaults populated from env")
	}
}

func TestNew_WithResourceOnly(t *testing.T) {
	t.Setenv(common.DATABASE_VIKING_PROJECT, "default")
	t.Setenv(common.DATABASE_VIKING_REGION, "cn-beijing")
	cli, err := New(&Client{ResourceID: "kb-xxxx"})
	if err != nil {
		t.Fatal(err)
	}
	if cli.ResourceID == "" {
		t.Fatal("expected ResourceID retained")
	}
}

func getClientOrSkip(t *testing.T, index string) Client {
	t.Helper()
	ak := os.Getenv(common.VOLCENGINE_ACCESS_KEY)
	sk := os.Getenv(common.VOLCENGINE_SECRET_KEY)
	if ak == "" || sk == "" {
		t.Skip("missing required env: VOLCENGINE_ACCESS_KEY/VOLCENGINE_SECRET_KEY")
	}
	client, err := New(&Client{Index: index, Project: "default", AK: ak, SK: sk})
	if err != nil {
		t.Fatal(err)
		t.Skip("missing required env: VOLCENGINE_ACCESS_KEY/VOLCENGINE_SECRET_KEY")
	}
	return *client
}

func TestClient_CollectionCreateInfoDelete(t *testing.T) {
	idx := fmt.Sprintf("veadk_test_%d", time.Now().UnixNano())
	client := getClientOrSkip(t, idx)
	respCreate, err := client.CollectionCreate("test collection created by veadk-go")
	if err != nil {
		t.Fatal(err)
	}
	if respCreate == nil || respCreate.Code != 0 {
		t.Fatal("create failed")
	}
	respInfo, err := client.CollectionInfo()
	if err != nil {
		t.Fatal(err)
	}
	respBytes, _ := json.Marshal(respInfo)
	t.Log("CollectionInfo respInfo = ", string(respBytes))

	if respInfo == nil || respInfo.Code != 0 {
		t.Fatal("info failed")
	}
	respDel, err := client.CollectionDelete()
	if err != nil {
		t.Fatal(err)
	}
	if respDel == nil || respDel.Code != 0 {
		t.Fatal("delete failed")
	}
}

func TestClient_DocumentListAndChunkList(t *testing.T) {
	client := getClientOrSkip(t, "sjy_test_coffee_kg")
	docs, err := client.DocumentList(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if docs == nil || docs.Code != 0 {
		t.Fatal("document list failed")
	}
	t.Log("docs = ", docs)

	chunks, err := client.ChunkList(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if chunks == nil || chunks.Code != 0 {
		t.Fatal("chunk list failed")
	}
	t.Log("chunks = ", chunks)
}

func TestClient_DocumentAddAndDelete_TOS(t *testing.T) {
	tosPath := os.Getenv("TOS_TEST_PATH")
	if tosPath == "" {
		t.Skip("missing env TOS_TEST_PATH")
	}
	idx := fmt.Sprintf("veadk_doc_test_%d", time.Now().UnixNano())
	client := getClientOrSkip(t, idx)
	_, _ = client.CollectionDelete()
	_, err := client.CollectionCreate("doc test")
	if err != nil {
		t.Fatal(err)
	}
	addResp, err := client.DocumentAddTOS(tosPath)
	if err != nil {
		t.Fatal(err)
	}
	if addResp == nil || addResp.Code != 0 {
		t.Fatal("add doc failed")
	}
	docs, err := client.DocumentList(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs.Data.DocList) == 0 {
		t.Fatal("no docs returned")
	}
	docID, ok := docs.Data.DocList[0]["doc_id"].(string)
	if !ok || docID == "" {
		t.Fatal("invalid doc id")
	}
	delResp, err := client.DocumentDelete(docID)
	if err != nil {
		t.Fatal(err)
	}
	if delResp == nil || delResp.Code != 0 {
		t.Fatal("delete doc failed")
	}
	_, _ = client.CollectionDelete()
}

func TestParseJsonUseNumber(t *testing.T) {
	input := []byte(`{"code":0,"message":"ok","data":{}}`)
	var resp *CommonResponse
	err := ParseJsonUseNumber(input, &resp)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Code != 0 {
		t.Fatal(errors.New("decode failed"))
	}
}
