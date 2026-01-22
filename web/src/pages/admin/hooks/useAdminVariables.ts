import { pickBy } from 'lodash';
import { toast } from 'sonner';

import { useMutation, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { listVariables, setVariable } from '@/services/admin-service';

export default function useAdminVariables() {
  const { t } = useTranslation();
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['admin/listVariables'],
    queryFn: async () => {
      const { data: rawData } = await listVariables();
      return rawData?.data;
    },
    retry: false,
    initialData:
      {} as AdminService.SystemVariables.RetypeByTypeAnnotation<AdminService.SystemVariables.All>,
  });

  const { mutateAsync: setVariables, isPending: isUpdating } = useMutation({
    mutationFn: (variables: AdminService.SetVariablesInput) => {
      const diff = Object.entries(
        pickBy(
          variables,
          (v, k) =>
            v != data[k as keyof AdminService.SystemVariables.All]?.value,
        ),
      );

      return Promise.all(diff.map(([name, value]) => setVariable(name, value)));
    },
    onSuccess: () => {
      toast.success(t('message.updated'), {
        position: 'top-center',
      });
      refetch();
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
