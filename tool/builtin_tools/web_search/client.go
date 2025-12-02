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

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const (
	Path = "/"
)

type Config struct {
	Host    string
	Service string
	Region  string
	Method  string
	Action  string
	Version string
}
type Client struct {
	config *Config
}

func NewClient() *Client {
	return &Client{
		config: &Config{
			Host:    "https://mercury.volcengineapi.com",
			Service: "volc_torchlight_api",
			Region:  "cn-beijing",
			Method:  http.MethodPost,
			Action:  "WebSearch",
			Version: "2025-01-01",
		},
	}
}

func (c *Client) DoRequest(ak, sk string, header map[string]string, body []byte) (*WebSearchResponse, error) {
	var result *WebSearchResponse

	queries := make(url.Values)
	queries.Set("Action", c.config.Action)
	queries.Set("Version", c.config.Version)
	requestAddr := fmt.Sprintf("%s%s?%s", c.config.Host, Path, queries.Encode())
	log.Printf("request addr: %s\n", requestAddr)

	request, err := http.NewRequest(c.config.Method, requestAddr, bytes.NewBuffer(body))
	if err != nil {
		return result, fmt.Errorf("web search bad request: %w", err)
	}

	now := time.Now()
	date := now.UTC().Format("20060102T150405Z")
	authDate := date[:8]
	request.Header.Set("X-Date", date)

	payload := hex.EncodeToString(hashSHA256(body))
	request.Header.Set("X-Content-Sha256", payload)
	request.Header.Set("Content-Type", "application/json")
	if header != nil {
		for k, v := range header {
			request.Header.Set(k, v)
		}
	}

	queryString := strings.Replace(queries.Encode(), "+", "%20", -1)
	signedHeaders := []string{"host", "x-date", "x-content-sha256", "content-type"}
	var headerList []string
	for _, h := range signedHeaders {
		if h == "host" {
			headerList = append(headerList, h+":"+request.Host)
		} else {
			v := request.Header.Get(h)
			headerList = append(headerList, h+":"+strings.TrimSpace(v))
		}
	}
	headerString := strings.Join(headerList, "\n")

	canonicalString := strings.Join([]string{
		c.config.Method,
		Path,
		queryString,
		headerString + "\n",
		strings.Join(signedHeaders, ";"),
		payload,
	}, "\n")
	log.Printf("canonical string:\n%s\n", canonicalString)

	hashedCanonicalString := hex.EncodeToString(hashSHA256([]byte(canonicalString)))
	log.Printf("hashed canonical string: %s\n", hashedCanonicalString)

	credentialScope := authDate + "/" + c.config.Region + "/" + c.config.Service + "/request"
	signString := strings.Join([]string{
		"HMAC-SHA256",
		date,
		credentialScope,
		hashedCanonicalString,
	}, "\n")
	log.Printf("sign string:\n%s\n", signString)

	signedKey := getSignedKey(sk, authDate, c.config.Region, c.config.Service)
	signature := hex.EncodeToString(hmacSHA256(signedKey, signString))
	log.Printf("signature: %s\n", signature)

	authorization := "HMAC-SHA256" +
		" Credential=" + ak + "/" + credentialScope +
		", SignedHeaders=" + strings.Join(signedHeaders, ";") +
		", Signature=" + signature
	request.Header.Set("Authorization", authorization)
	log.Printf("authorization: %s\n", authorization)

	requestRaw, err := httputil.DumpRequest(request, true)
	if err != nil {
		return result, fmt.Errorf("web search dump request err: %w", err)
	}

	log.Printf("request:\n%s\n", string(requestRaw))

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return result, fmt.Errorf("web search do request err: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		log.Printf("response status bad code: %v", response.StatusCode)
		return result, fmt.Errorf("web search get bad response code: %v", response.StatusCode)
	}

	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("web search unmarshal response err: %w", err)
	}

	return result, nil
}

func hmacSHA256(key []byte, content string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(content))
	return mac.Sum(nil)
}

func getSignedKey(secretKey, date, region, service string) []byte {
	kDate := hmacSHA256([]byte(secretKey), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "request")

	return kSigning
}

func hashSHA256(data []byte) []byte {
	hash := sha256.New()
	if _, err := hash.Write(data); err != nil {
		log.Printf("input hash err:%s", err.Error())
	}

	return hash.Sum(nil)
}
