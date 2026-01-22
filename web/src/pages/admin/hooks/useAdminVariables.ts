import { debounce, DebounceSettings, pickBy } from 'lodash';
import { useMemo } from 'react';
import { toast } from 'sonner';

import { useMutation, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import {
  listVariables,
  refreshVariables,
  setVariable,
} from '@/services/admin-service';

function useRefreshVariables() {
  const { mutateAsync, isPending } = useMutation({
    mutationKey: ['admin/refreshVariables'],
    mutationFn: (input: AdminService.RefreshVariablesInput) =>
      refreshVariables(input),
  });

  return {
    refreshVariables: mutateAsync,
    isPending,
  };
}

function useDebouncedRefreshVariables(wait = 5000, options?: DebounceSettings) {
  const { refreshVariables: mutateAsync, isPending } = useRefreshVariables();
  const debouncedFn = useMemo(
    () => debounce(mutateAsync, wait, options),
    [mutateAsync, wait, options],
  );

  return {
    refreshVariables: debouncedFn,
    isPending,
  };
}

export default function useAdminVariables() {
  const { t } = useTranslation();
  const { refreshVariables } = useDebouncedRefreshVariables();
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['admin/listVariables'],
    queryFn: async () => {
      const { data: rawData } = await listVariables();
      return rawData?.data;
    },
    retry: false,
    initialData: {} as AdminService.SystemVariables,
  });

  const { mutateAsync: setVariables, isPending: isUpdating } = useMutation({
    mutationFn: (variables: AdminService.SetVariablesInput) => {
      const diff = Object.entries(
        pickBy(
          variables,
          (v, k) => v != data[k as AdminService.SystemVariables.Name]?.value,
        ),
      );

      return Promise.all(diff.map(([name, value]) => setVariable(name, value)));
    },
    onSuccess: () => {
      toast.success(t('message.updated'), {
        position: 'top-center',
      });
      refetch();
      refreshVariables({
        oauth: true,
        smtp: true,
      });
    },
  });

  return {
    variables: data,
    isFetching,
    refetch,
    setVariables,
    isUpdating,
  };
}
