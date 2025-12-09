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

package ve_tos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos/enum"
	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/common"
	"github.com/volcengine/veadk-go/configs"
	"github.com/volcengine/veadk-go/utils"
)

var bucketRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)
var (
	TosConfigInvalidErr = errors.New("tos client config is invalid")
	TosBucketInvalidErr = errors.New("tos bucket invalid, bucket names must be 3-63 characters long, contain only lowercase letters, numbers , and hyphens (-), start and end with a letter or number")
	TosClientInvalidErr = errors.New("TOS client is not initialized")
)

func preCheckBucket(bucket string) error {
	if !bucketRe.MatchString(bucket) {
		return TosBucketInvalidErr
	}
	return nil
}

type Config struct {
	AK           string `validate:"required"`
	SK           string `validate:"required"`
	SessionToken string `validate:"omitempty"`
	Region       string `validate:"required"`
	Endpoint     string `validate:"required"`
	Bucket       string `validate:"required"`
}

func (c *Config) validate() error {
	var validate = validator.New()
	if err := validate.Struct(c); err != nil {
		for _, err := range err.(validator.ValidationErrors) {
			return fmt.Errorf("field %s validation failed: %s（rule: %s）", err.Field(), err.Tag(), err.Param())
		}
	}
	if err := preCheckBucket(c.Bucket); err != nil {
		return err
	}
	return nil
}

type Client struct {
	config *Config
	client *tos.ClientV2
}

