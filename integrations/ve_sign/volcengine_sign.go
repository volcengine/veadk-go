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

package ve_sign

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/volcengine/veadk-go/log"
)

const (
	HttpsSchema = "https"
	HttpSchema  = "http"
)

var ErrVeRequestParam = errors.New("VeRequest Param Invalid Error")

type VeRequest struct {
	AK      string
	SK      string
	Method  string
	Scheme  string
	Host    string
	Path    string
	Service string
	Region  string
	Action  string
	Version string
	Header  map[string]string
	Queries map[string]string
	Body    interface{}
	Timeout uint
}

func (vr VeRequest) validate() error {
	if vr.AK == "" || vr.SK == "" {
		return fmt.Errorf("%w: VOLCENGINE_ACCESS_KEY or VOLCENGINE_SECRET_KEY not set", ErrVeRequestParam)
	}
	m := strings.ToUpper(vr.Method)
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions:
	default:
		return fmt.Errorf("%w: %s method is invalid", ErrVeRequestParam, vr.Method)
	}
	if vr.Host == "" || strings.Contains(vr.Host, "/") {
		return fmt.Errorf("%w: Host {%s} is invalid", ErrVeRequestParam, vr.Host)
	}
	if vr.Path != "" && !strings.HasPrefix(vr.Path, "/") {
		return fmt.Errorf("%w: Ptah {%s} is invalid", ErrVeRequestParam, vr.Path)
	}
	if vr.Service == "" || vr.Region == "" {
		return fmt.Errorf("%w: Service or Region is empty", ErrVeRequestParam)
	}
	if (m == http.MethodPost || m == http.MethodPut || m == http.MethodPatch) && vr.Body == nil {
		return fmt.Errorf("%w: body can not be nil", ErrVeRequestParam)
	}
	if vr.Scheme != HttpsSchema && vr.Scheme != HttpSchema {
		return fmt.Errorf("%w: %s Scheme invalid", ErrVeRequestParam, vr.Scheme)
	}
	return nil
}

func (vr VeRequest) buildSignRequest() (*http.Request, error) {
	if vr.Scheme != HttpsSchema && vr.Scheme != HttpSchema {
		vr.Scheme = HttpsSchema
	}

	err := vr.validate()
	if err != nil {
		return nil, err
	}

	queries := make(url.Values)
	queries.Set("Action", vr.Action)
	queries.Set("Version", vr.Version)
	if len(vr.Queries) > 0 {
		for key, value := range vr.Queries {
			queries.Set(key, value)
		}
	}
	requestAddr := fmt.Sprintf("%s://%s%s?%s", vr.Scheme, vr.Host, vr.Path, queries.Encode())
	bodyBytes, _ := json.Marshal(vr.Body)

	request, err := http.NewRequest(vr.Method, requestAddr, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("VeRequest.v2 NewRequest bad request: %w", err)
	}

	now := time.Now()
	date := now.UTC().Format("20060102T150405Z")
	authDate := date[:8]
	request.Header.Set("X-Date", date)

	payload := hex.EncodeToString(hashSHA256(bodyBytes))
	request.Header.Set("X-Content-Sha256", payload)
	request.Header.Set("Content-Type", "application/json")
	for k, v := range vr.Header {
		request.Header.Set(k, v)
	}

	queryString := strings.ReplaceAll(queries.Encode(), "+", "%20")
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
		vr.Method,
		vr.Path,
		queryString,
		headerString + "\n",
		strings.Join(signedHeaders, ";"),
		payload,
	}, "\n")

	hashedCanonicalString := hex.EncodeToString(hashSHA256([]byte(canonicalString)))

	credentialScope := authDate + "/" + vr.Region + "/" + vr.Service + "/request"
	signString := strings.Join([]string{
		"HMAC-SHA256",
		date,
		credentialScope,
		hashedCanonicalString,
	}, "\n")

	signedKey := getSignedKey(vr.SK, authDate, vr.Region, vr.Service)
	signature := hex.EncodeToString(hmacSHA256(signedKey, signString))

	authorization := "HMAC-SHA256" +
		" Credential=" + vr.AK + "/" + credentialScope +
		", SignedHeaders=" + strings.Join(signedHeaders, ";") +
		", Signature=" + signature
	request.Header.Set("Authorization", authorization)
	return request, nil
}

func (vr VeRequest) DoRequest() ([]byte, error) {
	req, err := vr.buildSignRequest()
	if err != nil {
		return nil, err
	}
	var client = http.DefaultClient
	if vr.Timeout > 0 {
		client = &http.Client{Timeout: time.Duration(vr.Timeout) * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("VeRequest.v2 do request err: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return respBody, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
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
		log.Errorf("input hash err:%s", err.Error())
	}

	return hash.Sum(nil)
}
