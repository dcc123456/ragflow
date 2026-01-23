import { zodResolver } from '@hookform/resolvers/zod';
import { isEmpty, pick } from 'lodash';
import z from 'zod';

import { useId, useMemo } from 'react';
import { ResolverResult, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Form } from '@/components/ui/form';
import { ScrollArea } from '@/components/ui/scroll-area';

import Spotlight from '@/components/spotlight';

import SMTPSettingsForm from './form-group/smtp';
import WhitelistSettingsFormGroup from './form-group/whitelist';

import useAdminVariables from '../hooks/useAdminVariables';

const schema = z.object({
  smtp: SMTPSettingsForm.schema,
  whitelist: WhitelistSettingsFormGroup.schema,
});

export type AdminSettingsFormValues = z.infer<typeof schema>;

function AdminSettings() {
  const { t } = useTranslation();

  const { variables, setVariables, isUpdating, isFetching } =
    useAdminVariables();

  const values = useMemo(() => {
    return {
      smtp: SMTPSettingsForm.mapValuesFromData(variables),
      whitelist: WhitelistSettingsFormGroup.mapValuesFromData(variables),
    };
  }, [variables]);

  const formId = useId();
  const form = useForm<z.infer<typeof schema>>({
    resolver: async (values, context, options) => {
      const dirtyGroupNames = Object.keys(form.formState.dirtyFields);
      const { errors } = await zodResolver(schema)(values, context, options);
      const filteredErrors = pick(errors, dirtyGroupNames);

      const result = {
        errors: filteredErrors,
        values: isEmpty(filteredErrors) ? values : {},
      } as ResolverResult<AdminSettingsFormValues>;

      return result;
    },
    values,
  });

  return (
    <>
      <Card className="!shadow-none relative h-full bg-transparent rounded-xl overflow-hidden flex flex-col">
        <Spotlight />

        <CardHeader className="border-b-0.5 border-border-button">
          <CardTitle className="h-10 flex items-center">
            {t('admin.settings')}
          </CardTitle>
          <CardDescription className="text-text-secondary">
            {t('admin.settingsSubtitle')}
          </CardDescription>
        </CardHeader>

        <CardContent className="h-0 flex-1 p-0 flex flex-row">
          <div className="w-1/2 min-w-[40rem] max-w-[56rem] h-full flex flex-col">
            <ScrollArea className="flex-1 h-full">
              <div className="p-6">
                <Form {...form}>
                  <form
                    id={formId}
                    className="space-y-8"
                    onSubmit={form.handleSubmit(async (data) => {
                      await setVariables({
                        ...WhitelistSettingsFormGroup.mapValuesToData(
                          data.whitelist,
                        ),
                        ...SMTPSettingsForm.mapValuesToData(data.smtp),
                      });
                    })}
                  >
                    <SMTPSettingsForm />
                    <hr className="border-border-button" />
                    <WhitelistSettingsFormGroup />
                  </form>
                </Form>
              </div>
            </ScrollArea>

            <div className="p-6 text-right">
              <Button
                type="submit"
                form={formId}
                className="h-10 px-4"
                loading={isUpdating}
                disabled={isFetching || isUpdating || !form.formState.isDirty}
              >
                {t('common.save')}
              </Button>
            </div>
          </div>

          <hr className="mx-0 my-6 h-auto border-l border-border-button" />

          <div className="flex-auto p-6"></div>
        </CardContent>
      </Card>
    </>
  );
}

export default AdminSettings;
