import { Collapse } from '@/components/collapse';
import { Card } from '@/components/ui/card';
import { Switch } from '@/components/ui/switch';
import { useCallback } from 'react';
import { useFormContext, useWatch } from 'react-hook-form';
import type { Control } from 'react-hook-form';
import { useTranslation } from 'react-i18next';

/**
 * Mounted by the shared extractor form: the switch for the resume extraction
 * mode. The value rides the form's `resume.enabled` key through the shared
 * save chain (extraExtractorParams keeps the sub-object); nothing else in
 * the shared form knows the key exists.
 */
export const ExtractorResumeField = () => {
  const { t } = useTranslation();
  const form = useFormContext<Record<string, any>>();
  const enabledRaw = useWatch({
    control: form.control,
    name: 'resume.enabled',
  });
  const enabled = enabledRaw === true || enabledRaw === 1;

  const handleSwitch = useCallback(
    (checked: boolean) => {
      form.setValue('resume.enabled', checked, { shouldDirty: true });
    },
    [form],
  );

  return (
    <Card as="section" className="bg-bg-card px-5 py-2.5 border-none">
      <Collapse
        title={t('flow.extractorResumeTitle')}
        defaultOpen
        rightContent={
          <Switch
            checked={enabled}
            onCheckedChange={handleSwitch}
            data-testid="extractor-resume-switch"
          />
        }
      >
        <p className="text-xs text-text-secondary whitespace-pre-line">
          {t('flow.extractorResumeDescription')}
        </p>
      </Collapse>
    </Card>
  );
};

/**
 * Reports whether the resume extraction mode claims the extractor. While it
 * is on, the shared form hides the standard extraction groups — the mode
 * runs alone, and the backend gate (extractor_resume_ee.go) enforces the
 * same rule for any hand-written config.
 */
export const useExtractorResumeExclusive = (control: Control<any>): boolean => {
  const enabledRaw = useWatch({ control, name: 'resume.enabled' });
  return enabledRaw === true || enabledRaw === 1;
};
