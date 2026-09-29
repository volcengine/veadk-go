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

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/volcengine/veadk-go/v2/skills"
)

func main() {
	root := flag.String("root", ".adk/skills", "directory whose immediate children are local skills")
	strict := flag.Bool("strict", false, "fail if any immediate child skill is invalid")
	resource := flag.String("resource", "", "optional skill-name:relative/path resource to verify")
	flag.Parse()

	mode := skills.DiscoverySkipInvalid
	if *strict {
		mode = skills.DiscoveryStrict
	}
	discovered, err := skills.DiscoverSkillsFromDirWithOptions(context.Background(), *root, skills.DiscoveryOptions{
		Mode: mode,
		OnIssue: func(issue skills.DiscoveryIssue) {
			fmt.Fprintf(os.Stderr, "invalid skill %q: %v\n", issue.Entry, issue.Err)
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "discovery failed: %v\n", err)
		os.Exit(1)
	}
	for _, skill := range discovered {
		fmt.Printf("%s\t%s\n", skill.Name(), skill.Description())
	}

	if strings.TrimSpace(*resource) == "" {
		return
	}
	skillName, resourcePath, ok := strings.Cut(*resource, ":")
	if !ok || skillName == "" || resourcePath == "" {
		fmt.Fprintln(os.Stderr, "resource must use skill-name:relative/path format")
		os.Exit(2)
	}
	for _, skill := range discovered {
		if skill.Name() != skillName {
			continue
		}
		data, readErr := skill.ReadResourceContext(context.Background(), resourcePath, skills.DefaultMaxResourceBytes)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "resource verification failed: %v\n", readErr)
			os.Exit(1)
		}
		digest := sha256.Sum256(data)
		fmt.Printf("resource\t%s\t%d bytes\tsha256:%s\n", resourcePath, len(data), hex.EncodeToString(digest[:]))
		return
	}
	fmt.Fprintf(os.Stderr, "skill %q was not discovered\n", skillName)
	os.Exit(1)
}
