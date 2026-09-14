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

package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/integrations/ve_sign"
)

// RemoteSkill identifies an immutable remote revision. Do not include credentials
// or temporary signed URLs in descriptors; descriptors become cache identities.
type RemoteSkill struct {
	Name        string
	Description string
	ID          string
	Version     string
	Bucket      string
	ObjectKey   string
}

// SkillSource discovers metadata without downloading archives. Implementations
// must respect context cancellation and return an error for incomplete listings.
type SkillSource interface {
	Identity() string
	Discover(context.Context) ([]RemoteSkill, error)
	Download(context.Context, RemoteSkill, io.Writer) error
}

// RemoteSourceConfig uses explicit endpoints to avoid conflating the Sandbox
// listen address with the AgentKit API host. Empty fields select Volcengine defaults.
type RemoteSourceConfig struct {
	// MaxAttempts bounds control-plane retries on 429 and transient 5xx; default 2.
	MaxAttempts             int
	SpaceID                 string
	Endpoint                string
	Region                  string
	Service                 string
	Credentials             veauth.CredentialSource
	HTTPClient              *http.Client
	TOSRegion               string
	TOSEndpoint             string
	UseTemporaryDownloadURL bool
	// AllowedDownloadHosts restricts temporary URLs to exact HTTPS hosts (including
	// port). Required when UseTemporaryDownloadURL is enabled; redirects are refused.
	AllowedDownloadHosts []string
	Timeout              time.Duration
}

type RemoteSource struct {
	config   RemoteSourceConfig
	endpoint *url.URL
	client   *http.Client
}

