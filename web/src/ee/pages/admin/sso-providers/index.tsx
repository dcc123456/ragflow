import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { LucideCircle, LucideCircleDot } from 'lucide-react';

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';

import Spotlight from '@/components/spotlight';
import { ScrollArea } from '@/components/ui/scroll-area';
import { cn } from '@/lib/utils';

import { SSO_CLOUD_IDP_PROVIDERS } from './cloud-idp';
import SSOProviderLDAPList from './ldap-list';
import SSOProviderCASList from './cas';

import {
  type CASChannel,
  type ProviderType,
  hasCASConnectionSettings,
  useSSOVariables,
} from '@/ee/pages/admin/hooks/useSSOVariables';

function AdminSSOProviders() {
  const { t } = useTranslation();
  const [isConfiguringCAS, setIsConfiguringCAS] = useState(false);
  const {
    variables,
    providerType: activeProviderType,
    switchProviderType,
    isFetching,
    isUpdating,
  } = useSSOVariables();
  const providerType = isConfiguringCAS ? 'cas' : activeProviderType;
  const isCASSelected = providerType === 'cas';
  const casProviders = variables.cas;
  const firstReadyCASChannel = (
    Object.keys(casProviders).sort() as CASChannel[]
  ).find((channel) => hasCASConnectionSettings(casProviders[channel]));
  const casBusy = isFetching || isUpdating;

  const handleSelectProvider = (type: Exclude<ProviderType, 'cas'>) => {
    setIsConfiguringCAS(false);
    if (type !== activeProviderType) {
      return switchProviderType(type);
    }
  };

  const handleSelectCAS = () => {
    if (firstReadyCASChannel) {
      return switchProviderType('cas', firstReadyCASChannel);
    }
    setIsConfiguringCAS(true);
  };

  const handleEnableCAS = async (channel: CASChannel) => {
    await switchProviderType('cas', channel);
    setIsConfiguringCAS(false);
  };

  return (
    <>
      <Card className="!shadow-none relative h-full bg-transparent rounded-xl overflow-hidden flex flex-col">
        <Spotlight />

        <CardHeader className="border-b-0.5 border-border-button">
          <CardTitle className="h-10 flex items-center">
            {t('admin.ssoProvider')}
          </CardTitle>

          <CardDescription className="text-text-secondary">
            {t('admin.ssoProviderSubtitle')}
          </CardDescription>
        </CardHeader>

        <CardContent className="p-0 flex-1 h-0">
          <ScrollArea className="h-full">
            <div
              className="p-6 w-1/2 max-w-6xl min-w-[50rem] space-y-10"
              role="radiogroup"
            >
              <button
                type="button"
                className="group block w-full text-left"
                role="radio"
                aria-checked={providerType === 'none'}
                aria-disabled={providerType === 'none'}
                data-state={providerType === 'none' ? 'on' : 'off'}
                onClick={() => handleSelectProvider('none')}
                disabled={providerType === 'none'}
              >
                <Card className="bg-transparent">
                  <CardHeader className="p-4">
                    <CardTitle className="text-lg space-y-0 flex justify-between items-center gap-3 text-text-disabled group-aria-checked:text-text-primary transition-colors">
                      <span>
                        {t('admin.ssoProviderRadioGroup.values.none')}
                      </span>

                      {providerType === 'none' ? (
                        <LucideCircleDot className="size-[1em] text-accent-primary" />
                      ) : (
                        <LucideCircle className="size-[1em] text-border-button" />
                      )}
                    </CardTitle>
                  </CardHeader>
                </Card>
              </button>

              <div
                className="group"
                role="radio"
                aria-checked={providerType === 'idp'}
                aria-disabled={providerType === 'idp'}
                data-state={providerType === 'idp' ? 'on' : 'off'}
              >
                <Card className="bg-transparent">
                  <CardHeader className="p-4">
                    <CardTitle>
                      <button
                        type="button"
                        className="w-full text-lg space-y-0 flex justify-between items-center gap-3 text-text-disabled group-aria-checked:text-text-primary transition-colors"
                        disabled={providerType === 'idp'}
                        onClick={() => handleSelectProvider('idp')}
                      >
                        <span>
                          {t('admin.ssoProviderRadioGroup.values.cloud_idp')}
                        </span>

                        {providerType === 'idp' ? (
                          <LucideCircleDot className="size-[1em] text-accent-primary" />
                        ) : (
                          <LucideCircle className="size-[1em] text-border-button" />
                        )}
                      </button>
                    </CardTitle>
                  </CardHeader>

                  <CardContent className="px-4 pb-4 space-y-2 opacity-50 group-aria-checked:opacity-100 transition-opacity">
                    {SSO_CLOUD_IDP_PROVIDERS.map(
                      ({ key, Card: ProviderCard }) => (
                        <ProviderCard
                          key={key}
                          disabled={providerType !== 'idp'}
                        />
                      ),
                    )}
                  </CardContent>
                </Card>
              </div>

              <div
                className="group"
                role="radio"
                aria-checked={providerType === 'ldap'}
                aria-disabled={providerType === 'ldap'}
                data-state={providerType === 'ldap' ? 'on' : 'off'}
              >
                <Card className="bg-transparent">
                  <CardHeader className="p-4">
                    <CardTitle>
                      <button
                        className="w-full text-lg space-y-0 flex justify-between items-center gap-3 text-text-disabled group-aria-checked:text-text-primary transition-colors"
                        type="button"
                        disabled={providerType === 'ldap'}
                        onClick={() => handleSelectProvider('ldap')}
                      >
                        <span>
                          {t('admin.ssoProviderRadioGroup.values.ldap')}
                        </span>

                        {providerType === 'ldap' ? (
                          <LucideCircleDot className="size-[1em] text-accent-primary" />
                        ) : (
                          <LucideCircle className="size-[1em] text-border-button" />
                        )}
                      </button>
                    </CardTitle>
                  </CardHeader>

                  <CardContent className="px-4 pb-4 opacity-50 group-aria-checked:opacity-100 transition-opacity">
                    <SSOProviderLDAPList disabled={providerType !== 'ldap'} />
                  </CardContent>
                </Card>
              </div>
              <div>
                <Card className="bg-transparent">
                  <CardHeader className="p-4">
                    <CardTitle>
                      <button
                        type="button"
                        role="radio"
                        aria-checked={isCASSelected}
                        className="w-full text-lg flex justify-between items-center gap-3 text-text-disabled aria-checked:text-text-primary transition-colors"
                        disabled={isCASSelected || casBusy}
                        onClick={handleSelectCAS}
                      >
                        <span>
                          {t('admin.ssoProviderRadioGroup.values.cas')}
                        </span>
                        {isCASSelected ? (
                          <LucideCircleDot className="size-[1em] text-accent-primary" />
                        ) : (
                          <LucideCircle className="size-[1em] text-border-button" />
                        )}
                      </button>
                    </CardTitle>
                  </CardHeader>
                  <CardContent
                    className={cn(
                      'px-4 pb-4 transition-opacity',
                      isCASSelected ? 'opacity-100' : 'opacity-50',
                    )}
                  >
                    <SSOProviderCASList
                      providers={casProviders}
                      disabled={!isCASSelected || casBusy}
                      onEnable={handleEnableCAS}
                    />
                  </CardContent>
                </Card>
              </div>
            </div>
          </ScrollArea>
        </CardContent>
      </Card>
    </>
  );
}

export default AdminSSOProviders;
