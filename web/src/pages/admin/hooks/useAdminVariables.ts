import { keyBy, pickBy } from 'lodash';
import { toast } from 'sonner';

import { useMutation, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';

import { listVariables, setVariable } from '@/services/admin-service';

function castFromInt(value: string): number | null {
  const parsed = parseInt(value);
  return isNaN(parsed) ? null : parsed;
}

export default function useAdminVariables() {
  const { t } = useTranslation();
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['admin/listVariables'],
    queryFn: async () => {
      const { data: rawData } = await listVariables();
      const data = (rawData?.data ?? []).map((variable) => {
        switch (variable.data_type) {
          case 'bool':
            return {
              ...variable,
              value: variable.value === 'true',
            };
          case 'integer':
            return {
              ...variable,
              value: castFromInt(variable.value),
            };
          default:
            return variable;
        }
      }) satisfies AdminService.Variable[];

      return keyBy(data, 'name') as AdminService.VariableDictionary;
    },
    retry: false,
    initialData: {} as AdminService.VariableDictionary,
  });

  const { mutateAsync: setVariables, isPending: isUpdating } = useMutation({
    mutationKey: ['admin/setVariables'],
    mutationFn: (variables: AdminService.SetVariablesInput) => {
      const diff = Object.entries(
        pickBy(
          variables,
          (v, k) => v != data[k as AdminService.VariableName]?.value,
        ),
      );

      return Promise.all(
        diff.map(([name, value]) => setVariable(name, String(value))),
      );
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
