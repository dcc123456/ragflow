import { RAGFlowFormItem } from '@/components/ragflow-form';
import { Switch } from '@/components/ui/switch';
import { createContext, PropsWithChildren, useContext } from 'react';
import { useTranslation } from 'react-i18next';

const DatasetParserContext = createContext(false);

export function DatasetParserOptions({ children }: PropsWithChildren) {
  return (
    <DatasetParserContext.Provider value>
      {children}
    </DatasetParserContext.Provider>
  );
}

export function ParserExtensionFields() {
  return useContext(DatasetParserContext) ? <WordTextFilterField /> : null;
}

function WordTextFilterField() {
  const { t } = useTranslation();
  const label = t('flow.removeLowContrastWordText');
  return (
    <RAGFlowFormItem
      name="remove_low_contrast_text"
      label={label}
      tooltip={t('flow.removeLowContrastWordTextDescription')}
      horizontal={true}
      labelClassName="w-full"
      valueClassName="w-8"
    >
      {(field) => (
        <Switch
          aria-label={label}
          checked={field.value === true}
          onCheckedChange={field.onChange}
        />
      )}
    </RAGFlowFormItem>
  );
}
