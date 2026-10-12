// Copyright (c) 2025 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0

package veauth

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/volcengine/veadk-go/v2/common"
)

func TestRoleCredentialSourceIsLazyAndPrefersExplicitCredential(t *testing.T) {
	source := NewRoleCredentialSource(RoleCredentialSourceConfig{
		AccessKeyID:     " explicit-ak ",
		SecretAccessKey: " explicit-sk ",
		SessionToken:    " explicit-token ",
		CredentialPath:  filepath.Join(t.TempDir(), "missing"),
	})

	credential, err := source.Credential(context.Background())
	if err != nil {
		t.Fatalf("Credential() error = %v", err)
	}
	if credential.AccessKeyID != "explicit-ak" || credential.SecretAccessKey != "explicit-sk" || credential.SessionToken != "explicit-token" {
		t.Fatalf("Credential() = %#v, want trimmed explicit credential", credential)
	}
}

func TestRoleCredentialSourceReadsMountedCredentialLazily(t *testing.T) {
	t.Setenv(common.VOLCENGINE_ACCESS_KEY, "")
	t.Setenv(common.VOLCENGINE_SECRET_KEY, "")
	t.Setenv(common.VOLCENGINE_SESSION_TOKEN, "")
	t.Setenv(common.VOLC_SESSIONTOKEN, "")

	path := filepath.Join(t.TempDir(), "credential")
	source := NewRoleCredentialSource(RoleCredentialSourceConfig{CredentialPath: path})
	if err := os.WriteFile(path, []byte(`{"access_key_id":"file-ak","secret_access_key":"file-sk","session_token":"file-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	credential, err := source.Credential(context.Background())
	if err != nil {
		t.Fatalf("Credential() error = %v", err)
	}
	if credential.AccessKeyID != "file-ak" || credential.SecretAccessKey != "file-sk" || credential.SessionToken != "file-token" {
		t.Fatalf("Credential() = %#v, want mounted credential", credential)
	}
}

func TestRoleCredentialSourceRejectsPartialCredential(t *testing.T) {
	source := NewRoleCredentialSource(RoleCredentialSourceConfig{
		AccessKeyID:    "only-ak",
		CredentialPath: filepath.Join(t.TempDir(), "must-not-be-read"),
	})
	if _, err := source.Credential(context.Background()); err == nil {
		t.Fatal("Credential() error = nil, want partial credential error")
	}
}

func TestRoleCredentialSourceUsesExplicitSessionTokenWithEnvironmentKeys(t *testing.T) {
	t.Setenv(common.VOLCENGINE_ACCESS_KEY, "environment-ak")
	t.Setenv(common.VOLCENGINE_SECRET_KEY, "environment-sk")
	t.Setenv(common.VOLCENGINE_SESSION_TOKEN, "environment-token")

	source := NewRoleCredentialSource(RoleCredentialSourceConfig{
		SessionToken:   "explicit-token",
		CredentialPath: filepath.Join(t.TempDir(), "must-not-be-read"),
	})
	credential, err := source.Credential(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessKeyID != "environment-ak" || credential.SecretAccessKey != "environment-sk" || credential.SessionToken != "explicit-token" {
		t.Fatalf("Credential() = %#v, want environment keys with explicit token", credential)
	}
}

func TestRoleCredentialSourceSupportsBytePlusEnvironment(t *testing.T) {
	t.Setenv(common.BYTEPLUS_ACCESS_KEY, "byteplus-ak")
	t.Setenv(common.BYTEPLUS_SECRET_KEY, "byteplus-sk")
	t.Setenv(common.BYTEPLUS_SESSION_TOKEN, "byteplus-token")

	source := NewRoleCredentialSource(RoleCredentialSourceConfig{
		EnvironmentPrefix: "byteplus",
		CredentialPath:    filepath.Join(t.TempDir(), "must-not-be-read"),
	})
	credential, err := source.Credential(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessKeyID != "byteplus-ak" || credential.SecretAccessKey != "byteplus-sk" || credential.SessionToken != "byteplus-token" {
		t.Fatalf("Credential() = %#v, want BytePlus environment credential", credential)
	}
}

func TestRoleCredentialSourceHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := NewRoleCredentialSource(RoleCredentialSourceConfig{
		CredentialPath: filepath.Join(t.TempDir(), "must-not-be-read"),
	})
	if _, err := source.Credential(ctx); err == nil {
		t.Fatal("Credential() error = nil, want context cancellation")
	}
}
