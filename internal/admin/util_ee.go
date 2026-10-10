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

package admin

import "time"

func formatLicenseTime(t time.Time) string {
	if t.IsZero() {
		return "N/A (Perpetual)"
	}
	return t.Format("2006-01-02 15:04:05")
}

func formatTime(t *int64) string {
	if t == nil {
		return "N/A"
	}
	return time.UnixMilli(*t).Format("2006-01-02 15:04:05")
}
