import message from '@/components/ui/message';
import privateLLMService from '@/services/private-llm-service';
import { useMutation, useQuery } from '@tanstack/react-query';

export const enum PrivateLLMApiAction {
  FetchEnableAdmin = 'fetchEnableAdmin',
  FetchIsAdmin = 'fetchIsAdmin',
  SetDefaultLlm = 'setDefaultLlm',
}

export async function fetchEnableAdminQueryFn() {
  const { data } = await privateLLMService.enableAdmin();
  return data?.data?.enable !== 0;
}
export function useFetchEnableAdmin() {
  const { data, isFetching: loading } = useQuery<boolean>({
    queryKey: [PrivateLLMApiAction.FetchEnableAdmin],
    initialData: false,
    queryFn: fetchEnableAdminQueryFn,
  });

  return { data, loading };
}

export async function fetchIsAdminQueryFn() {
  const { data } = await privateLLMService.isAdmin();
  return data?.data?.admin === true;
}

export function useFetchIsAdmin() {
  const { data, isFetching: loading } = useQuery<boolean>({
    queryKey: [PrivateLLMApiAction.FetchIsAdmin],
    initialData: false,
    queryFn: fetchIsAdminQueryFn,
  });

  return { data, loading };
}

export const useSetDefaultLlm = () => {
  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation<number, Error, { llm_factory: string; llm_name: string }>({
    mutationKey: [PrivateLLMApiAction.SetDefaultLlm],
    mutationFn: async (params) => {
      const ret = await privateLLMService.setDefaultLlm(params);
      if (ret?.data?.code === 0) {
        message.success('success');
      } else {
        message.error(ret?.data?.data);
      }
      return ret?.data?.code;
    },
  });

  return { data, loading, setDefaultLlm: mutateAsync };
};
