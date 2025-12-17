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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/volcengine/volc-sdk-golang/base"
)

const HttpClientTimeoutTime = 10

var ErrVeRequestParam = errors.New("VeRequest Param Invalid Error")

type VeRequest struct {
	AK      string
	SK      string
	Method  string
	Host    string
	Path    string
	Service string
	Region  string
	Header  map[string]string
	Queries map[string]string
	Body    interface{}
}

func (v VeRequest) validate() error {
	if v.AK == "" || v.SK == "" {
		return ErrVeRequestParam
	}
	m := strings.ToUpper(v.Method)
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions:
	default:
		return ErrVeRequestParam
	}
	if v.Host == "" || strings.Contains(v.Host, "/") {
		return ErrVeRequestParam
	}
	if v.Path == "" || !strings.HasPrefix(v.Path, "/") {
		return ErrVeRequestParam
	}
	if v.Service == "" || v.Region == "" {
		return ErrVeRequestParam
	}
	if (m == http.MethodPost || m == http.MethodPut || m == http.MethodPatch) && v.Body == nil {
		return ErrVeRequestParam
	}
	return nil
}

func (v VeRequest) DoRequest() ([]byte, error) {
	req, err := v.buildSignRequest()
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: HttpClientTimeoutTime * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		//Try to parse error response
		return respBody, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil

}

func (v VeRequest) buildSignRequest() (*http.Request, error) {
	err := v.validate()
	if err != nil {
		return nil, err
	}

	paramsBytes, err := serializeToJsonBytes(v.Body)
	if err != nil {
		return nil, err
	}

	u := url.URL{
		Scheme: "https",
		Host:   v.Host,
		Path:   v.Path,
	}

	if len(v.Queries) > 0 {
		queries := make(url.Values)
		for key, value := range v.Header {
			queries.Set(key, value)
		}
		v.Host = fmt.Sprintf("%s/?%s", v.Host, queries.Encode())
	}

	req, _ := http.NewRequest(strings.ToUpper(v.Method), u.String(), bytes.NewReader(paramsBytes))
	req.Header.Add("Host", v.Host)
	if len(v.Header) > 0 {
		for key, value := range v.Header {
			req.Header.Add(key, value)
		}
	}
	credential := base.Credentials{
		AccessKeyID:     v.AK,
		SecretAccessKey: v.SK,
		Service:         v.Service,
		Region:          v.Region,
	}
	req = credential.Sign(req)
	return req, nil
}

func serializeToJsonBytes(source interface{}) ([]byte, error) {
	buf := new(bytes.Buffer)
	encoder := json.NewEncoder(buf)
	err := encoder.Encode(source)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
