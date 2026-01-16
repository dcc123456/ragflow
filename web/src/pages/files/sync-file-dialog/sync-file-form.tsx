import { noop } from 'lodash';

import { zodResolver } from '@hookform/resolvers/zod';
import { useCallback, useMemo } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form';

import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';

import { IModalProps } from '@/interfaces/common';
import { useListDataSource } from '@/pages/user-setting/data-source/hooks';

import { Spin } from '@/components/ui/spin';
import { useFetchPureFileList } from '@/hooks/use-file-request';
import { IFile } from '@/interfaces/database/file-manager';
import {
  DataSourceKey,
  generateDataSourceInfo,
} from '@/pages/user-setting/data-source/constant';
import {
  listDataSourceFiles,
  type DataSourceFileItem,
} from '@/services/data-source-service';
import { useQuery } from '@tanstack/react-query';
import { LucideFolder } from 'lucide-react';
import { useGetFolderId } from '../hooks';
import { TreeView } from './tree-view';

type DataSourceFileItemWithPath = DataSourceFileItem & {
  idPath: string[];
  namePath: string[];
};

function flattenTreeData(
  data: DataSourceFileItem[] | DataSourceFileItem,
  idPath: string[] = [],
  namePath: string[] = [],
): DataSourceFileItemWithPath[] {
  const _data = Array.isArray(data) ? data : [data];

  return _data.flatMap((item) => {
    const thisItemIdPath = [...idPath, item.token];
    const thisItemNamePath = [...namePath, item.name];
    return [
      {
        ...item,
        idPath: thisItemIdPath,
        namePath: thisItemNamePath,
      },
      ...(Array.isArray(item.children) && item.children.length
        ? flattenTreeData(item.children, thisItemIdPath, thisItemNamePath)
        : []),
    ];
  });
}

function isLeafNode(item: DataSourceFileItem) {
  return item.type !== 'folder';
}

export function SyncFileForm({
  id: formId,
  onOk = noop,
}: IModalProps<any> & { id?: string }) {
  const { t } = useTranslation();
  const { categorizedList: _catList, isFetching: isFetchingDataSourceList } =
    useListDataSource();

  const currentFolderId = useGetFolderId();
  const { fetchList: fetchFileList } = useFetchPureFileList();

  const { data: folderList, isFetching: isFetchingFolderList } = useQuery({
    queryKey: ['fileManager/sync/fetchFileList', currentFolderId],
    queryFn: async () => {
      const { data } = await fetchFileList(currentFolderId);
      return data.files.filter(
        (item: IFile) => item.type === 'folder',
      ) as IFile[];
    },
    initialData: [],
  });

  const categorizedList = useMemo(
    // Currently only support Lark and Confluence
    () =>
      _catList.filter(
        (item) =>
          item.id === DataSourceKey.LARK ||
          item.id === DataSourceKey.CONFLUENCE,
      ),
    [_catList],
  );

  const dataSourceInfo = useMemo(() => generateDataSourceInfo(t), [t]);

  const form = useForm<z.infer<typeof SyncFileForm.schema>>({
    resolver: zodResolver(SyncFileForm.schema),
    defaultValues: {
      dataSource: '',
      targetFolder: '',
      syncFiles: [],
    },
  });

  const selectedDataSourceId = form.watch('dataSource', '');

  const {
    data: { data, flatData },
    isFetching: isFetchingDataSourceFiles,
  } = useQuery({
    queryKey: ['fileManager/sync/listDataSourceFiles', selectedDataSourceId],
    queryFn: async () => {
      const { data } = await listDataSourceFiles(selectedDataSourceId);
      return {
        data,
        flatData: flattenTreeData(data),
      };
    },
    retry: false,
    enabled: !!selectedDataSourceId,
    initialData: {
      data: [],
      flatData: [],
    },
  });

  const handleSubmit = useCallback(
    (data: z.infer<typeof SyncFileForm.schema>) => {
      const filteredData = flatData.filter((item) =>
        data.syncFiles.includes(item.token),
      );

      return onOk?.({
        dataSource: data.dataSource,
        targetFolder: data.targetFolder,
        syncFiles: filteredData,
      });
    },
    [onOk, flatData],
  );

  return (
    <Spin spinning={isFetchingFolderList || isFetchingDataSourceList}>
      <Form {...form}>
        <form
          id={formId}
          className="space-y-6"
          onSubmit={form.handleSubmit(handleSubmit)}
        >
          <FormField
            control={form.control}
            name="dataSource"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('fileManager.dataSource')}</FormLabel>
                <FormControl>
                  <Select
                    disabled={field.disabled || isFetchingDataSourceList}
                    value={field.value || ''}
                    onValueChange={field.onChange}
                  >
                    <SelectTrigger ref={field.ref}>
                      <SelectValue />
                    </SelectTrigger>

                    <SelectContent>
                      {categorizedList.length ? (
                        categorizedList.map((cat) => {
                          const dataSource =
                            dataSourceInfo[cat.id as DataSourceKey];

                          return (
                            <SelectGroup key={cat.id}>
                              <SelectLabel className="pl-2 flex items-center text-text-disabled">
                                <span className="size-[1em] [&_*]:!block [&_*]:!size-[1em] mr-1">
                                  {dataSource.icon}
                                </span>
                                <span>{dataSource.name}</span>
                              </SelectLabel>

                              {cat.list.map((item) => (
                                <SelectItem key={item.id} value={item.id}>
                                  {item.name}
                                </SelectItem>
                              ))}
                            </SelectGroup>
                          );
                        })
                      ) : (
                        <div className="p-4 text-center text-sm text-text-disabled">
                          {t('common.noData')}
                        </div>
                      )}
                    </SelectContent>
                  </Select>
                </FormControl>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="syncFiles"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('fileManager.syncFiles')}</FormLabel>

                <Spin spinning={isFetchingDataSourceFiles}>
                  <div className="h-64 border border-border-default rounded overflow-auto">
                    <FormControl>
                      <TreeView
                        ref={field.ref}
                        data={data}
                        idProp="token"
                        multiple
                        showSelectAll
                        value={field.value}
                        onSelectChange={field.onChange}
                        disabled={field.disabled}
                        // @ts-ignore
                        isLeafNode={isLeafNode}
                      />
                    </FormControl>
                  </div>
                </Spin>
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="targetFolder"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('fileManager.targetFolder')}</FormLabel>
                <FormControl>
                  <Select
                    disabled={field.disabled || isFetchingFolderList}
                    value={field.value || ''}
                    onValueChange={field.onChange}
                  >
                    <SelectTrigger ref={field.ref}>
                      <SelectValue />
                    </SelectTrigger>

                    <SelectContent>
                      <SelectGroup>
                        {folderList.map((item) => (
                          <SelectItem key={item.id} value={item.id}>
                            <div className="flex items-center">
                              <LucideFolder className="size-[1em] mr-1" />
                              <span>{item.name}</span>
                            </div>
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </FormControl>
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Spin>
  );
}

SyncFileForm.schema = z.object({
  dataSource: z.string().min(1, { message: '' }),
  targetFolder: z.string().min(1, { message: '' }),
  syncFiles: z.array(z.string()).min(1, { message: '' }),
});
