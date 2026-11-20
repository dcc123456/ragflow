import { zodResolver } from '@hookform/resolvers/zod';
import { mapValues } from 'lodash';
import { LucideSettings } from 'lucide-react';
import { useId, useState } from 'react';
import {
  type Control,
  type DefaultValues,
  useForm,
  useWatch,
} from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import z from 'zod';

import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { TruncatedText } from '@/components/truncated-text';
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { useSetModalState } from '@/hooks/common-hooks';
import {
  type CASChannel,
  type CASProviderVariables,
  type GroupedCASVariables,
  hasCASConnectionSettings,
  useMutateIdpProvider,
} from '@/ee/pages/admin/hooks/useSSOVariables';
import { uuidParts } from '@/ee/pages/admin/utils';

import {
  SSOProviderAddButton,
  SSOProviderFormDialog,
} from './provider-form-dialog';

const CASCallbackPath = '/api/v1/auth/cas/callback';

enum CASProtocolVersion {
  V1 = '1',
  V2 = '2',
  V3 = '3',
}

enum CASAttributeSource {
  Username = 'username',
  Attribute = 'attribute',
}

const CASProtocolOptions = Object.values(CASProtocolVersion).map((value) => ({
  value,
  label: `CAS ${value}.0`,
}));

const CASFieldLabels = {
  name: 'name',
  protocol_version: 'protocolVersion',
  login_url: 'loginUrl',
  validation_url: 'validationUrl',
  redirect_uri: 'redirectUri',
  email_attribute: 'emailAttribute',
  nickname_attribute: 'nicknameAttribute',
} as const;

const CASURLSchema = z
  .string()
  .trim()
  .url()
  .refine((value) => {
    let url: URL;
    try {
      url = new URL(value);
    } catch {
      return false;
    }
    const loopback =
      url.hostname === 'localhost' ||
      url.hostname === '[::1]' ||
      /^127\.\d+\.\d+\.\d+$/.test(url.hostname);
    return (
      (url.protocol === 'https:' || (url.protocol === 'http:' && loopback)) &&
      !url.username &&
      !url.password &&
      !url.search &&
      !value.includes('?') &&
      !value.includes('#')
    );
  }, 'Use HTTPS, or HTTP on localhost, without credentials, query parameters or fragments.');

type CASFieldName = keyof typeof CASFieldLabels;

function SSOProviderCASField({
  control,
  name,
  required = true,
  placeholder,
  hint,
  options,
}: {
  control: Control<SchemaType>;
  name: CASFieldName;
  required?: boolean;
  placeholder?: string;
  hint?: string;
  options?: { value: string; label: string }[];
}) {
  const { t } = useTranslation();
  const label = CASFieldLabels[name];

  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem className="min-w-0">
          <FormLabel required={required}>
            {t(`admin.ssoProviderTabs.cas.fields.${label}`)}
          </FormLabel>
          {options ? (
            <Select value={field.value} onValueChange={field.onChange}>
              <FormControl>
                <SelectTrigger className="h-10">
                  <SelectValue />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {options.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <FormControl>
              <Input {...field} className="h-10" placeholder={placeholder} />
            </FormControl>
          )}
          {hint && <FormDescription>{hint}</FormDescription>}
          <FormMessage />
        </FormItem>
      )}
    />
  );
}

