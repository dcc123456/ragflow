#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#

import asyncio

from common.asyncio_utils import LoopLocalSemaphore


def test_loop_local_semaphore_does_not_reuse_locked_semaphore_across_event_loops():
    limiter = LoopLocalSemaphore(1)

    async def bind_and_leave_locked():
        await limiter.acquire()
        waiter = asyncio.create_task(limiter.acquire())
        await asyncio.sleep(0)
        waiter.cancel()
        await asyncio.gather(waiter, return_exceptions=True)

    async def acquire_in_next_loop():
        async with limiter:
            return True

    asyncio.run(bind_and_leave_locked())

    assert asyncio.run(acquire_in_next_loop())
    assert not any(loop.is_closed() for loop in limiter._semaphores)
