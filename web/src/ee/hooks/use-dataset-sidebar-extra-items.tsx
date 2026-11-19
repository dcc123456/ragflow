import { useMemo } from 'react';
import { LucideHistory } from 'lucide-react';
import { useTranslation } from 'react-i18next';

import { PrivateRoutes } from '@/ee/private-routes';

export type DatasetSidebarExtraItem = {
  icon: React.ReactNode;
  label: string;
  key: string;
};

/**
 * Enterprise seam for the dataset sidebar. Open source keeps no version
 * history, so its stub contributes no item; the enterprise build appends the
 * history entry that opens the version timeline.
 */
export function useDatasetSidebarExtraItems(): DatasetSidebarExtraItem[] {
  const { t } = useTranslation();
  return useMemo(
    () => [
      {
        icon: <LucideHistory className="size-[1em]" />,
        label: t('knowledgeDetails.versions'),
        key: PrivateRoutes.DataSetVersions,
      },
    ],
    [t],
  );
}
