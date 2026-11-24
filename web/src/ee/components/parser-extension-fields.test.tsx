import { Operator } from '@/constants/agent';
import { TooltipProvider } from '@/components/ui/tooltip';
import {
  transformApiConfigToForm,
  transformFormConfigToApi,
} from '@/utils/pipeline-operator';

import { fireEvent, render, screen } from '@testing-library/react';
import { FormProvider, useForm, useWatch } from 'react-hook-form';
import {
  DatasetParserOptions,
  ParserExtensionFields,
} from './parser-extension-fields';

jest.mock('react-i18next', () => ({
  ...jest.requireActual('react-i18next'),
  useTranslation: () => ({ t: (key: string) => key }),
}));

it('changes a single Parser-level setting in dataset settings', () => {
  function DatasetForm() {
    const form = useForm({
      defaultValues: {
        setups: [{ fileFormat: 'docx' }, { fileFormat: 'pdf' }],
      },
    });
    const values = useWatch({ control: form.control });
    return (
      <FormProvider {...form}>
        <TooltipProvider delayDuration={0}>
          <DatasetParserOptions>
            <ParserExtensionFields />
          </DatasetParserOptions>
        </TooltipProvider>
        <output>{JSON.stringify(values)}</output>
      </FormProvider>
    );
  }

  render(<DatasetForm />);
  const toggle = screen.getByRole('switch', {
    name: 'flow.removeLowContrastWordText',
  });
  expect(
    screen.queryByText('flow.removeLowContrastWordTextDescription'),
  ).not.toBeInTheDocument();
  expect(toggle).not.toBeChecked();
  fireEvent.click(toggle);
  expect(toggle).toBeChecked();
  expect(JSON.parse(screen.getByRole('status').textContent ?? '')).toEqual({
    setups: [{ fileFormat: 'docx' }, { fileFormat: 'pdf' }],
    remove_low_contrast_text: true,
  });
  fireEvent.click(toggle);
  expect(toggle).not.toBeChecked();
});

it('keeps the field out of the standalone parser form', () => {
  render(<ParserExtensionFields />);
  expect(screen.queryByRole('switch')).not.toBeInTheDocument();
});

describe('dataset Word parser configuration', () => {
  it.each([true, false])(
    'round-trips one Parser-level value (%s)',
    (enabled) => {
      const loaded = transformApiConfigToForm(Operator.Parser, {
        remove_low_contrast_text: enabled,
        doc: {},
        docx: {},
        pdf: {},
      });
      expect(loaded.remove_low_contrast_text).toBe(enabled);
      expect(loaded.setups).toHaveLength(3);

      const saved = transformFormConfigToApi(Operator.Parser, loaded);
      expect(saved.remove_low_contrast_text).toBe(enabled);
      expect(saved.doc).not.toHaveProperty('remove_low_contrast_text');
      expect(saved.docx).not.toHaveProperty('remove_low_contrast_text');
    },
  );
});
