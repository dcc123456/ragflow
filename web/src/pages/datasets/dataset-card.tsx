import { HomeCard } from '@/components/home-card';
import { MoreButton } from '@/components/more-button';
import { SharedBadge } from '@/components/shared-badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { useTranslate } from '@/hooks/common-hooks';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import { useTraceDuplicate } from '@/hooks/use-knowledge-request';
import { IKnowledge } from '@/interfaces/database/knowledge';
import { cn } from '@/lib/utils';
import { ChevronRight, LucideCircleX } from 'lucide-react';
import { ProcessingType } from '../dataset/dataset-overview/dataset-common';
import { useUnBindTask } from '../dataset/dataset/generate-button/hook';
import { DatasetDropdown } from './dataset-dropdown';
import useDuplicateDataset from './use-duplicate-dataset';
import { useRenameDataset } from './use-rename-dataset';

export type DatasetCardProps = {
  dataset: IKnowledge;
  showPrivilegeModal(): void;
} & Pick<ReturnType<typeof useRenameDataset>, 'showDatasetRenameModal'> & {
    showDatasetDuplicateModal: ReturnType<
      typeof useDuplicateDataset
    >['showModal'];
  };

export function DatasetCard({
  dataset,
  showDatasetRenameModal,
  showDatasetDuplicateModal,
  showPrivilegeModal,
}: DatasetCardProps) {
  const { t } = useTranslate('knowledgeList');
  const { navigateToDataset } = useNavigatePage();
  const { hasProgress = false, progress } = useTraceDuplicate(dataset.id);
  const { handleUnbindTask } = useUnBindTask();

  return (
    <HomeCard
      className={cn(hasProgress && 'text-text-disabled')}
      data={{ ...dataset, description: `${dataset.doc_num} files` }}
      moreDropdown={
        !hasProgress ? (
          <DatasetDropdown
            showDatasetRenameModal={showDatasetRenameModal}
            showDatasetDuplicateModal={showDatasetDuplicateModal}
            showPrivilegeModal={showPrivilegeModal}
            dataset={dataset}
          >
            <MoreButton></MoreButton>
          </DatasetDropdown>
        ) : null
      }
      sharedBadge={<SharedBadge>{dataset.nickname}</SharedBadge>}
      // Prevent navigate to dataset details if is duplicating
      onClick={hasProgress ? undefined : navigateToDataset(dataset.id)}
    >
      {hasProgress && (
        <>
          <p className="text-text-secondary">{t('duplicatingTip')}</p>

          <div className="flex items-center gap-2 text-text-primary">
            <Progress className="h-1" value={progress * 100} />
            <span>{`${(progress * 100).toFixed(0)}%`}</span>

            <Button
              variant="danger"
              size="icon"
              className="
                !border-none !bg-transparent p-0 size-auto
                hover:text-colors-text-functional-danger
                focus-visible:text-colors-text-functional-danger"
              onClick={() => handleUnbindTask({ type: ProcessingType.clone })}
            >
              <LucideCircleX />
            </Button>
          </div>
        </>
      )}
    </HomeCard>
  );
}

export function SeeAllCard() {
  const { navigateToDatasetList } = useNavigatePage();

  return (
    <Card
      className="w-full flex-none h-full cursor-pointer"
      onClick={() => navigateToDatasetList({ isCreate: false })}
    >
      <CardContent className="p-2.5 pt-1 w-full h-full flex items-center justify-center gap-1.5 text-text-secondary">
        {t('common.seeAll')} <ChevronRight className="size-4" />
      </CardContent>
    </Card>
  );
}
