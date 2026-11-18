import { pickBy } from 'lodash';
import { toast } from 'sonner';

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { listVariables, setVariable } from '@/ee/services/admin-service';
import { SystemConfigKeys } from '@/hooks/use-system-request';

export default function useAdminVariables() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['admin/listVariables'],
    queryFn: async () => {
      const { data: rawData } = await listVariables();
      return rawData?.data;
    },
    retry: false,
    initialData: {} as AdminServiceEe.SystemVariables,
  });

  const { mutateAsync: setVariables, isPending: isUpdating } = useMutation({
    mutationFn: (variables: AdminServiceEe.SetVariablesInput) => {
      const diff = Object.entries(
        pickBy(
          variables,
          (v, k) => v != data[k as AdminServiceEe.SystemVariables.Name]?.value,
        ),
      );

      return Promise.all(diff.map(([name, value]) => setVariable(name, value)));
    },
    onSuccess: () => {
      toast.success(t('message.updated'), {
        position: 'top-center',
      });
      refetch();
      queryClient.invalidateQueries({ queryKey: SystemConfigKeys.config() });
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
