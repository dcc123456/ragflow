import heartBeatService from '@/services/heart-beat-service';
import { useQuery } from '@tanstack/react-query';

export const enum HeartBeatApiAction {
  HeartBeat = 'heartBeat',
}

export const useHeartBeat = () => {
  useQuery({
    queryKey: [HeartBeatApiAction.HeartBeat],
    queryFn: async () => {
      const { data } = await heartBeatService.heartBeat();
      return data;
    },
    refetchInterval: 60 * 1000 * 2,
  });
};
