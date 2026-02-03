import { FormContainer } from '@/components/form-container';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormMessage,
} from '@/components/ui/form';
import { useGetMcpServer } from '@/hooks/use-mcp-request';
import useGraphStore from '@/pages/agent/store';
import { zodResolver } from '@hookform/resolvers/zod';
import { isEmpty } from 'lodash';
import { Plus } from 'lucide-react';
import { memo } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';
import { HeaderList } from './header-list';
import { MCPCard } from './mcp-card';
import { useValues } from './use-values';
import { useWatchFormChange } from './use-watch-change';

const FormSchema = z.object({
  items: z.array(z.string()),
  headers: z.record(z.string(), z.string()).optional(),
});

function MCPForm() {
  const clickedToolId = useGraphStore((state) => state.clickedToolId);
  const values = useValues();
  const form = useForm({
    defaultValues: values,
    resolver: zodResolver(FormSchema),
  });
  const { data } = useGetMcpServer(clickedToolId);
  const { t } = useTranslation();

  useWatchFormChange(form);

  const HeaderAdder = ({ field }: any) => {
    const handleAdd = () => {
      field.onChange({
        ...(field.value || {}),
        ['']: '',
      });
    };

    return (
      <div className="flex flex-row items-center gap-2 space-x-2 pt-2">
        <Button type="button" variant={'ghost'} onClick={handleAdd}>
          <Plus />
          {t('common.add')}
        </Button>
      </div>
    );
  };

  return (
    <Form {...form}>
      <form
        className="space-y-6 p-4"
        onSubmit={(e) => {
          e.preventDefault();
        }}
      >
        <Card className="bg-background-highlight p-5">
          <CardHeader className="p-0 pb-3">
            <div>{data.name}</div>
          </CardHeader>
          <CardContent className="p-0 text-sm">
            <span className="pr-2"> URL:</span>
            <a href={data.url} className="text-accent-primary">
              {data.url}
            </a>
          </CardContent>
        </Card>
        <div className="flex flex-col space-y-2">
          <FormField
            control={form.control}
            name="headers"
            render={({ field }) => (
              <>
                <div className="w-full flex flex-row items-center justify-between">
                  <div>{t('flow.header')}</div>
                  <HeaderAdder field={field} />
                </div>
                {!isEmpty(field.value) && (
                  <FormContainer>
                    <FormItem className="space-y-4">
                      <HeaderList
                        headers={field.value || {}}
                        onChange={field.onChange}
                      />
                      <FormMessage />
                    </FormItem>
                  </FormContainer>
                )}
              </>
            )}
          />
        </div>
        <div className="flex flex-col space-y-2">
          <div>{t('flow.tools')}</div>
          <FormContainer>
            <FormField
              control={form.control}
              name="items"
              render={() => (
                <FormItem className="space-y-2">
                  {Object.entries(data.variables?.tools || {}).map(
                    ([name, mcp]) => (
                      <FormField
                        key={name}
                        control={form.control}
                        name="items"
                        render={({ field }) => {
                          return (
                            <FormItem
                              key={name}
                              className="flex flex-row items-center gap-2"
                            >
                              <FormControl>
                                <MCPCard key={name} data={{ ...mcp, name }}>
                                  <Checkbox
                                    className="translate-y-0.5"
                                    checked={field.value?.includes(name)}
                                    onCheckedChange={(checked) => {
                                      return checked
                                        ? field.onChange([...field.value, name])
                                        : field.onChange(
                                            field.value?.filter(
                                              (value) => value !== name,
                                            ),
                                          );
                                    }}
                                  />
                                </MCPCard>
                              </FormControl>
                            </FormItem>
                          );
                        }}
                      />
                    ),
                  )}
                  <FormMessage />
                </FormItem>
              )}
            />
          </FormContainer>
        </div>
      </form>
    </Form>
  );
}

export default memo(MCPForm);
