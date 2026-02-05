import { useNavigate, useParams } from 'react-router';

import { LucideArrowLeft } from 'lucide-react';

import Spotlight from '@/components/spotlight';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { ScrollArea } from '@/components/ui/scroll-area';

import { useTranslate } from '@/hooks/common-hooks';
import { cn } from '@/lib/utils';
import { Routes } from '@/routes';

import AddedModels from './components/added-models';
import AvailableFactoryList from './components/available-factory-list';
import DefaultModelsSettings from './components/default-model-settings';

export default function AdminRoleConfigModelProviders() {
  const { roleName } = useParams();
  const navigate = useNavigate();

  const { t } = useTranslate('admin.roleModelProviders');
  const { t: tAdmin } = useTranslate('admin');

  return (
    <section className="px-10 py-5 size-full flex flex-col">
      <nav className="mb-5">
        <Button
          variant="outline"
          className="h-10 px-3 dark:bg-bg-input dark:border-border-button"
          onClick={() => navigate(`${Routes.AdminRoles}`)}
        >
          <LucideArrowLeft />
          <span>{tAdmin('back')}</span>
        </Button>
      </nav>

      <Card className="!shadow-none relative h-full border-0.5 border-border-button bg-transparent rounded-xl overflow-hidden">
        <Spotlight />

        <div
          className={cn(
            'p-0 overflow-hidden',
            'size-full grid grid-rows-[auto_minmax(0,1fr)] grid-cols-[2fr_minmax(auto,1fr)]',
            '[grid-template-areas:"title_aside"_"content_aside"]',
          )}
        >
          <ScrollArea className="[grid-area:content] flex-1 size-full">
            <CardContent className="p-6 space-y-12">
              <Card className="flex-none bg-transparent border-0">
                <CardHeader className="p-0 pb-6">
                  <CardTitle className="text-2xl font-semibold leading-none tracking-tight h-10 flex items-center">
                    {t('setDefaultModels', { role: roleName })}
                  </CardTitle>
                </CardHeader>

                <CardContent className="p-6 border-0.5 border-border-button rounded-lg">
                  <DefaultModelsSettings />
                </CardContent>
              </Card>

              <Card className="bg-transparent border-0 size-full">
                <CardHeader className="p-0 pb-6">
                  <CardTitle>{t('addedModels')}</CardTitle>
                </CardHeader>

                <CardContent className="p-0">
                  <AddedModels />
                </CardContent>
              </Card>
            </CardContent>
          </ScrollArea>

          <aside
            className={cn(
              '[grid-area:aside]',
              'flex flex-col size-full min-w-80',
              'border-l-0.5 border-border-button overflow-hidden',
            )}
          >
            <AvailableFactoryList />
          </aside>
        </div>
      </Card>
    </section>
  );
}
