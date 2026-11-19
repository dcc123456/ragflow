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

import { parseUnifiedDiff } from '../unified-diff';

describe('parseUnifiedDiff', () => {
  it('does not turn the trailing newline into an empty row', () => {
    const hunks = parseUnifiedDiff(
      '--- a\n+++ b\n@@ -1,1 +1,1 @@\n-old body\n+new body\n',
    );

    expect(hunks).toHaveLength(1);
    expect(hunks[0].lines).toEqual([
      { type: 'removed', content: 'old body' },
      { type: 'added', content: 'new body' },
    ]);
  });

  it('keeps a context line that carries nothing but its prefix', () => {
    const hunks = parseUnifiedDiff('@@ -1,2 +1,2 @@\n first\n \n');

    expect(hunks[0].lines).toEqual([
      { type: 'context', content: ' first' },
      { type: 'context', content: ' ' },
    ]);
  });
});
