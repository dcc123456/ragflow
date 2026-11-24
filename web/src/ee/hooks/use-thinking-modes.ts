/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

/**
 * Enterprise overlay for the reasoning-engine options (modes 6-8).
 *
 * The enterprise build offers three engine levels on top of the shared naive
 * ('0') and react ('5') choices. Order matters: they render first, highest
 * effort on the left.
 */
import type { ThinkingOption } from '@/hooks/use-thinking-level';

export const extraThinkingOptions: ThinkingOption[] = [
  {
    value: '8',
    labelKey: 'chat.thinkingLevelCodex',
    descriptionKey: 'chat.thinkingLevelCodexDescription',
  },
  {
    value: '7',
    labelKey: 'chat.thinkingLevelIterative',
    descriptionKey: 'chat.thinkingLevelIterativeDescription',
  },
  {
    value: '6',
    labelKey: 'chat.thinkingLevelPlanExplorerAuditor',
    descriptionKey: 'chat.thinkingLevelPlanExplorerAuditorDescription',
  },
];
