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

package web_search

import "time"

type ResponseMetadata struct {
	RequestID string `json:"RequestId"`
	Action    string `json:"Action"`
	Version   string `json:"Version"`
	Service   string `json:"Service"`
	Region    string `json:"Region"`
}
type WebResults struct {
	ID            string    `json:"Id"`
	SortID        int       `json:"SortId"`
	Title         string    `json:"Title"`
	SiteName      string    `json:"SiteName"`
	URL           string    `json:"Url"`
	Snippet       string    `json:"Snippet"`
	Summary       string    `json:"Summary"`
	Content       string    `json:"Content"`
	PublishTime   time.Time `json:"PublishTime"`
	LogoURL       string    `json:"LogoUrl"`
	RankScore     float64   `json:"RankScore"`
	AuthInfoDes   string    `json:"AuthInfoDes"`
	AuthInfoLevel int       `json:"AuthInfoLevel"`
}

type SearchContext struct {
	OriginQuery string `json:"OriginQuery"`
	SearchType  string `json:"SearchType"`
}

type Result struct {
	ResultCount   int           `json:"ResultCount"`
	WebResults    []WebResults  `json:"WebResults"`
	SearchContext SearchContext `json:"SearchContext"`
	TimeCost      int           `json:"TimeCost"`
	LogID         string        `json:"LogId"`
	Choices       interface{}   `json:"Choices"`
	Usage         interface{}   `json:"Usage"`
	ImageResults  interface{}   `json:"ImageResults"`
	CardResults   interface{}   `json:"CardResults"`
}

type WebSearchResponse struct {
	ResponseMetadata ResponseMetadata `json:"ResponseMetadata"`
	Result           Result           `json:"Result"`
}
