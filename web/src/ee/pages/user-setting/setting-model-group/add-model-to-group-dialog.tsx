import { DirectModelTreeSelect } from '@/components/model-tree-select';
import { Button, ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useAddModelGroupMember } from '@/ee/hooks/use-model-group-request';
import type { IModelGroupMember } from '@/ee/interfaces/database/model-group';
import { useCallback, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

const ModelGroupMemberModelTypes = [
  'chat',
  'vision',
  'decision',
  'embedding',
  'asr',
  'rerank',
  'tts',
  'ocr',
];

export function AddModelToGroupDialog({
  groupId,
  members,
  hideModal,
}: {
  groupId: string;
  members: IModelGroupMember[];
  hideModal: () => void;
}) {
  const { t } = useTranslation();
  const [modelId, setModelId] = useState('');
  const submittingRef = useRef(false);
  const { loading, run } = useAddModelGroupMember(groupId);
  const duplicate = members.some((member) => member.model_id === modelId);

  const handleModelChange = useCallback((value: string) => {
    setModelId(value);
  }, []);

  const handleSubmit = useCallback(async () => {
    if (!modelId || duplicate || loading || submittingRef.current) return;
    submittingRef.current = true;
    try {
      const code = await run({ model_id: modelId });
      if (code === 0) hideModal();
    } catch {
      // The request layer reports the error; preserve the selected model.
    } finally {
      submittingRef.current = false;
    }
  }, [duplicate, hideModal, loading, modelId, run]);

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[520px]" aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>
            {t('setting.modelGroupPage.addMemberTitle')}
          </DialogTitle>
        </DialogHeader>
        <DirectModelTreeSelect
          value={modelId}
          onChange={handleModelChange}
          modelTypes={ModelGroupMemberModelTypes}
          requireModelId={true}
          allowClear
        />
        {duplicate && (
          <p role="alert" className="text-sm text-state-error">
            {t('setting.modelGroupPage.memberAlreadyAdded')}
          </p>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={hideModal}>
            {t('common.cancel')}
          </Button>
          <ButtonLoading
            loading={loading}
            disabled={!modelId || duplicate}
            onClick={handleSubmit}
          >
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