func NewRemoteSource(c RemoteSourceConfig) (*RemoteSource, error) {
	c.SpaceID = strings.TrimSpace(c.SpaceID)
	if c.SpaceID == "" || strings.ContainsAny(c.SpaceID, ",/\\\x00") {
		return nil, errors.New("invalid skill space ID")
	}
	if strings.HasPrefix(c.SpaceID, "sp-") {
		return nil, errors.New("SkillHub spaces are not supported")
	}
	if c.Region == "" {
		c.Region = "cn-beijing"
	}
	if c.Service == "" {
		c.Service = "agentkit"
	}
	if c.Endpoint == "" {
		c.Endpoint = "https://agentkit." + c.Region + ".volcengineapi.com"
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid skill API endpoint")
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 2
	}
	if c.MaxAttempts < 1 || c.MaxAttempts > 3 {
		return nil, errors.New("invalid retry limit")
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	if c.Timeout < 0 {
		return nil, errors.New("invalid remote source limits")
	}
	if c.UseTemporaryDownloadURL && len(c.AllowedDownloadHosts) == 0 {
		return nil, errors.New("temporary downloads require allowed hosts")
	}
	c.AllowedDownloadHosts = append([]string(nil), c.AllowedDownloadHosts...)
	if c.Credentials == nil {
		c.Credentials = veauth.NewRoleCredentialSource(veauth.RoleCredentialSourceConfig{})
	}
	client := http.Client{}
	if c.HTTPClient != nil {
		client = *c.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &RemoteSource{config: c, endpoint: u, client: &client}, nil
}

func (s *RemoteSource) Identity() string {
	return s.endpoint.Scheme + "://" + s.endpoint.Host + "/" + s.config.Region + "/" + s.config.Service + "/" + s.config.SpaceID
}

type remoteItem struct {
	Name         string
	Description  string
	SkillID      string
	BucketName   string
	TosPath      string
	Version      string
	SkillVersion string
}

// Discover uses the unpaginated ListSkillsBySpaceId contract from veadk-python.
func (s *RemoteSource) Discover(ctx context.Context) ([]RemoteSkill, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	body := map[string]any{"SkillSpaceId": s.config.SpaceID, "InnerTags": map[string]string{"source": "sandbox"}}
	data, err := s.request(ctx, "/", "ListSkillsBySpaceId", body, 4<<20)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result           json.RawMessage
		ResponseMetadata struct{ Error json.RawMessage }
	}
	if json.Unmarshal(data, &envelope) != nil || (len(envelope.ResponseMetadata.Error) > 0 && string(envelope.ResponseMetadata.Error) != "null") {
		return nil, errors.New("invalid skill list response")
	}
	payload := data
	if len(envelope.Result) > 0 && string(envelope.Result) != "null" {
		payload = envelope.Result
	}
	var listing struct {
		Items      []remoteItem
		TotalCount *int
	}
	if json.Unmarshal(payload, &listing) != nil || listing.Items == nil {
		return nil, errors.New("invalid skill list response")
	}
	if listing.TotalCount != nil && *listing.TotalCount != len(listing.Items) {
		return nil, errors.New("incomplete skill list response")
	}
	result := make([]RemoteSkill, 0, len(listing.Items))
	seen := map[string]bool{}
	for _, item := range listing.Items {
		d := RemoteSkill{Name: item.Name, Description: item.Description, ID: item.SkillID, Version: item.Version, Bucket: item.BucketName, ObjectKey: item.TosPath}
		if d.Version == "" {
			d.Version = item.SkillVersion
		}
		if err := validateRemote(d); err != nil {
			return nil, err
		}
		if d.Bucket == "" || d.ObjectKey == "" {
			return nil, errors.New("incomplete remote skill descriptor")
		}
		if seen[d.Name] {
			return nil, ErrDuplicateSkill
		}
		seen[d.Name] = true
		result = append(result, d)
	}
	return result, nil
}

func validateRemote(d RemoteSkill) error {
	if !validNameRegex.MatchString(d.Name) || len(d.Name) > 64 || len(d.Description) > 1024 || len(d.ID) > 4096 || len(d.Version) > 4096 || len(d.ObjectKey) > 8192 || len(d.Bucket) > 255 {
		return errors.New("invalid remote skill metadata")
	}
	return nil
}

func (s *RemoteSource) request(ctx context.Context, path, action string, body any, limit int64) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		data, err := s.requestOnce(ctx, path, action, body, limit)
		var status *ve_sign.HTTPError
		if err == nil || attempt+1 >= s.config.MaxAttempts || !errors.As(err, &status) || (status.StatusCode != 429 && status.StatusCode != 500 && status.StatusCode != 502 && status.StatusCode != 503 && status.StatusCode != 504) {
			return data, err
		}
		timer := time.NewTimer(time.Duration(50*(1<<attempt)) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
func (s *RemoteSource) requestOnce(ctx context.Context, path, action string, body any, limit int64) ([]byte, error) {
	cred, err := s.config.Credentials.Credential(ctx)
	if err != nil {
		return nil, safeRemoteError(ctx, "skill credential unavailable")
	}
	vr := ve_sign.VeRequest{AK: cred.AccessKeyID, SK: cred.SecretAccessKey, Method: http.MethodPost, Scheme: s.endpoint.Scheme, Host: s.endpoint.Host, Path: path, Service: s.config.Service, Region: s.config.Region, Body: body, Header: map[string]string{"X-Security-Token": cred.SessionToken}}
	vr.Action = action
	vr.Version = "2025-10-30"
	req, err := vr.BuildRequestWithContext(ctx)
	if err != nil {
		return nil, safeRemoteError(ctx, "skill signing failed")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, safeRemoteError(ctx, "skill request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &ve_sign.HTTPError{StatusCode: resp.StatusCode}
	}
	data, err := readBoundedContext(ctx, resp.Body, limit, errors.New("skill response limit exceeded"))
	if err != nil {
		return nil, safeRemoteError(ctx, "skill response read failed or exceeded limit")
	}
	return data, nil
}

func safeRemoteError(ctx context.Context, message string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New(message)
}

func (s *RemoteSource) Download(ctx context.Context, d RemoteSkill, w io.Writer) error {
	w = &boundedWriter{w: w, left: 64 << 20}
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	if err := validateRemote(d); err != nil {
		return err
	}
	if s.config.UseTemporaryDownloadURL {
		return s.downloadTemporary(ctx, d, w)
	}
	cred, err := s.config.Credentials.Credential(ctx)
	if err != nil {
		return safeRemoteError(ctx, "skill credential unavailable")
	}
	tc := tos.NewStaticCredentials(cred.AccessKeyID, cred.SecretAccessKey)
	tc.WithSecurityToken(cred.SessionToken)
	region := s.config.TOSRegion
	if region == "" {
		region = s.config.Region
	}
	endpoint := s.config.TOSEndpoint
	if endpoint == "" {
		endpoint = "https://tos-" + region + ".volces.com"
	}
	transport := s.client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	cli, err := tos.NewClientV2(endpoint, tos.WithRegion(region), tos.WithCredentials(tc), tos.WithHTTPTransport(transport), tos.WithMaxRetryCount(0))
	if err != nil {
		return errors.New("invalid TOS configuration")
	}
	defer cli.Close()
	out, err := cli.GetObjectV2(ctx, &tos.GetObjectV2Input{Bucket: d.Bucket, Key: d.ObjectKey})
	if err != nil {
		return safeRemoteError(ctx, "skill TOS download failed")
	}
	defer func() { _ = out.Content.Close() }()
	_, err = io.Copy(w, &contextReader{ctx: ctx, r: out.Content})
	if err != nil {
		return safeRemoteError(ctx, "skill archive read or write failed")
	}
	return nil
}

func (s *RemoteSource) downloadTemporary(ctx context.Context, d RemoteSkill, w io.Writer) error {
	parts := strings.Split(d.ObjectKey, "/")
	if len(parts) < 3 {
		return errors.New("invalid skill object key")
	}
	id := d.ID
	if id == "" {
		id = parts[1]
	}
	data, err := s.request(ctx, "/", "GenTempTosObjectDownloadUrl", map[string]string{"SkillId": id, "SkillVersion": parts[2]}, 4<<20)
	if err != nil {
		return err
	}
	var response struct {
		Result struct {
			SignedURL string `json:"SignedUrl"`
		}
	}
	if json.Unmarshal(data, &response) != nil {
		return errors.New("invalid skill download response")
	}
	u, err := url.Parse(response.Result.SignedURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return errors.New("invalid skill download URL")
	}
	allowed := false
	for _, host := range s.config.AllowedDownloadHosts {
		if strings.EqualFold(host, u.Host) {
			allowed = true
		}
	}
	if !allowed {
		return errors.New("skill download host not allowed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return errors.New("invalid skill download request")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return safeRemoteError(ctx, "skill download failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("skill download status %d", resp.StatusCode)
	}
	_, err = io.Copy(w, &contextReader{ctx: ctx, r: resp.Body})
	if err != nil {
		return safeRemoteError(ctx, "skill archive read or write failed")
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
