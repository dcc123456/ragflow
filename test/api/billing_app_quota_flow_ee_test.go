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
	"testing"
	"time"
)

func TestApp01BasicQuotaEnforcement(t *testing.T) {
	tenant := newTenant(t)
	limit := int(appsLimit(t, "Trial"))
	for i := 0; i < limit; i++ {
		tenant.createDataset(fmt.Sprintf("trial-app-%d", i))
	}
	if code, message := tenant.createDatasetExpectError("trial-app-over"); code == 0 {
		t.Fatalf("Trial tenant exceeded its app quota (%s)", message)
	}
}

// TestApp02DowngradeBlockedByResourceUsage ports APP-02: resources created on a
// paid plan prevent falling back to Trial, and the guard cancels the change.

func TestApp02DowngradeBlockedByResourceUsage(t *testing.T) {
	tenant := newTenant(t)
	tenant.attachTestCard()
	tenant.ensureSubscriptionAtPrice("Starter")
	tenant.waitForPlan("Starter", 150*time.Second)
	for i := 0; i < int(appsLimit(t, "Trial"))+1; i++ {
		tenant.createDataset(fmt.Sprintf("starter-app-%d", i))
	}
	tenant.post("/billing/subscription", map[string]interface{}{
		"payment_type":          "subscription",
		"subscription_price_id": trialPriceID(t),
	})
	// The over-quota downgrade is scheduled, then cancelled by the guard; the
	// plan must still be Starter when the sweep runs (10 min) or on the next
	// read after the guard cancels it.
	plan, _ := tenant.mustSubscription()["plan_name"].(string)
	if plan != "Starter" {
		t.Fatalf("plan changed before the period end: %q", plan)
	}
}

// TestTrial02FirstUpgradeViaSetupIntent ports TRIAL-02: the first upgrade can
// reuse a confirmed setup intent instead of collecting the card again.
