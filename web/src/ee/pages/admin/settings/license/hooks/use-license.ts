import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';

import { getLicense, setLicense } from '@/services/admin-service';

export function useLicense() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  // Fetch license information
  const { data, isFetching, refetch } = useQuery({
    queryKey: ['admin/license'],
    queryFn: async () => {
      const { data: rawData } = await getLicense();
      if (rawData?.code !== 0) {
        throw new Error(rawData?.message || 'Failed to fetch license');
      }
      return rawData?.data;
    },
    retry: false,
  });

  // Set license
  const { mutateAsync: submitLicense, isPending: isSubmitting } = useMutation({
    mutationFn: async (licenseKey: string) => {
      const { data: rawData } = await setLicense(licenseKey);
      if (rawData?.code !== 0) {
        throw new Error(rawData?.message || 'Failed to set license');
      }
      return rawData?.data;
    },
    onSuccess: () => {
      toast.success(t('license.addSuccess', 'License added successfully'), {
        position: 'top-center',
      });
      queryClient.invalidateQueries({ queryKey: ['admin/license'] });
    },
  });

  // Calculate license status based on dates
  const getLicenseStatus = (
    license?: AdminService.LicenseInfo,
  ): AdminService.LicenseStatus => {
    if (!license) return 'pending';
    const now = new Date();
    const validFrom = new Date(license.ValidFrom);
    const validUntil = new Date(license.ValidUntil);

    if (now < validFrom) return 'inactive';
    if (now > validUntil) return 'expired';
    return 'valid';
  };

  return {
    license: data,
    licenseStatus: getLicenseStatus(data),
    isFetching,
    refetch,
    submitLicense,
    isSubmitting,
  };
}
