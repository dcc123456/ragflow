import Empty from '@/components/empty/empty';
import { Button } from '@/components/ui/button';
import { useModelGroupList } from '@/ee/hooks/use-model-group-request';
import { Loader2, Plus } from 'lucide-react';
import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { CreateModelGroupDialog } from './create-model-group-dialog';
import { ModelGroupList } from './model-group-list';

export default function ModelGroupPage() {
  const { t } = useTranslation();
  const [createOpen, setCreateOpen] = useState(false);
  const { data: groups = [], loading, isError, refetch } = useModelGroupList();

  const handleRetry = useCallback(() => {
    void refetch();
  }, [refetch]);
  const showCreateDialog = useCallback(() => setCreateOpen(true), []);
  const hideCreateDialog = useCallback(() => setCreateOpen(false), []);

  return (
    <section className="flex w-full flex-1 min-w-0 min-h-0 flex-col gap-4 p-6">
      <header className="flex shrink-0 items-center justify-between gap-4">
        <h1 className="min-w-0 truncate text-lg font-semibold">
          {t('setting.modelGroupPage.title')}
        </h1>
        <Button type="button" className="shrink-0" onClick={showCreateDialog}>
          <Plus />
          {t('setting.modelGroupPage.newGroup')}
        </Button>
      </header>
      {loading ? (
        <div role="status" className="flex flex-1 items-center justify-center">
          <Loader2
            className="animate-spin"
            aria-label={t('setting.modelGroupPage.loading')}
          />
        </div>
      ) : isError ? (
        <div
          role="alert"
          className="flex flex-1 flex-col items-center justify-center gap-3"
        >
          <p className="text-sm text-state-error">
            {t('setting.modelGroupPage.loadFailed')}
          </p>
          <Button type="button" variant="outline" onClick={handleRetry}>
            {t('setting.modelGroupPage.retry')}
          </Button>
        </div>
      ) : groups.length === 0 ? (
        <div className="flex flex-1 items-center justify-center">
          <Empty text={t('setting.modelGroupPage.empty')} />
        </div>
      ) : (
        <ModelGroupList groups={groups} />
      )}
      {createOpen && (
        <CreateModelGroupDialog groups={groups} hideModal={hideCreateDialog} />
      )}
    </section>
  );
}
