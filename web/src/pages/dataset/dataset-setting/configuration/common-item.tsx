import { Trans, useTranslation } from 'react-i18next';

import {
  FormFieldConfig,
  FormFieldType,
  RenderField,
} from '@/components/dynamic-form';
import {
  SelectWithSearch,
  SelectWithSearchFlagOptionType,
} from '@/components/originui/select-with-search';
import { SliderInputFormField } from '@/components/slider-input-form-field';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Progress } from '@/components/ui/progress';
import { Radio } from '@/components/ui/radio';
import { Switch } from '@/components/ui/switch';
import { LlmModelType } from '@/constants/knowledge';
import { useSetModalState, useTranslate } from '@/hooks/common-hooks';
import { useComposeLlmOptionsByModelTypes } from '@/hooks/use-llm-request';
import { cn } from '@/lib/utils';
import { history } from '@/utils/simple-history-util';
import { Settings } from 'lucide-react';
import { useCallback, useContext, useEffect, useRef } from 'react';
import {
  type ControllerRenderProps,
  type FieldValues,
  useFormContext,
} from 'react-hook-form';
import { useLocation } from 'react-router';
import { DataSetContext } from '..';
import {
  MetadataType,
  useManageMetadata,
  util,
} from '../../components/metedata/hooks/use-manage-modal';
import { IMetaDataReturnJSONSettings } from '../../components/metedata/interface';
import { ManageMetadataModal } from '../../components/metedata/manage-modal';
import {
  // useHandleKbEmbedding,
  useHasParsedDocument,
  useKbSwitchEmbeddingModel,
  useSelectChunkMethodList,
  useSelectEmbeddingModelOptions,
  useTraceEmbedding,
} from '../hooks';

interface IProps {
  line?: 1 | 2;
  isEdit?: boolean;
  label?: string;
  name?: string;
}
export function ChunkMethodItem(props: IProps) {
  const { line } = props;
  const { t } = useTranslate('knowledgeConfiguration');
  const form = useFormContext();
  // const handleChunkMethodSelectChange = useHandleChunkMethodSelectChange(form);
  const parserList = useSelectChunkMethodList();

  return (
    <FormField
      control={form.control}
      name={'parser_id'}
      render={({ field }) => (
        <FormItem className=" items-center space-y-1">
          <div className={line === 1 ? 'flex items-center' : ''}>
            <FormLabel
              required
              tooltip={t('chunkMethodTip')}
              className={cn('text-sm', {
                'w-1/4 whitespace-pre-wrap': line === 1,
              })}
            >
              {t('builtIn')}
            </FormLabel>
            <div className={line === 1 ? 'w-3/4 ' : 'w-full'}>
              <FormControl>
                <SelectWithSearch
                  {...field}
                  options={parserList}
                  placeholder={t('chunkMethodPlaceholder')}
                />
              </FormControl>
            </div>
          </div>
          <div className="flex pt-1">
            <div className={line === 1 ? 'w-1/4' : ''}></div>
            <FormMessage />
          </div>
        </FormItem>
      )}
    />
  );
}

