import message from '@/components/ui/message';
import { isBillingEnabled } from '@/ee/services/billingStatus';
import exposeApiService from '@/ee/services/expose-api-service';
import exposeModelProviderService from '@/ee/services/expose-model-provider-service';
import privateLLMService from '@/ee/services/private-llm-service';
import { useMutation, useQuery } from '@tanstack/react-query';

export const enum PrivateLLMApiAction {
  SetDefaultLlm = 'setDefaultLlm',
  FetchExposeModelProvider = 'fetchExposeModelProvider',
  FetchExposeApi = 'fetchExposeApi',
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

export async function fetchExposeModelProviderQueryFn() {
  const { data } = await exposeModelProviderService.getExposeModelProvider();
  return data?.data?.enabled === true;
}

export function useFetchExposeModelProvider() {
  const { data, isFetching: loading } = useQuery<boolean>({
    queryKey: [PrivateLLMApiAction.FetchExposeModelProvider],
    initialData: false,
    queryFn: fetchExposeModelProviderQueryFn,
  });

  return { data, loading };
}

export async function fetchExposeApiQueryFn() {
  const { data } = await exposeApiService.getExposeApi();
  const enabled = data?.data?.enabled;
  if (enabled === undefined || enabled === null) {
    return isBillingEnabled();
  }
  return enabled === true;
}

export function useFetchExposeApi() {
  const { data, isFetching: loading } = useQuery<boolean>({
    queryKey: [PrivateLLMApiAction.FetchExposeApi],
    initialData: isBillingEnabled(),
    queryFn: fetchExposeApiQueryFn,
  });

  return { data, loading };
}
