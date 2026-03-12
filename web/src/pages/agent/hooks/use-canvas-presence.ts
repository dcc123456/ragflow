import {
  useFetchCanvasPresence,
  useHeartbeatCanvasPresence,
  useJoinCanvasPresence,
  useLeaveCanvasPresence,
} from '@/hooks/use-agent-request';
import {
  ICanvasPresenceResponse,
  ICanvasPresenceUser,
} from '@/interfaces/database/agent';
import { useInterval, useMemoizedFn, useUnmount } from 'ahooks';
import { useEffect, useMemo, useRef } from 'react';
import { useParams } from 'react-router';

const HEARTBEAT_INTERVAL = 5_000;
const TAB_ID_STORAGE_KEY = 'ragflow_canvas_tab_id';

const emptyPresence: ICanvasPresenceResponse = {
  canvas_id: '',
  online_user_count: 0,
  users: [],
};

function createPresenceId() {
  return (
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now()}-${Math.random().toString(16).slice(2)}`
  );
}

function getOrCreateTabId() {
  if (typeof window === 'undefined') {
    return '';
  }

  const currentTabId = window.sessionStorage.getItem(TAB_ID_STORAGE_KEY);
  if (currentTabId) {
    return currentTabId;
  }

  const nextTabId = createPresenceId();
  window.sessionStorage.setItem(TAB_ID_STORAGE_KEY, nextTabId);
  return nextTabId;
}

function buildPresencePayload(
  canvasId: string,
  tabId: string,
  presenceSessionId: string,
) {
  return {
    canvas_id: canvasId,
    tab_id: tabId,
    presence_session_id: presenceSessionId,
  };
}

export function useCanvasPresence() {
  const leaveSentRef = useRef(false);
  const { id: canvasId } = useParams();

  // Generate stable IDs for this session
  const { tabId, presenceSessionId } = useMemo(() => {
    if (!canvasId || typeof window === 'undefined') {
      return { tabId: '', presenceSessionId: '' };
    }
    return {
      tabId: getOrCreateTabId(),
      presenceSessionId: createPresenceId(),
    };
  }, [canvasId]);

  // Use hooks from use-agent-request
  const { data: presence } = useFetchCanvasPresence();

  const { joinPresence } = useJoinCanvasPresence(canvasId);
  const { heartbeat } = useHeartbeatCanvasPresence();
  const { leavePresence } = useLeaveCanvasPresence();

  // Heartbeat function with auto-rejoin on session not found
  const sendHeartbeat = useMemoizedFn(async () => {
    if (!canvasId || !tabId) return;

    const payload = buildPresencePayload(canvasId, tabId, presenceSessionId);
    const heartbeatData = await heartbeat(payload);

    if (
      heartbeatData?.ok === false &&
      heartbeatData?.code === 'SESSION_NOT_FOUND'
    ) {
      await joinPresence(payload);
    }
  });

  // Leave presence function
  const sendLeave = useMemoizedFn(() => {
    if (leaveSentRef.current || !canvasId || !tabId) {
      return;
    }
    leaveSentRef.current = true;

    // Use keepalive for reliable delivery during page unload
    const payload = buildPresencePayload(canvasId, tabId, presenceSessionId);
    void leavePresence(payload).catch(() => {});
  });

  // Initial join
  useEffect(() => {
    if (canvasId && tabId && typeof window !== 'undefined') {
      leaveSentRef.current = false;
      const payload = buildPresencePayload(canvasId, tabId, presenceSessionId);
      joinPresence(payload);
    }
  }, [canvasId, tabId, presenceSessionId, joinPresence]);

  // Heartbeat interval
  useInterval(
    () => {
      sendHeartbeat();
    },
    canvasId && tabId ? HEARTBEAT_INTERVAL : undefined,
  );

  // Cleanup on unmount
  useUnmount(() => {
    sendLeave();
  });

  // Compute sorted users
  const users = useMemo(() => {
    const presenceData = presence ?? emptyPresence;
    return presenceData.users
      .map((user: ICanvasPresenceUser) => ({
        ...user,
        permission: Number(user.permission ?? 0),
      }))
      .sort((left: ICanvasPresenceUser, right: ICanvasPresenceUser) => {
        const permissionDiff = right.permission - left.permission;
        if (permissionDiff !== 0) {
          return permissionDiff;
        }

        const lastSeenDiff = right.last_seen_at - left.last_seen_at;
        if (lastSeenDiff !== 0) {
          return lastSeenDiff;
        }

        return left.user_id.localeCompare(right.user_id);
      });
  }, [presence]);

  const finalPresence =
    presence ??
    (canvasId ? { ...emptyPresence, canvas_id: canvasId } : emptyPresence);

  return {
    presence: finalPresence,
    users,
    onlineUserCount: finalPresence.online_user_count,
    operatorPermission: Number(presence.operator_permission ?? 0),
  };
}
