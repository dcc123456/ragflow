//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ragflow/internal/common"
	"ragflow/internal/entity"
	"ragflow/internal/ingestion/testutil"
	"ragflow/internal/ldap"
	"ragflow/internal/server"

	"gorm.io/gorm"
)

// ldapTestPassword is not valid base64, so the RSA helper hands it back
// unchanged instead of looking for conf/private.pem. That keeps these tests on
// the account handling they are about.
const ldapTestPassword = "plain-text"

func setupLDAPLogin(t *testing.T, settings ...entity.SystemSettings) *gorm.DB {
	t.Helper()

	db := testutil.SetupTestDB(t,
		&entity.SystemSettings{},
		&entity.User{},
		&entity.Tenant{},
		&entity.UserTenant{},
		&entity.File{},
		// A first-time login initializes the tenant's default member role.
		&entity.Role{},
		&entity.RoleResource{},
	)
	t.Cleanup(testutil.ReplaceDBForTest(t, db))

	for i := range settings {
		if err := db.Create(&settings[i]).Error; err != nil {
			t.Fatalf("seed %s: %v", settings[i].Name, err)
		}
	}
	// Provisioning a first-time user reads the default model names from the
	// global config.
	if err := server.Init(""); err != nil {
		t.Fatalf("init config: %v", err)
	}
	return db
}

// stubLDAPAuthenticate replaces the directory call and records what the login
// flow passed to it.
func stubLDAPAuthenticate(t *testing.T, identity *ldap.Identity, authenticateErr error) *[]string {
	t.Helper()

	original := ldapAuthenticate
	t.Cleanup(func() { ldapAuthenticate = original })

	calls := make([]string, 0, 1)
	ldapAuthenticate = func(_ context.Context, cfg ldap.Config, username, password string) (*ldap.Identity, error) {
		calls = append(calls, cfg.URL+"|"+username+"|"+password)
		return identity, authenticateErr
	}
	return &calls
}

func TestLDAPLoginRejectsUnknownChannel(t *testing.T) {
	setupLDAPLogin(t, ldapDefaultServer()...)
	calls := stubLDAPAuthenticate(t, &ldap.Identity{Email: "john@corp.example"}, nil)

	_, code, err := NewUserService().LDAPLogin(context.Background(), "ldap|missing", "jdoe", ldapTestPassword)
	if !errors.Is(err, ErrLDAPInvalidChannel) {
		t.Fatalf("expected an invalid-channel error, got %v", err)
	}
	if code != common.CodeDataError {
		t.Errorf("expected a data error code, got %v", code)
	}
	if len(*calls) != 0 {
		t.Errorf("expected the directory to be left alone, got %v", *calls)
	}
}

