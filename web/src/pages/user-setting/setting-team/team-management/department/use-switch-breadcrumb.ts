import { useCallback, useState } from 'react';

export type BreadcrumbType = { label: string; value: string };

export function useSwitchBreadcrumb() {
  const [breadcrumbs, setBreadcrumbs] = useState<BreadcrumbType[]>([
    { value: '', label: 'Root' },
  ]);

  const switchToHomeBreadcrumb = useCallback(() => {
    setBreadcrumbs((pre) => pre.slice(0, 1));
  }, []);

  return { breadcrumbs, setBreadcrumbs, switchToHomeBreadcrumb };
}
