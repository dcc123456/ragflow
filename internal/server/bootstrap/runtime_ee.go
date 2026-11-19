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

//go:build ee

package bootstrap

import (
	"time"

	"go.uber.org/zap"

	"ragflow/internal/common"
	"ragflow/internal/server/local"
	"ragflow/internal/service"
	"ragflow/internal/utility"
)

// StartHeartbeat initializes and starts the heartbeat reporter to the admin
// server. The caller must stop and wait for the returned task before closing
// shared resources.
func StartHeartbeat(serverType common.ServerType, serverID string, port int, heartBeatInterval time.Duration) *utility.ScheduledTask {
	localIP, err := utility.GetLocalIP()
	if err != nil {
		common.Fatal("fail to get local ip address")
	}

	client := service.NewAdminClient(
		serverType,
		serverID,
		localIP,
		port,
	)
	if err = client.InitHTTPClient(); err != nil {
		common.Warn("Failed to initialize heartbeat service", zap.Error(err))
		return nil
	}

	heartbeatReporter := utility.NewScheduledTask("Heartbeat reporter", heartBeatInterval, func() {
		if err := client.SendHeartbeat(); err == nil {
			local.SetAdminStatus(local.Available, "")
		} else {
			local.SetAdminStatus(local.Unavailable, err.Error())
		}
	})
	heartbeatReporter.Start()
	return heartbeatReporter
}