func TestLDAPLoginRequiresCredentials(t *testing.T) {
	setupLDAPLogin(t, ldapDefaultServer()...)
	calls := stubLDAPAuthenticate(t, &ldap.Identity{Email: "john@corp.example"}, nil)

	for name, credentials := range map[string][2]string{
		"empty username": {"   ", ldapTestPassword},
		"empty password": {"jdoe", ""},
	} {
		_, code, err := NewUserService().LDAPLogin(context.Background(), "ldap|default", credentials[0], credentials[1])
		if !errors.Is(err, ErrLDAPMissingCredentials) {
			t.Errorf("%s: expected a missing-credentials error, got %v", name, err)
		}
		if code != common.CodeAuthenticationError {
			t.Errorf("%s: expected an authentication error code, got %v", name, code)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("expected the directory to be left alone, got %v", *calls)
	}
}

// TestLDAPLoginSignsInExistingUser covers the account the directory already
// owns: the match is by email, the local password is never consulted, and the
// login rotates the access token like every other sign-in.
func TestLDAPLoginSignsInExistingUser(t *testing.T) {
	db := setupLDAPLogin(t, ldapDefaultServer()...)

	oldToken := "old-token"
	status := "1"
	if err := db.Create(&entity.User{
		ID:              "user-1",
		Email:           "john.doe@corp.example",
		Nickname:        "John",
		AccessToken:     &oldToken,
		IsActive:        "1",
		IsAuthenticated: "1",
		Status:          &status,
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Create(&entity.Tenant{ID: "user-1", Status: &status}).Error; err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := db.Create(&entity.UserTenant{
		ID:       "membership-1",
		UserID:   "user-1",
		TenantID: "user-1",
		Role:     "owner",
		Status:   &status,
	}).Error; err != nil {
		t.Fatalf("seed tenant membership: %v", err)
	}
	calls := stubLDAPAuthenticate(t, &ldap.Identity{
		DN:       "cn=John Doe,ou=people,dc=corp,dc=example",
		Email:    "john.doe@corp.example",
		Nickname: "John Doe",
	}, nil)

	user, code, err := NewUserService().LDAPLogin(context.Background(), "ldap|default", "jdoe", ldapTestPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != common.CodeSuccess {
		t.Fatalf("expected success, got %v", code)
	}
	if user.ID != "user-1" {
		t.Fatalf("expected the directory address to match the existing account, got %q", user.ID)
	}
	if user.AccessToken == nil || *user.AccessToken == oldToken {
		t.Errorf("expected a rotated access token, got %v", user.AccessToken)
	}
	if user.LastLoginTime == nil {
		t.Error("expected the login time to be recorded")
	}
	if len(*calls) != 1 || (*calls)[0] != "ldap.example.com|jdoe|"+ldapTestPassword {
		t.Errorf("expected the configured server and the decrypted password, got %v", *calls)
	}
}

// TestLDAPLoginProvisionsNewUser mirrors the OAuth first-login path: the
// directory address creates a local account bound to the channel that resolved
// it, with the tenant and root folder the rest of the app expects.
func TestLDAPLoginProvisionsNewUser(t *testing.T) {
	db := setupLDAPLogin(t, append(ldapDefaultServer(),
		ldapSetting("ldap|server-corp.enabled", "true", "admin"),
		ldapSetting("ldap|server-corp.url", "ldaps://dir.corp.example", "admin"),
		ldapSetting("ldap|server-corp.dn", "cn=reader,dc=corp,dc=example", "admin"),
		ldapSetting("ldap|server-corp.search_filter", "(uid={username})", "admin"),
	)...)
	stubLDAPAuthenticate(t, &ldap.Identity{
		DN:        "cn=Jane Roe,ou=people,dc=corp,dc=example",
		Email:     "jane.roe@corp.example",
		Nickname:  "Jane Roe",
		AvatarURL: "data:image/png;base64,AAAA",
	}, nil)

	user, code, err := NewUserService().LDAPLogin(context.Background(), "ldap|server-corp", "jroe", ldapTestPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != common.CodeSuccess {
		t.Fatalf("expected success, got %v", code)
	}
	if user.Email != "jane.roe@corp.example" || user.Nickname != "Jane Roe" {
		t.Errorf("expected the directory profile, got %q / %q", user.Email, user.Nickname)
	}
	if user.LoginChannel == nil || *user.LoginChannel != "ldap|server-corp" {
		t.Errorf("expected the account bound to the channel that resolved it, got %v", user.LoginChannel)
	}

	var tenant entity.Tenant
	if err := db.Where("id = ?", user.ID).Take(&tenant).Error; err != nil {
		t.Fatalf("expected a tenant for the new account: %v", err)
	}
	var files int64
	if err := db.Model(&entity.File{}).Where("tenant_id = ?", user.ID).Count(&files).Error; err != nil {
		t.Fatalf("count files: %v", err)
	}
	if files != 1 {
		t.Errorf("expected a root folder, got %d files", files)
	}
}

// TestLDAPLoginDecodesBase64Password covers the double wrap the login form
// applies: the password is base64-encoded before it is RSA-encrypted, so the
// directory must receive the decoded plaintext, not the encoded form.
func TestLDAPLoginDecodesBase64Password(t *testing.T) {
	setupLDAPLogin(t, ldapDefaultServer()...)

	// DecryptPassword reads conf/private.pem relative to the working
	// directory; give the test a throwaway key so the RSA path runs without
	// touching the repository's real one.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "conf"), 0o755); err != nil {
		t.Fatalf("create conf dir: %v", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(filepath.Join(root, "conf", "private.pem"), pemBytes, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	t.Chdir(root)

	// The form sends base64(RSA(base64(password))): DecryptPassword peels the
	// RSA layer, decodeLDAPSecret peels the remaining base64 layer.
	encoded := base64.StdEncoding.EncodeToString([]byte("s3cret!"))
	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, &key.PublicKey, []byte(encoded))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	payload := base64.StdEncoding.EncodeToString(ciphertext)

	calls := stubLDAPAuthenticate(t, &ldap.Identity{Email: "john@corp.example"}, nil)

	_, code, err := NewUserService().LDAPLogin(context.Background(), "ldap|default", "jdoe", payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != common.CodeSuccess {
		t.Fatalf("expected success, got %v", code)
	}
	if len(*calls) != 1 || (*calls)[0] != "ldap.example.com|jdoe|s3cret!" {
		t.Errorf("expected the directory to receive the decoded password, got %v", *calls)
	}
}

func TestLDAPLoginRejectsDisabledAccount(t *testing.T) {
	db := setupLDAPLogin(t, ldapDefaultServer()...)
	if err := db.Create(&entity.User{
		ID:       "user-1",
		Email:    "john.doe@corp.example",
		Nickname: "John",
		IsActive: "0",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	stubLDAPAuthenticate(t, &ldap.Identity{Email: "john.doe@corp.example"}, nil)

	_, code, err := NewUserService().LDAPLogin(context.Background(), "ldap|default", "jdoe", ldapTestPassword)
	if !errors.Is(err, ErrOAuthUserInactive) {
		t.Fatalf("expected an inactive-account error, got %v", err)
	}
	if code != common.CodeForbidden {
		t.Errorf("expected a forbidden code, got %v", code)
	}
}

func TestLDAPLoginMapsDirectoryFailures(t *testing.T) {
	tests := []struct {
		name            string
		identity        *ldap.Identity
		authenticateErr error
		wantErr         error
		wantCode        common.ErrorCode
	}{
		{
			name:            "wrong password",
			authenticateErr: ldap.ErrInvalidCredentials,
			wantErr:         ErrLDAPAuthFailed,
			wantCode:        common.CodeAuthenticationError,
		},
		{
			name:            "no matching entry",
			authenticateErr: ldap.ErrUserNotFound,
			wantErr:         ErrLDAPAuthFailed,
			wantCode:        common.CodeAuthenticationError,
		},
		{
			name:            "directory unreachable",
			authenticateErr: errors.New("connection refused"),
			wantErr:         nil,
			wantCode:        common.CodeServerError,
		},
		{
			name:     "entry without an address",
			identity: &ldap.Identity{DN: "cn=Nobody", Nickname: "Nobody"},
			wantErr:  ErrLDAPEmailMissing,
			wantCode: common.CodeDataError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupLDAPLogin(t, ldapDefaultServer()...)
			stubLDAPAuthenticate(t, test.identity, test.authenticateErr)

			_, code, err := NewUserService().LDAPLogin(context.Background(), "ldap|default", "jdoe", ldapTestPassword)
			if err == nil {
				t.Fatal("expected an error")
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("expected %v, got %v", test.wantErr, err)
			}
			if code != test.wantCode {
				t.Fatalf("expected code %v, got %v", test.wantCode, code)
			}
		})
	}
}
