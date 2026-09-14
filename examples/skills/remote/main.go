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

// Remote skill discovery and on-demand resource verification. Credentials are
// obtained lazily from the process environment or mounted Role file.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/volcengine/veadk-go/auth/veauth"
	"github.com/volcengine/veadk-go/skills"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "remote skill verification failed:", err)
		os.Exit(1)
	}
}
func run() error {
	credentialFile := flag.String("credential-file", "", "explicit mounted IAM credential file (never printed)")
	space := flag.String("space", "", "AgentKit Skill Space ID")
	endpoint := flag.String("endpoint", "", "explicit control-plane endpoint")
	region := flag.String("region", "", "signing region")
	tosEndpoint := flag.String("tos-endpoint", "", "TOS endpoint override")
	cacheDir := flag.String("cache", ".adk/remote-skills", "private persistent cache directory")
	name := flag.String("skill", "", "skill to load after discovery")
	resource := flag.String("resource", "", "optional relative resource to hash")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var credentials veauth.CredentialSource
	if *credentialFile != "" {
		credentials = veauth.CredentialSourceFunc(func(ctx context.Context) (veauth.VeIAMCredential, error) {
			if err := ctx.Err(); err != nil {
				return veauth.VeIAMCredential{}, err
			}
			return veauth.GetCredentialFromVeFaaSIAM(*credentialFile)
		})
	}
	source, err := skills.NewRemoteSource(skills.RemoteSourceConfig{Credentials: credentials, SpaceID: *space, Endpoint: *endpoint, Region: *region, TOSEndpoint: *tosEndpoint})
	if err != nil {
		return err
	}
	cache, err := skills.NewSkillCache(skills.SkillCacheConfig{Directory: *cacheDir})
	if err != nil {
		return err
	}
	registry, err := skills.NewRegistry(skills.RegistryConfig{Sources: []skills.SourceBinding{{Source: source}}, Cache: cache})
	if err != nil {
		return err
	}
	defer func() { _ = registry.Close() }()
	if _, err = registry.Refresh(ctx); err != nil {
		return err
	}
	snapshot := registry.Snapshot()
	defer func() { _ = snapshot.Close() }()
	output := struct {
		Names         []string `json:"names"`
		Loaded        string   `json:"loaded,omitempty"`
		ResourceBytes int      `json:"resource_bytes,omitempty"`
		SHA256        string   `json:"sha256,omitempty"`
	}{Names: []string{}}
	for _, meta := range snapshot.List() {
		output.Names = append(output.Names, meta.Name)
	}
	if *name != "" {
		sk, err := snapshot.Get(ctx, *name)
		if err != nil {
			return err
		}
		output.Loaded = sk.Name()
		if *resource != "" {
			data, err := sk.ReadResourceContext(ctx, *resource, skills.DefaultMaxResourceBytes)
			if err != nil {
				return err
			}
			h := sha256.Sum256(data)
			output.ResourceBytes = len(data)
			output.SHA256 = hex.EncodeToString(h[:])
		}
	}
	return json.NewEncoder(os.Stdout).Encode(output)
}
