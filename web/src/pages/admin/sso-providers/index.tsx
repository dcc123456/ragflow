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

import { SSO_CLOUD_IDP_PROVIDERS } from './cloud-idp';
import SSOProviderLDAPList from './ldap-list';

import { useSSOVariables } from '../hooks/useSSOVariables';

function AdminSSOProviders() {
  const { t } = useTranslation();
  const { providerType, switchProviderType } = useSSOVariables();

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
                onClick={() => {
                  if (providerType !== 'none') {
                    switchProviderType('none');
                  }
                }}
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
                        onClick={() => {
                          if (providerType !== 'idp') {
                            switchProviderType('idp');
                          }
                        }}
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
                        onClick={() => {
                          if (providerType !== 'ldap') {
                            switchProviderType('ldap');
                          }
                        }}
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
            </div>
          </ScrollArea>
        </CardContent>
      </Card>
    </>
  );
}

export default AdminSSOProviders;
