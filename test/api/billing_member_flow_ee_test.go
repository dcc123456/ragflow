//go:build integration && ee

//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestMember01TrialQuota(t *testing.T) {
	tenant := newTenant(t)
	code, message := tenant.postExpectError(fmt.Sprintf("/tenants/%s/users", tenant.tenantID), map[string]interface{}{
		"email": registerMember(t, "trial"),
	})
	if code == 0 {
		t.Fatalf("Trial tenant accepted an extra member (%s)", message)
	}
}

// TestMember01StarterQuota ports MEMBER-01 (Starter): after upgrading, the same
// invitation is accepted.

func TestMember01StarterQuota(t *testing.T) {
	tenant := newTenant(t)
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Starter")
	tenant.waitForPlan("Starter", 150*time.Second)

	tenant.post(fmt.Sprintf("/tenants/%s/users", tenant.tenantID), map[string]interface{}{
		"email": registerMember(t, "starter"),
	})
	resp := rawJSON(t, http.MethodGet, TestConfig.HostAddress+"/api/v1"+fmt.Sprintf("/tenants/%s/users", tenant.tenantID), nil, tenant.cfg.AuthToken)
	if resp.status != http.StatusOK {
		t.Fatalf("member list failed: %d %s", resp.status, resp.body)
	}
}

// TestMember02TrialRejectedStarterAccepted ports MEMBER-02: rejection on Trial
// then acceptance after the upgrade.

func TestMember02TrialRejectedStarterAccepted(t *testing.T) {
	tenant := newTenant(t)
	email := registerMember(t, "upgrade")
	if code, _ := tenant.postExpectError(fmt.Sprintf("/tenants/%s/users", tenant.tenantID), map[string]interface{}{"email": email}); code == 0 {
		t.Fatal("Trial tenant should reject the invitation")
	}
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Starter")
	tenant.waitForPlan("Starter", 150*time.Second)
	tenant.post(fmt.Sprintf("/tenants/%s/users", tenant.tenantID), map[string]interface{}{"email": email})
}

// TestApp01BasicQuotaEnforcement ports APP-01: a Trial tenant is limited to its
// app quota and gets more room after upgrading.

func TestMember03RemoveToEnableAcceptance(t *testing.T) {
	tenant := newTenant(t)
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Starter")
	tenant.waitForPlan("Starter", 150*time.Second)

	// Fill every seat with pending invitations, then free one seat and confirm a
	// previously rejected invitation is accepted.
	limit := int(membersLimit(t, "Starter"))
	var invited []string
	for i := 0; i < limit-1; i++ {
		email := registerMember(t, fmt.Sprintf("seat%d", i))
		tenant.post(fmt.Sprintf("/tenants/%s/users", tenant.tenantID), map[string]interface{}{"email": email})
		invited = append(invited, email)
	}
	overflow := registerMember(t, "overflow")
	if code, _ := tenant.postExpectError(fmt.Sprintf("/tenants/%s/users", tenant.tenantID), map[string]interface{}{"email": overflow}); code == 0 {
		t.Fatal("tenant accepted a member beyond its seat quota")
	}
	tenant.removeMember(invited[len(invited)-1])
	tenant.post(fmt.Sprintf("/tenants/%s/users", tenant.tenantID), map[string]interface{}{"email": overflow})
}

// TestFileUpload02StorageQuotaExceeded ports FILE-UPLOAD-BILLING-02.