func New(cfg *Config) (*Client, error) {
	if cfg.AK == "" {
		cfg.AK = utils.GetEnvWithDefault(common.VOLCENGINE_ACCESS_KEY, configs.GetGlobalConfig().Volcengine.AK)
	}
	if cfg.SK == "" {
		cfg.SK = utils.GetEnvWithDefault(common.VOLCENGINE_SECRET_KEY, configs.GetGlobalConfig().Volcengine.SK)
	}
	if cfg.AK == "" || cfg.SK == "" {
		iam, err := veauth.GetCredentialFromVeFaaSIAM()
		if err != nil {
			return nil, fmt.Errorf("%w : GetCredential error: %w", TosConfigInvalidErr, err)
		}
		cfg.AK = iam.AccessKeyID
		cfg.SK = iam.SecretAccessKey
		cfg.SessionToken = iam.SessionToken
	}

	if cfg.Region == "" {
		cfg.Region = utils.GetEnvWithDefault(common.DATABASE_TOS_REGION, configs.GetGlobalConfig().Database.TOS.Region, common.DEFAULT_DATABASE_TOS_REGION)
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = utils.GetEnvWithDefault(common.DATABASE_TOS_ENDPOINT, configs.GetGlobalConfig().Database.TOS.Endpoint, fmt.Sprintf("https://tos-%s.volces.com", cfg.Region))
	}
	if cfg.Bucket == "" {
		cfg.Bucket = utils.GetEnvWithDefault(common.DATABASE_TOS_BUCKET, configs.GetGlobalConfig().Database.TOS.Bucket)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	cred := tos.NewStaticCredentials(cfg.AK, cfg.SK)
	if cfg.SessionToken != "" {
		cred.WithSecurityToken(cfg.SessionToken)
	}

	client, err := tos.NewClientV2(cfg.Endpoint,
		tos.WithRegion(cfg.Region),
		tos.WithCredentials(cred))

	if err != nil {
		return nil, err
	}

	return &Client{
		config: cfg,
		client: client,
	}, nil
}

func (c *Client) CreateBucket(ctx context.Context) error {
	if err := preCheckBucket(c.config.Bucket); err != nil {
		return err
	}
	_, err := c.client.CreateBucketV2(ctx, &tos.CreateBucketV2Input{
		Bucket:       c.config.Bucket,
		ACL:          enum.ACLPublicRead,
		StorageClass: enum.StorageClassStandard,
	})
	if err != nil {
		return fmt.Errorf("CreateBucket error: %v", err)
	}
	//Set CORS rules
	_, err = c.client.PutBucketCORS(ctx, &tos.PutBucketCORSInput{
		Bucket: c.config.Bucket,
		CORSRules: []tos.CorsRule{
			tos.CorsRule{
				AllowedOrigin: []string{"*"},
				AllowedMethod: []string{"GET", "HEAD"},
				AllowedHeader: []string{"*"},
				MaxAgeSeconds: 1000,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("PutBucketCORS error: %v", err)
	}
	return nil
}

func (c *Client) BucketExist(ctx context.Context) (bool, error) {
	if err := preCheckBucket(c.config.Bucket); err != nil {
		return false, err
	}
	_, err := c.client.HeadBucket(ctx, &tos.HeadBucketInput{
		Bucket: c.config.Bucket,
	})
	if err != nil {
		var serverErr *tos.TosServerError
		if errors.As(err, &serverErr) {
			if serverErr.StatusCode == http.StatusNotFound {
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}

func (c *Client) DeleteBucket(ctx context.Context) error {
	if err := preCheckBucket(c.config.Bucket); err != nil {
		return err
	}
	_, err := c.client.DeleteBucket(ctx, &tos.DeleteBucketInput{
		Bucket: c.config.Bucket,
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) BuildObjectKeyForFile(dataPath string, bucketPath ...string) string {
	u, _ := url.Parse(dataPath)
	if u != nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "ftp" || u.Scheme == "ftps") {
		objectKey := strings.TrimPrefix(u.Host+u.Path, "/")
		return objectKey
	}
	absPath, _ := filepath.Abs(dataPath)
	cwd, _ := os.Getwd()
	relPath, err := filepath.Rel(cwd, absPath)
	var objectKey string
	if err == nil &&
		!strings.HasPrefix(relPath, "../") &&
		!strings.HasPrefix(relPath, `..\`) &&
		!strings.HasPrefix(relPath, "./") &&
		!strings.HasPrefix(relPath, `.\`) {
		objectKey = relPath
	} else {
		objectKey = filepath.Base(dataPath)
	}
	if strings.HasPrefix(objectKey, "/") {
		objectKey = objectKey[1:]
	}
	if objectKey == "" ||
		strings.Contains(objectKey, "../") ||
		strings.Contains(objectKey, `..\`) ||
		strings.Contains(objectKey, "./") ||
		strings.Contains(objectKey, `.\`) {
		objectKey = filepath.Base(dataPath)
	}
	if bucketPath != nil && len(bucketPath) > 0 {
		return fmt.Sprintf("%s/%s", bucketPath[0], objectKey)
	}
	return objectKey
}

func (c *Client) BuildObjectKeyForText(bucketPath ...string) string {
	if bucketPath != nil && len(bucketPath) > 0 {
		return fmt.Sprintf("%s/%s.%s", bucketPath[0], time.Now().Format("20060102150405.000"), "txt")
	}
	return fmt.Sprintf("%s.%s", time.Now().Format("20060102150405.000"), "txt")
}

func (c *Client) BuildObjectKeyForBytes(bucketPath ...string) string {
	if bucketPath != nil && len(bucketPath) > 0 {
		return fmt.Sprintf("%s/%s", bucketPath[0], time.Now().Format("20060102150405.000"))
	}
	return time.Now().Format("20060102150405.000")
}

func (c *Client) BuildTOSURL(objectKey string) string {
	return fmt.Sprintf("%s/%s", c.config.Bucket, objectKey)
}

func (c *Client) buildTOSSignedURL(objectKey string) (string, error) {
	if err := preCheckBucket(c.config.Bucket); err != nil {
		return "", err
	}
	out, err := c.client.PreSignedURL(&tos.PreSignedURLInput{
		HTTPMethod: http.MethodGet,
		Bucket:     c.config.Bucket,
		Key:        objectKey,
		Expires:    604800,
	})
	if err != nil {
		return "", err
	}
	return out.SignedUrl, nil
}

func (c *Client) ensureClientAndBucket() error {
	// todo refreshClient
	if c.client == nil {
		return TosClientInvalidErr
	}
	if exist, err := c.BucketExist(context.Background()); err != nil || !exist {
		if err := c.CreateBucket(context.Background()); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) UploadText(text string, objectKey string, metadata map[string]string) error {
	if err := preCheckBucket(c.config.Bucket); err != nil {
		return err
	}
	if objectKey == "" {
		objectKey = c.BuildObjectKeyForText()
	}
	if err := c.ensureClientAndBucket(); err != nil {
		return err
	}
	if _, err := c.client.PutObjectV2(context.Background(), &tos.PutObjectV2Input{
		PutObjectBasicInput: tos.PutObjectBasicInput{
			Bucket: c.config.Bucket,
			Key:    objectKey,
			Meta:   metadata,
		},
		Content: strings.NewReader(text),
	}); err != nil {
		return err
	}
	return nil
}

func (c *Client) AsyncUploadText(text string, objectKey string, metadata map[string]string) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- c.UploadText(text, objectKey, metadata)
		close(ch)
	}()
	return ch
}

func (c *Client) UploadBytes(data []byte, objectKey string, metadata map[string]string) error {
	if err := preCheckBucket(c.config.Bucket); err != nil {
		return err
	}
	if objectKey == "" {
		objectKey = c.BuildObjectKeyForBytes()
	}
	if err := c.ensureClientAndBucket(); err != nil {
		return err
	}
	if _, err := c.client.PutObjectV2(context.Background(), &tos.PutObjectV2Input{
		PutObjectBasicInput: tos.PutObjectBasicInput{
			Bucket: c.config.Bucket,
			Key:    objectKey,
			Meta:   metadata,
		},
		Content: strings.NewReader(string(data)),
	}); err != nil {
		return err
	}
	return nil
}

func (c *Client) AsyncUploadBytes(data []byte, objectKey string, metadata map[string]string) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- c.UploadBytes(data, objectKey, metadata)
		close(ch)
	}()
	return ch
}

func (c *Client) UploadFile(filePath string, objectKey string, metadata map[string]string) error {
	if err := preCheckBucket(c.config.Bucket); err != nil {
		return err
	}
	if objectKey == "" {
		objectKey = c.BuildObjectKeyForFile(filePath)
	}
	if err := c.ensureClientAndBucket(); err != nil {
		return err
	}
	if _, err := c.client.PutObjectFromFile(context.Background(), &tos.PutObjectFromFileInput{
		PutObjectBasicInput: tos.PutObjectBasicInput{
			Bucket: c.config.Bucket,
			Key:    objectKey,
			Meta:   metadata,
		},
		FilePath: filePath,
	}); err != nil {
		return err
	}
	return nil
}

func (c *Client) AsyncUploadFile(filePath string, objectKey string, metadata map[string]string) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- c.UploadFile(filePath, objectKey, metadata)
		close(ch)
	}()
	return ch
}

func (c *Client) UploadFiles(filePaths []string, objectKeys []string, metadata map[string]string) error {
	if err := preCheckBucket(c.config.Bucket); err != nil {
		return err
	}
	if objectKeys == nil {
		objectKeys = make([]string, 0, len(filePaths))
		for _, fp := range filePaths {
			objectKeys = append(objectKeys, c.BuildObjectKeyForFile(fp))
		}
	}
	if len(objectKeys) != len(filePaths) {
		return fmt.Errorf("objectKeys and filePaths lengths mismatch")
	}
	for i, fp := range filePaths {
		if _, err := c.client.PutObjectFromFile(context.Background(), &tos.PutObjectFromFileInput{
			PutObjectBasicInput: tos.PutObjectBasicInput{
				Bucket: c.config.Bucket,
				Key:    objectKeys[i],
				Meta:   metadata,
			},
			FilePath: fp,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) AsyncUploadFiles(filePaths []string, objectKeys []string, metadata map[string]string) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- c.UploadFiles(filePaths, objectKeys, metadata)
		close(ch)
	}()
	return ch
}

func (c *Client) UploadDirectory(directoryPath string, metadata map[string]string) error {
	if err := preCheckBucket(c.config.Bucket); err != nil {
		return err
	}
	if err := filepath.Walk(directoryPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		objectKey, err2 := filepath.Rel(directoryPath, path)
		if err2 != nil {
			return err2
		}
		if _, err = c.client.PutObjectFromFile(context.Background(), &tos.PutObjectFromFileInput{
			PutObjectBasicInput: tos.PutObjectBasicInput{
				Bucket: c.config.Bucket,
				Key:    objectKey,
				Meta:   metadata,
			},
			FilePath: path,
		}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (c *Client) AsyncUploadDirectory(directoryPath string, metadata map[string]string) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- c.UploadDirectory(directoryPath, metadata)
		close(ch)
	}()
	return ch
}

// Download https://www.volcengine.com/docs/6349/93471?lang=zh
func (c *Client) Download(objectKey string, savePath string) error {
	if err := preCheckBucket(c.config.Bucket); err != nil {
		return err
	}
	if objectKey == "" || savePath == "" {
		return fmt.Errorf("objectKey or savePath is empty")

	}
	if err := c.ensureClientAndBucket(); err != nil {
		return err
	}
	rc, err := c.client.GetObjectV2(context.Background(), &tos.GetObjectV2Input{
		Bucket: c.config.Bucket,
		Key:    objectKey,
	})
	if err != nil {
		return err
	}
	defer rc.Content.Close()

	if dir := filepath.Dir(savePath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	f, err := os.Create(savePath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = io.Copy(f, rc.Content); err != nil {
		return err
	}
	return nil
}

func (c *Client) Close() {
	if c.client != nil {
		c.client.Close()
	}
}