function SSOProviderCASForm({
  id,
  isSaved,
  defaultValues,
  onSubmit,
}: {
  id: string;
  isSaved: boolean;
  defaultValues: DefaultValues<SchemaType> & {
    email_source?: string;
    nickname_source?: string;
  };
  onSubmit: (data: SchemaType) => Promise<void>;
}) {
  const { t } = useTranslation();
  const savedEmailAttribute =
    defaultValues.email_attribute?.trim() ||
    (isSaved && !defaultValues.email_source ? 'mail' : '');
  const savedNicknameAttribute =
    defaultValues.nickname_attribute?.trim() ||
    (isSaved && !defaultValues.nickname_source ? 'displayName' : '');
  const useUsernameForEmail =
    defaultValues.email_source === CASAttributeSource.Username ||
    (!defaultValues.email_source && savedEmailAttribute === 'user');
  const useUsernameForNickname =
    defaultValues.nickname_source === CASAttributeSource.Username ||
    (!defaultValues.nickname_source && savedNicknameAttribute === 'user');
  const form = useForm<SchemaType>({
    resolver: zodResolver(CASFormSchema),
    defaultValues: {
      name: '',
      login_url: '',
      validation_url: '',
      protocol_version: CASProtocolVersion.V3,
      ...defaultValues,
      email_attribute: useUsernameForEmail ? '' : savedEmailAttribute,
      nickname_attribute: useUsernameForNickname ? '' : savedNicknameAttribute,
      redirect_uri: defaultValues.redirect_uri ?? '',
    },
  });
  const { control } = form;
  const protocolVersion = useWatch({ control, name: 'protocol_version' });
  const isCAS1 = protocolVersion === CASProtocolVersion.V1;

  return (
    <Form {...form}>
      <form
        id={id}
        className="space-y-4"
        spellCheck={false}
        autoComplete="off"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <div className="grid gap-x-4 gap-y-2 sm:grid-cols-[minmax(0,1fr)_10rem]">
          <SSOProviderCASField control={control} name="name" />
          <SSOProviderCASField
            control={control}
            name="protocol_version"
            options={CASProtocolOptions}
          />
          {isCAS1 && (
            <p className="text-sm text-text-secondary sm:col-span-2">
              {t('admin.ssoProviderTabs.cas.description.emailFromUsername')}
            </p>
          )}
        </div>
        <SSOProviderCASField control={control} name="login_url" />
        <SSOProviderCASField control={control} name="validation_url" />
        <SSOProviderCASField
          control={control}
          name="redirect_uri"
          placeholder={`https://<domain>${CASCallbackPath}`}
          hint={t('admin.ssoProviderTabs.cas.description.redirectUri')}
        />
        {!isCAS1 && (
          <>
            <SSOProviderCASField
              control={control}
              name="email_attribute"
              placeholder="email"
              required={false}
              hint={t('admin.ssoProviderTabs.cas.description.emailAttribute')}
            />
            <SSOProviderCASField
              control={control}
              name="nickname_attribute"
              placeholder="nickname"
              required={false}
              hint={t(
                'admin.ssoProviderTabs.cas.description.nicknameAttribute',
              )}
            />
          </>
        )}
      </form>
    </Form>
  );
}

const CASFormSchema = z.object({
  name: z.string().trim().min(1).max(128),
  login_url: CASURLSchema,
  validation_url: CASURLSchema,
  redirect_uri: CASURLSchema.refine((value) => {
    try {
      return new URL(value).pathname === CASCallbackPath;
    } catch {
      return false;
    }
  }, `Use ${CASCallbackPath} as the callback path.`),
  protocol_version: z.nativeEnum(CASProtocolVersion),
  email_attribute: z.string().trim(),
  nickname_attribute: z.string().trim(),
});

type SchemaType = z.infer<typeof CASFormSchema>;

