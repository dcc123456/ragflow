import docAnalyzerService from '@/ee/services/doc-analyzer-service';
import { useQuery } from '@tanstack/react-query';
import { useMemo } from 'react';

interface DocAnalyzer {
  id: string;
  kind: 'builtin' | 'ee';
  available: boolean;
  label?: string;
}

const DocAnalyzerKeys = {
  all: () => ['docAnalyzers'] as const,
};

/**
 * Parser options contributed by the EE doc-analyzer backends this deployment
 * can actually serve. The shared parser select appends them to its static
 * list; the open-source stub keeps that list unchanged.
 */
export function useExtraParserOptions(): { value: string; label: string }[] {
  const { data } = useQuery({
    queryKey: DocAnalyzerKeys.all(),
    queryFn: async ({ signal }) => {
      const { data: res } = await docAnalyzerService.getDocAnalyzers(
        { signal, skipGlobalErrorNotification: true },
        true,
      );
      return res?.code === 0 && Array.isArray(res.data)
        ? (res.data as DocAnalyzer[])
        : [];
    },
    staleTime: 60_000,
    retry: false,
  });

  return useMemo(
    () =>
      (data ?? [])
        .filter((analyzer) => analyzer.available && analyzer.kind === 'ee')
        .map((analyzer) => ({
          value: analyzer.id,
          label: analyzer.label ?? analyzer.id,
        })),
    [data],
  );
}
