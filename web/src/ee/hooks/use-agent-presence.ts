import { AgentApiAction } from '@/hooks/use-agent-request';
import {
  ICanvasPresenceHeartbeatData,
  ICanvasPresenceResponse,
  IPresencePayload,
} from '@/interfaces/database/agent';
import canvasPresenceService from '@/ee/services/canvas-presence-service';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useParams } from 'react-router';

const emptyPresence: ICanvasPresenceResponse = {
  canvas_id: '',
  online_user_count: 0,
  users: [],
};

export const useFetchCanvasPresence = () => {
  const { id: canvasId } = useParams();

  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<ICanvasPresenceResponse>({
    queryKey: [AgentApiAction.FetchCanvasPresence, canvasId],
    queryFn: async () => {
      if (!canvasId) return emptyPresence;
      const { data } =
        await canvasPresenceService.fetchCanvasPresence(canvasId);
      return data?.data ?? emptyPresence;
    },
    enabled: !!canvasId,
    refetchInterval: 5000,
    staleTime: 5000,
    placeholderData: canvasId
      ? { ...emptyPresence, canvas_id: canvasId }
      : emptyPresence,
  });

  return { data: data ?? emptyPresence, loading, refetch };
};

export const useJoinCanvasPresence = (canvasId?: string) => {
  const queryClient = useQueryClient();

  const { mutateAsync, isPending: loading } = useMutation({
    mutationKey: [AgentApiAction.JoinCanvasPresence],
    mutationFn: async (payload: IPresencePayload) => {
      if (!canvasId || !payload.tab_id) return;
      return canvasPresenceService.joinCanvasPresence(payload);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: [AgentApiAction.FetchCanvasPresence, canvasId],
      });
    },
  });

  return { joinPresence: mutateAsync, loading };
};

export const useHeartbeatCanvasPresence = () => {
  const {
    mutateAsync,
    isPending: loading,
    data,
  } = useMutation({
    mutationKey: [AgentApiAction.HeartbeatCanvasPresence],
    mutationFn: async (payload: IPresencePayload) => {
      if (!payload.canvas_id || !payload.tab_id) return null;
      const { data } =
        await canvasPresenceService.heartbeatCanvasPresence(payload);
      return data?.data as ICanvasPresenceHeartbeatData | null;
    },
  });

  return { heartbeat: mutateAsync, loading, data };
};

export const useLeaveCanvasPresence = () => {
  const { mutateAsync, isPending: loading } = useMutation({
    mutationKey: [AgentApiAction.LeaveCanvasPresence],
    mutationFn: async (payload: IPresencePayload) => {
      return canvasPresenceService.leaveCanvasPresence(payload);
    },
  });

  return { leavePresence: mutateAsync, loading };
};
