import type { RAGFlowNodeType } from '@/interfaces/database/agent';
import { PropsWithChildren } from 'react';
import { useTranslation } from 'react-i18next';

const CadFileFormat = 'cad';

type ParserFormContentProps = PropsWithChildren<{
  node?: RAGFlowNodeType;
}>;

function isCadSetup(setup: unknown) {
  if (typeof setup !== 'object' || setup === null) {
    return false;
  }
  return (setup as { fileFormat?: unknown }).fileFormat === CadFileFormat;
}

export function ParserFormContent({ node, children }: ParserFormContentProps) {
  const { t } = useTranslation();
  const setups: unknown = node?.data?.form?.setups;
  const isCadOnly =
    Array.isArray(setups) && setups.length > 0 && setups.every(isCadSetup);

  if (isCadOnly) {
    return (
      <p className="p-4 text-sm text-text-secondary">
        {t('flow.cadParserNoConfig')}
      </p>
    );
  }
  return children;
}
