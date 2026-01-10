import { zodResolver } from '@hookform/resolvers/zod';
import z from 'zod';

import { useEffect, useId } from 'react';
import { useForm } from 'react-hook-form';
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

import SMTPSettingsFormGroup from './form-group/smtp';
import WhitelistSettingsFormGroup from './form-group/whitelist';

import { Spin } from '@/components/ui/spin';
import useAdminVariables from '../hooks/useAdminVariables';

const schema = z.object({
  smtp: SMTPSettingsFormGroup.schema,
  whitelist: WhitelistSettingsFormGroup.schema,
});

export type AdminSettingsFormValues = z.infer<typeof schema>;

function AdminSettings() {
  const { t } = useTranslation();

  const { variables, setVariables, isFetching, isUpdating } =
    useAdminVariables();

  const formId = useId();
  const form = useForm<z.infer<typeof schema>>({
    resolver: zodResolver(schema),
    disabled: isFetching || isUpdating,
  });

  useEffect(() => {
    const formData = {
      smtp: SMTPSettingsFormGroup.mapValuesFromData(variables),
      whitelist: WhitelistSettingsFormGroup.mapValuesFromData(variables),
    };

    form.reset(formData);
  }, [variables, form]);

  return (
    <>
      <Card className="!shadow-none relative h-full bg-transparent rounded-xl overflow-hidden flex flex-col">
        <Spotlight />

        <CardHeader className="border-b border-border-button">
          <CardTitle className="h-10 flex items-center">
            {t('admin.settings')}
          </CardTitle>
          <CardDescription className="text-text-secondary">
            {t('admin.settingsSubtitle')}
          </CardDescription>
        </CardHeader>

        <Spin spinning={isFetching} className="flex-1 h-0">
          <CardContent className="p-0 flex flex-row h-full">
            <div className="w-1/2 min-w-[30rem] max-w-[50rem] h-full flex flex-col">
              <ScrollArea className="flex-1 h-full">
                <div className="p-6">
                  <Form {...form}>
                    <form
                      id={formId}
                      className="space-y-8"
                      onSubmit={form.handleSubmit((data) => {
                        setVariables({
                          ...WhitelistSettingsFormGroup.mapValuesToData(
                            data.whitelist,
                          ),
                          ...SMTPSettingsFormGroup.mapValuesToData(data.smtp),
                        });
                      })}
                    >
                      <SMTPSettingsFormGroup />
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
        </Spin>
      </Card>
    </>
  );
}

export default AdminSettings;
