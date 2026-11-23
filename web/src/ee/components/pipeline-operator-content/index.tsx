import type { RAGFlowNodeType } from '@/interfaces/database/agent';
import { PropsWithChildren } from 'react';
import { useTranslation } from 'react-i18next';

const NoConfigParserMessages: Record<string, string> = {
  cad: 'flow.cadParserNoConfig',
  visio: 'flow.visioParserNoConfig',
};

type ParserFormContentProps = PropsWithChildren<{
  node?: RAGFlowNodeType;
}>;

function getSetupFileFormat(setup: unknown) {
  if (typeof setup !== 'object' || setup === null) {
    return undefined;
  }
  const value = (setup as { fileFormat?: unknown }).fileFormat;
  return typeof value === 'string' ? value : undefined;
}

export function ParserFormContent({ node, children }: ParserFormContentProps) {
  const { t } = useTranslation();
  const setups: unknown = node?.data?.form?.setups;
  if (Array.isArray(setups) && setups.length > 0) {
    const firstFormat = getSetupFileFormat(setups[0]);
    const messageKey = firstFormat
      ? NoConfigParserMessages[firstFormat]
      : undefined;
    if (
      messageKey &&
      setups.every((setup) => getSetupFileFormat(setup) === firstFormat)
    ) {
      return <p className="p-4 text-sm text-text-secondary">{t(messageKey)}</p>;
    }
  }
  return children;
}
