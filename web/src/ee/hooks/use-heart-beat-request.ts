import heartBeatService from '@/ee/services/heart-beat-service';
import { useQuery } from '@tanstack/react-query';

const HeartBeatKeys = {
  heartbeat: () => ['heartBeat'] as const,
};

export const useHeartBeat = () => {
  useQuery({
    queryKey: HeartBeatKeys.heartbeat(),
    queryFn: async ({ signal }) => {
      const { data } = await heartBeatService.heartBeat({ signal }, true);
      return data;
    },
    refetchInterval: 2 * 60 * 1000,
  });
};
