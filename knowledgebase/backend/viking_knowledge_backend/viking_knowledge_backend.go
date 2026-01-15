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

package viking_knowledge_backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/volcengine/veadk-go/integrations/ve_tos"
	"github.com/volcengine/veadk-go/integrations/ve_viking"
	"github.com/volcengine/veadk-go/integrations/ve_viking/viking_knowledge"
	"github.com/volcengine/veadk-go/knowledgebase/interface"
	"github.com/volcengine/veadk-go/knowledgebase/ktypes"
	"github.com/volcengine/veadk-go/log"
	"github.com/volcengine/veadk-go/utils"
)

const (
	TosBucketPath = "knowledgebase"
)

var (
	DefaultTopK                int32 = 5
	DefaultChunkDiffusionCount int32 = 0
	DefaultRerank              bool  = true
)

var ErrNewVikingKnowledgeBase = errors.New("NewVikingKnowledgeBase error")
var ErrVikingKnowledgeBaseSearch = errors.New("VikingKnowledgeBase search error")
var ErrVikingKnowledgeBaseAddDocs = errors.New("VikingKnowledgeBase add docs error")

type Config struct {
	AK                  string
	SK                  string
	SessionToken        string
	Index               string
	Project             string
	Region              string
	CreateIfNotExist    bool
	TopK                int32
	ChunkDiffusionCount *int32
	Rerank              *bool
	TosConfig           *ve_tos.Config
}

type VikingKnowledgeBackend struct {
	viking *viking_knowledge.Client
	tos    *ve_tos.Client
	config *Config
}

func NewVikingKnowledgeBackend(cfg *Config) (_interface.KnowledgeBackend, error) {
	client, err := viking_knowledge.New(&ve_viking.ClientConfig{
		Index:        cfg.Index,
		Project:      cfg.Project,
		Region:       cfg.Region,
		AK:           cfg.AK,
		SK:           cfg.SK,
		SessionToken: cfg.SessionToken,
	})
	if err != nil {
		return nil, fmt.Errorf("%w : %w", ErrNewVikingKnowledgeBase, err)
	}

	collectionInfo, err := client.CollectionInfo()
	if err != nil {
		return nil, fmt.Errorf("%w : get viking collection info error: %w", ErrNewVikingKnowledgeBase, err)
	}

	if collectionInfo.Code == ve_viking.VikingKnowledgeBaseIndexNotExistCode {
		if cfg.CreateIfNotExist {
			_, err := client.CollectionCreate("")
			if err != nil {
				return nil, fmt.Errorf("%w : create viking knowledge index error: %w", ErrNewVikingKnowledgeBase, err)
			}
			log.Info("Create viking knowledge index", cfg.Index, "successfully")
		} else {
			return nil, fmt.Errorf("%w : viking index not exist", ErrNewVikingKnowledgeBase)
		}
	}

	if cfg.TopK <= 0 {
		cfg.TopK = DefaultTopK
	}
	if cfg.ChunkDiffusionCount == nil {
		cfg.ChunkDiffusionCount = &DefaultChunkDiffusionCount
	}
	if cfg.Rerank == nil {
		cfg.Rerank = &DefaultRerank
	}

	// new tos client
	if cfg.TosConfig == nil {
		cfg.TosConfig = &ve_tos.Config{}
	}
	cfg.TosConfig.AK = cfg.AK
	cfg.TosConfig.SK = cfg.SK
	cfg.TosConfig.SessionToken = cfg.SessionToken

	tosClient, err := ve_tos.New(cfg.TosConfig)
	if err != nil {
		return nil, fmt.Errorf("%w : new tos client error: %w", ErrNewVikingKnowledgeBase, err)
	}
	return &VikingKnowledgeBackend{
		viking: client,
		tos:    tosClient,
		config: cfg,
	}, nil
}

func (v *VikingKnowledgeBackend) Search(query string, opts ...map[string]any) ([]ktypes.KnowledgeEntry, error) {
	chunks, err := v.viking.SearchKnowledge(
		query,
		utils.ExtractOptsValueWithDefault[int32]("topK", v.config.TopK, opts...),
		utils.ExtractOptsValueWithDefault[bool]("rerank", *v.config.Rerank, opts...),
		utils.ExtractOptsValueWithDefault[int32]("chunkDiffusionCount", *v.config.ChunkDiffusionCount, opts...),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("%w : %w", ErrVikingKnowledgeBaseSearch, err)
	}

	if chunks.Code != ve_viking.VikingKnowledgeBaseSuccessCode {
		return nil, fmt.Errorf("%w : with bad code %d, message:%s", ErrVikingKnowledgeBaseSearch, chunks.Code, chunks.Message)
	}

	var entries []ktypes.KnowledgeEntry
	for _, item := range chunks.Data.ResultList {
		var metadata = make(map[string]any)
		if item.DocInfo.DocMeta != "" {
			err = json.Unmarshal([]byte(item.DocInfo.DocMeta), &metadata)
			if err != nil {
				return nil, fmt.Errorf("%w : Unmarshal DocMeta error:%w", ErrVikingKnowledgeBaseSearch, err)
			}
		}
		entries = append(entries, ktypes.KnowledgeEntry{
			Content:  item.Content,
			Metadata: metadata,
		})
	}

	return entries, nil
}

func (v *VikingKnowledgeBackend) Index() string {
	return v.config.Index
}

func (v *VikingKnowledgeBackend) AddFromText(text []string, opts ...map[string]any) error {
	for _, t := range text {
		objectKey := v.tos.BuildObjectKeyForText(TosBucketPath)
		err := v.tos.UploadText(t, objectKey, nil)
		if err != nil {
			return fmt.Errorf("%w :UploadText error: %w", ErrVikingKnowledgeBaseAddDocs, err)
		}
		tosUrl := v.tos.BuildTOSURL(objectKey)
		_, err = v.viking.DocumentAddTOS(tosUrl)
		if err != nil {
			return fmt.Errorf("%w : DocumentAddTOS from %s error: %w", ErrVikingKnowledgeBaseAddDocs, tosUrl, err)
		}
	}
	return nil
}

func (v *VikingKnowledgeBackend) AddFromFiles(files []string, opts ...map[string]any) error {
	for _, f := range files {
		objectKey := v.tos.BuildObjectKeyForFile(f, TosBucketPath)
		err := v.tos.UploadFile(f, objectKey, nil)
		if err != nil {
			return fmt.Errorf("%w :UploadFile error: %w", ErrVikingKnowledgeBaseAddDocs, err)
		}
		tosUrl := v.tos.BuildTOSURL(objectKey)
		_, err = v.viking.DocumentAddTOS(tosUrl)
		if err != nil {
			return fmt.Errorf("%w : DocumentAddTOS from %s error: %w", ErrVikingKnowledgeBaseAddDocs, tosUrl, err)
		}
	}
	return nil
}

func (v *VikingKnowledgeBackend) AddFromDirectory(directory string, opts ...map[string]any) error {
	files, err := getFilesInDirectory(directory)
	if err != nil {
		return fmt.Errorf("%w : AddFromDirectory error: %w", ErrVikingKnowledgeBaseAddDocs, err)
	}
	log.Info(fmt.Sprintf("Add from files: %+v", files))
	return v.AddFromFiles(files, opts...)
}

func getFilesInDirectory(directory string) ([]string, error) {
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("the directory does not exist: %s", directory)
	}

	var files []string
	err = filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