export const EmbeddingSelect = ({
  isEdit,
  field,
  name,
  disabled: propDisabled = false,
}: {
  isEdit: boolean;
  field: ControllerRenderProps<FieldValues, 'embd_id'>;
  name?: string;
  disabled?: boolean;
}) => {
  const { t } = useTranslate('knowledgeConfiguration');
  const { t: tCommon } = useTranslate('common');

  const form = useFormContext();
  const embeddingModelOptions = useSelectEmbeddingModelOptions();
  // const { handleChange } = useHandleKbEmbedding();
  const { switchEmbeddingModel, isLoading: isSwitchingModel } =
    useKbSwitchEmbeddingModel();

  const { visible, showModal, hideModal } = useSetModalState(false);

  const { hasProgress: isReEmbedding, progress } = useTraceEmbedding();

  const oldValue: string = form.getValues(name || 'embd_id');
  const nextValueRef = useRef<string>('');

  const disabled = (!isEdit && propDisabled) || field.disabled || isReEmbedding;

  return (
    <>
      <div className="relative">
        <SelectWithSearch
          onChange={(value) => {
            // Only pops modal when in dataset configuration page
            if (isEdit) {
              if (value !== oldValue && !disabled) {
                nextValueRef.current = value;
                showModal();
              }
            } else {
              field.onChange(value);
            }
          }}
          disabled={disabled}
          value={field.value}
          triggerClassName="flex"
          options={embeddingModelOptions}
          placeholder={t('embeddingModelPlaceholder')}
        />

        {isReEmbedding && (
          <Progress
            value={progress * 100}
            className="absolute bottom-0 left-0 w-full h-1 bg-bg-component rounded-t-none rounded-b-lg"
          />
        )}
      </div>

      <Dialog
        open={!disabled && visible}
        onOpenChange={() => !isSwitchingModel && hideModal()}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('switchEmbeddingModel.title')}</DialogTitle>
          </DialogHeader>

          <DialogDescription asChild>
            <div className="text-sm text-text-primary">
              <Trans
                t={t}
                i18nKey="switchEmbeddingModel.description"
                components={{
                  p: <p className="mb-4" />,
                  ol: (
                    <ol className="mb-4 list-decimal list-inside text-state-error" />
                  ),
                  li: <li />,
                }}
              />
            </div>
          </DialogDescription>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={isSwitchingModel}
              onClick={() => !isSwitchingModel && hideModal()}
            >
              {tCommon('cancel')}
            </Button>

            <Button
              type="button"
              variant="destructive"
              loading={isSwitchingModel || disabled}
              onClick={async () => {
                if (isSwitchingModel || disabled) {
                  return;
                }

                await switchEmbeddingModel(nextValueRef.current);
                field.onChange(nextValueRef.current);
                hideModal();
              }}
            >
              {tCommon('confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};

export function EmbeddingModelItem({ line = 1, isEdit }: IProps) {
  const { hasProgress: isReEmbedding } = useTraceEmbedding();

  const { t } = useTranslate('knowledgeConfiguration');
  const form = useFormContext();
  const disabled = useHasParsedDocument(isEdit) || isReEmbedding;

  return (
    <>
      <FormField
        control={form.control}
        name={'embd_id'}
        render={({ field }) => (
          <FormItem className={cn(' items-center space-y-0 ')}>
            <div
              className={cn('flex', {
                'items-center': line === 1,
                'flex-col gap-1': line === 2,
              })}
            >
              <FormLabel
                required
                tooltip={t('embeddingModelTip')}
                className={cn('text-sm  whitespace-wrap ', {
                  'w-1/4': line === 1,
                })}
              >
                {t('embeddingModel')}
              </FormLabel>
              <div
                className={cn('text-muted-foreground', { 'w-3/4': line === 1 })}
              >
                <FormControl>
                  <EmbeddingSelect
                    isEdit={!!isEdit}
                    field={field}
                    disabled={disabled}
                  ></EmbeddingSelect>
                </FormControl>
              </div>
            </div>
            <div className="flex pt-1">
              <div className={line === 1 ? 'w-1/4' : ''}></div>
              <FormMessage />
            </div>
          </FormItem>
        )}
      />
    </>
  );
}

export function ParseTypeItem({ line = 2 }: { line?: number }) {
  const { t } = useTranslate('knowledgeConfiguration');
  const form = useFormContext();

  return (
    <FormField
      control={form.control}
      name={'parseType'}
      render={({ field }) => (
        <FormItem className=" items-center space-y-0 ">
          <div
            className={cn('flex', {
              ' items-center': line === 1,
              'flex-col gap-1': line === 2,
            })}
          >
            <FormLabel
              // tooltip={t('parseTypeTip')}
              className={cn('text-sm  whitespace-wrap ', {
                'w-1/4': line === 1,
              })}
            >
              {t('parseType')}
            </FormLabel>
            <div
              className={cn('text-muted-foreground', { 'w-3/4': line === 1 })}
            >
              <FormControl>
                <Radio.Group {...field}>
                  <div
                    className={cn(
                      'flex gap-2 justify-between text-muted-foreground',
                      line === 1 ? 'w-1/2' : 'w-3/4',
                    )}
                  >
                    <Radio value={1}>{t('builtIn')}</Radio>
                    <Radio value={2}>{t('manualSetup')}</Radio>
                  </div>
                </Radio.Group>
              </FormControl>
            </div>
          </div>
          <div className="flex pt-1">
            <div className={line === 1 ? 'w-1/4' : ''}></div>
            <FormMessage />
          </div>
        </FormItem>
      )}
    />
  );
}

export function EnableAutoGenerateItem() {
  const { t } = useTranslate('knowledgeConfiguration');
  const form = useFormContext();

  return (
    <FormField
      control={form.control}
      name={'enableAutoGenerate'}
      render={({ field }) => (
        <FormItem className=" items-center space-y-0 ">
          <div className="flex items-center">
            <FormLabel
              tooltip={t('enableAutoGenerateTip')}
              className="text-sm  whitespace-wrap w-1/4"
            >
              {t('enableAutoGenerate')}
            </FormLabel>
            <div className="text-muted-foreground w-3/4">
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </div>
          </div>
          <div className="flex pt-1">
            <div className="w-1/4"></div>
            <FormMessage />
          </div>
        </FormItem>
      )}
    />
  );
}

export function EnableTocToggle() {
  const { t } = useTranslate('knowledgeConfiguration');
  const form = useFormContext();

  return (
    <FormField
      control={form.control}
      name={'parser_config.toc_extraction'}
      render={({ field }) => (
        <FormItem className=" items-center space-y-0 ">
          <div className="flex items-center">
            <FormLabel
              tooltip={t('tocExtractionTip')}
              className="text-sm  whitespace-wrap w-1/4"
            >
              {t('tocExtraction')}
            </FormLabel>
            <div className="text-muted-foreground w-3/4">
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </div>
          </div>
          <div className="flex pt-1">
            <div className="w-1/4"></div>
            <FormMessage />
          </div>
        </FormItem>
      )}
    />
  );
}

export function ImageContextWindow() {
  const { t } = useTranslate('knowledgeConfiguration');
  const form = useFormContext();

  return (
    <FormField
      control={form.control}
      name="parser_config.image_table_context_window"
      render={({ field }) => (
        <FormItem>
          <FormControl>
            <SliderInputFormField
              {...field}
              label={t('imageTableContextWindow')}
              tooltip={t('imageTableContextWindowTip')}
              defaultValue={0}
              min={0}
              max={256}
            />
          </FormControl>
          <div className="flex pt-1">
            <div className="w-1/4"></div>
            <FormMessage />
          </div>
        </FormItem>
      )}
    />
  );
}

export function OverlappedPercent() {
  const { t } = useTranslation();

  return (
    <SliderInputFormField
      percentage={true}
      name="parser_config.overlapped_percent"
      label={t('knowledgeConfiguration.overlappedPercent')}
      max={0.3}
      step={0.01}
    ></SliderInputFormField>
  );
}

export function AutoMetadata({
  type = MetadataType.Setting,
  otherData,
}: {
  type?: MetadataType;
  otherData?: Record<string, any>;
}) {
  const { t } = useTranslation();

  // get metadata field
  const location = useLocation();
  const form = useFormContext();
  const datasetContext = useContext(DataSetContext);
  const {
    manageMetadataVisible,
    showManageMetadataModal,
    hideManageMetadataModal,
    tableData,
    config: metadataConfig,
  } = useManageMetadata();

  const handleClickOpenMetadata = useCallback(() => {
    const metadata = form.getValues('parser_config.metadata');
    const tableMetaData = util.metaDataSettingJSONToMetaDataTableData(metadata);
    showManageMetadataModal({
      metadata: tableMetaData,
      isCanAdd: true,
      type: type,
      record: otherData,
    });
  }, [form, otherData, showManageMetadataModal, type]);

  useEffect(() => {
    const locationState = location.state as
      | { openMetadata?: boolean }
      | undefined;
    if (locationState?.openMetadata && !datasetContext?.loading) {
      setTimeout(() => {
        handleClickOpenMetadata();
      }, 0);
      locationState.openMetadata = false;
      history.replace({ ...location }, locationState);
    }
  }, [location, handleClickOpenMetadata, datasetContext]);

  const autoMetadataField: FormFieldConfig = {
    name: 'parser_config.enable_metadata',
    label: t('knowledgeConfiguration.autoMetadata'),
    type: FormFieldType.Custom,
    horizontal: true,
    defaultValue: true,
    tooltip: t('knowledgeConfiguration.autoMetadataTip'),
    render: (fieldProps: ControllerRenderProps) => (
      <div className="flex items-center justify-between">
        <Button type="button" variant="ghost" onClick={handleClickOpenMetadata}>
          <div className="flex items-center gap-2">
            <Settings />
            {t('knowledgeConfiguration.settings')}
          </div>
        </Button>
        <Switch
          checked={fieldProps.value}
          onCheckedChange={fieldProps.onChange}
        />
      </div>
    ),
  };

  const handleSaveMetadata = (data?: IMetaDataReturnJSONSettings) => {
    form.setValue('parser_config.metadata', data || []);
  };
  return (
    <>
      <RenderField field={autoMetadataField} />
      {manageMetadataVisible && (
        <ManageMetadataModal
          title={
            metadataConfig.title || (
              <div className="flex flex-col gap-2">
                <div className="text-base font-normal">
                  {t('knowledgeDetails.metadata.metadataGenerationSettings')}
                </div>
                <div className="text-sm text-text-secondary">
                  {t('knowledgeDetails.metadata.changesAffectNewParses')}
                </div>
              </div>
            )
          }
          visible={manageMetadataVisible}
          hideModal={hideManageMetadataModal}
          // selectedRowKeys={selectedRowKeys}
          tableData={tableData}
          isCanAdd={metadataConfig.isCanAdd}
          isDeleteSingleValue={metadataConfig.isDeleteSingleValue}
          type={metadataConfig.type}
          otherData={metadataConfig.record}
          isShowDescription={true}
          isShowValueSwitch={true}
          isVerticalShowValue={false}
          success={(data?: IMetaDataReturnJSONSettings) => {
            handleSaveMetadata(data);
          }}
        />
      )}
    </>
  );
}

export const LLMSelect = ({
  isEdit,
  field,
  disabled = false,
}: {
  isEdit: boolean;
  field: FieldValues;
  name?: string;
  disabled?: boolean;
}) => {
  const { t } = useTranslate('knowledgeConfiguration');
  const modelOptions = useComposeLlmOptionsByModelTypes([
    LlmModelType.Chat,
    LlmModelType.Image2text,
  ]);
  return (
    <SelectWithSearch
      onChange={async (value) => {
        field.onChange(value);
      }}
      disabled={disabled && !isEdit}
      value={field.value}
      options={modelOptions as SelectWithSearchFlagOptionType[]}
      placeholder={t('embeddingModelPlaceholder')}
    />
  );
};

export function LLMModelItem({ line = 1, isEdit, label, name }: IProps) {
  const { t } = useTranslate('knowledgeConfiguration');
  const form = useFormContext();
  // const disabled = useHasParsedDocument(isEdit);
  return (
    <>
      <FormField
        control={form.control}
        name={name ?? 'llm_id'}
        render={({ field }) => (
          <FormItem className={cn(' items-center space-y-0 ')}>
            <div
              className={cn('flex', {
                ' items-center': line === 1,
                'flex-col gap-1': line === 2,
              })}
            >
              <FormLabel
                tooltip={t('globalIndexModelTip')}
                className={cn('text-sm  whitespace-wrap ', {
                  'w-1/4': line === 1,
                })}
              >
                {label ?? t('llmModel')}
              </FormLabel>
              <div
                className={cn('text-text-secondary', { 'w-3/4': line === 1 })}
              >
                <FormControl>
                  <LLMSelect
                    isEdit={!!isEdit}
                    field={field}
                    disabled={false}
                  ></LLMSelect>
                </FormControl>
              </div>
            </div>
            <div className="flex pt-1">
              <div className={line === 1 ? 'w-1/4' : ''}></div>
              <FormMessage />
            </div>
          </FormItem>
        )}
      />
    </>
  );
}