function SSOProviderCASDialog({
  channel,
  provider,
  open,
  onClose,
}: {
  channel: CASChannel;
  provider?: CASProviderVariables;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { update, isUpdating } = useMutateIdpProvider(channel);
  const formId = useId();
  const providerName =
    provider?.name?.value?.trim() ||
    t('admin.ssoProviderRadioGroup.values.cas');

  const handleSubmit = async (data: SchemaType) => {
    const settings = {
      ...data,
      email_source: data.email_attribute
        ? CASAttributeSource.Attribute
        : CASAttributeSource.Username,
      nickname_source: data.nickname_attribute
        ? CASAttributeSource.Attribute
        : CASAttributeSource.Username,
    };
    await update(provider ? settings : { ...settings, enabled: false });
    onClose();
  };

  return (
    <SSOProviderFormDialog
      open={open}
      onClose={onClose}
      title={
        provider
          ? t('admin.ssoProviderTabs.cas.dialogTitle', { name: providerName })
          : t('admin.ssoProviderTabs.cas.addDialogTitle')
      }
      formId={formId}
      submitLabel={t(provider ? 'common.save' : 'common.add')}
      pending={isUpdating}
      className="max-h-[calc(100dvh_-_2rem)] w-[calc(100%_-_2rem)] grid-rows-[auto_minmax(0,1fr)_auto] sm:max-w-2xl"
      bodyClassName="min-h-0 overflow-y-auto py-1"
    >
      <SSOProviderCASForm
        id={formId}
        isSaved={Boolean(provider)}
        defaultValues={mapValues(provider, 'value')}
        onSubmit={handleSubmit}
      />
    </SSOProviderFormDialog>
  );
}

function SSOProviderCASListItem({
  channel,
  provider,
  disabled,
  onEnable,
}: {
  channel: CASChannel;
  provider: CASProviderVariables;
  disabled: boolean;
  onEnable: (channel: CASChannel) => Promise<unknown> | undefined;
}) {
  const { t } = useTranslation();
  const { disable, isSwitchingState } = useMutateIdpProvider(channel);
  const { visible, showModal, hideModal } = useSetModalState();
  const enabled = Boolean(provider.enabled?.value);
  const isConfigured = hasCASConnectionSettings(provider);
  const providerName =
    provider.name?.value?.trim() || t('admin.ssoProviderRadioGroup.values.cas');

  const handleEnabledChange = async (checked: boolean) => {
    await (checked ? onEnable(channel) : disable());
  };

  return (
    <li>
      <Card className="bg-transparent">
        <CardContent className="flex items-center gap-4 p-4">
          <TruncatedText
            as="span"
            className="min-w-0 flex-1 truncate text-base"
            tooltip={providerName}
          >
            {providerName}
          </TruncatedText>
          <div className="ml-auto flex shrink-0 items-center gap-2">
            <Switch
              className="mr-4"
              aria-label={providerName}
              checked={enabled}
              disabled={
                disabled || isSwitchingState || (!enabled && !isConfigured)
              }
              onCheckedChange={handleEnabledChange}
            />
            <Button
              type="button"
              variant="transparent"
              size="icon"
              className="size-8 border-none"
              aria-label={t('admin.ssoProviderTabs.cas.dialogTitle', {
                name: providerName,
              })}
              onClick={showModal}
              disabled={disabled || isSwitchingState}
            >
              <LucideSettings />
            </Button>
          </div>
        </CardContent>
      </Card>
      <SSOProviderCASDialog
        channel={channel}
        provider={provider}
        open={visible}
        onClose={hideModal}
      />
    </li>
  );
}

export default function SSOProviderCASList({
  providers,
  disabled,
  onEnable,
}: {
  providers: GroupedCASVariables;
  disabled: boolean;
  onEnable: (channel: CASChannel) => Promise<unknown> | undefined;
}) {
  const { t } = useTranslation();
  const [newChannel, setNewChannel] = useState<CASChannel>();
  const openAddDialog = () =>
    setNewChannel(('cas-' + uuidParts().join('')) as CASChannel);
  const closeAddDialog = () => setNewChannel(undefined);

  return (
    <ul className="list-none space-y-2">
      {(Object.keys(providers).sort() as CASChannel[]).map((channel) => {
        const provider = providers[channel];
        if (
          !provider ||
          !(
            provider.name?.value ||
            provider.login_url?.value ||
            provider.validation_url?.value
          )
        ) {
          return null;
        }
        return (
          <SSOProviderCASListItem
            key={channel}
            channel={channel}
            provider={provider}
            disabled={disabled}
            onEnable={onEnable}
          />
        );
      })}
      <li>
        <SSOProviderAddButton onClick={openAddDialog} disabled={disabled}>
          {t('common.add')}
        </SSOProviderAddButton>
        {newChannel && (
          <SSOProviderCASDialog
            channel={newChannel}
            open
            onClose={closeAddDialog}
          />
        )}
      </li>
    </ul>
  );
}
