import { useTranslate } from '@/hooks/common-hooks';
import { useSetPaginationParams } from '@/hooks/route-hook';
import { Pagination } from '@/interfaces/common';
import { useCallback, useMemo, useState } from 'react';

type PaginationParams = { page: number; pageSize?: number; total?: number };

export const useGetPaginationWithRouter = () => {
  const { t } = useTranslate('common');
  const {
    setPaginationParams,
    page,
    size: pageSize,
  } = useSetPaginationParams();

  const onPageChange: Pagination['onChange'] = useCallback(
    (pageNumber: number, size?: number) => {
      if (size !== pageSize) {
        setPaginationParams(1, size);
      } else {
        setPaginationParams(pageNumber, size);
      }
    },
    [setPaginationParams, pageSize],
  );

  const setCurrentPagination = useCallback(
    (pagination: PaginationParams) => {
      if (pagination.pageSize !== pageSize) {
        pagination.page = 1; // Reset to first page if pageSize changes
      }
      setPaginationParams(pagination.page, pagination.pageSize);
    },
    [setPaginationParams, pageSize],
  );

  const pagination: Pagination = useMemo(() => {
    return {
      showQuickJumper: true,
      total: 0,
      showSizeChanger: true,
      current: page,
      pageSize: pageSize,
      pageSizeOptions: [1, 2, 10, 20, 50, 100],
      onChange: onPageChange,
      showTotal: (total: number) => `${t('total')} ${total}`,
    };
  }, [t, onPageChange, page, pageSize]);

  return {
    pagination,
    setPagination: setCurrentPagination,
  };
};

export const useGetPagination = (options?: { pageSize?: number }) => {
  const [pagination, setPagination] = useState({
    page: 1,
    pageSize: options?.pageSize ?? 10,
    total: 0,
  });
  const { t } = useTranslate('common');

  const onPageChange: Pagination['onChange'] = useCallback(
    (pageNumber: number, pageSize?: number) => {
      setPagination((pre) => ({
        ...pre,
        page: pageNumber,
        pageSize: pageSize ?? pre.pageSize,
      }));
    },
    [],
  );

  const changePagination = useCallback((next: PaginationParams) => {
    setPagination((prev) => ({ ...prev, ...next }));
  }, []);

  const currentPagination: Pagination = useMemo(() => {
    return {
      showQuickJumper: true,
      total: pagination.total ?? 0,
      showSizeChanger: true,
      current: pagination.page,
      pageSize: pagination.pageSize,
      pageSizeOptions: [1, 2, 10, 20, 50, 100],
      onChange: onPageChange,
      showTotal: (total: number) => `${t('total')} ${total}`,
    };
  }, [t, onPageChange, pagination]);

  return {
    pagination: currentPagination,
    changePagination,
    setPagination,
  };
};
