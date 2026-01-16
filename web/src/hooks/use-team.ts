import {
  IDepartment,
  IGroup,
  IMember,
  IPermission,
} from '@/interfaces/database/team';
import {
  ICreateDepartmentRequestBody,
  ICreateGroupRequestBody,
  IDeleteDepartmentMemberRequestBody,
  IFetchDepartmentRequestParams,
  IMoveDepartmentRequestBody,
  ITransferGroupOwnerRequestBody,
  IUpdateDepartmentRequestBody,
  IUpdateDialogPermission,
  IUpdateGroupRequestBody,
  IUpdatePermission,
} from '@/interfaces/request/team';
import teamService, {
  deleteDepartment,
  deleteDepartmentMember,
  deleteGroup,
  listDepartment,
  listDepartmentMember,
  listGroup,
  listGroupMember,
  transferGroupOwner,
  updateDepartment,
  updateGroup,
} from '@/services/team-service';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useDebounceFn } from 'ahooks';
import { message } from 'antd';
import { isEmpty } from 'lodash';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSetPaginationParams } from './route-hook';

export const enum TeamApiAction {
  ListDepartment = 'listDepartment',
  CreateDepartment = 'createDepartment',
  UpdateDepartment = 'updateDepartment',
  DeleteDepartment = 'deleteDepartment',
  DeleteDepartmentMember = 'deleteDepartmentMember',
  MoveDepartment = 'moveDepartment',
  CreateDepartmentMember = 'createDepartmentMember',
  ListGroup = 'listGroup',
  CreateGroup = 'createGroup',
  UpdateGroup = 'updateGroup',
  DeleteGroup = 'deleteGroup',
  CreateGroupMember = 'createGroupMember',
  ListDepartmentMember = 'listDepartmentMember',
  ListGroupMember = 'listGroupMember',
  TransferGroupOwner = 'transferGroupOwner',
  UpdatePermission = 'updatePermission',
  ListPermission = 'listPermission',
  UpdateDialogPermission = 'updateDialogPermission',
  ConfirmDeletePermission = 'confirmDeletePermission',
}

export const useFetchDepartmentList = (tenantId: string) => {
  const [fetchDepartmentListParams, setFetchDepartmentListParams] =
    useState<IFetchDepartmentRequestParams>({
      parentId: '',
    } as IFetchDepartmentRequestParams);

  const { data, isFetching: loading } = useQuery<IDepartment[]>({
    queryKey: [
      TeamApiAction.ListDepartment,
      fetchDepartmentListParams,
      tenantId,
    ],
    initialData: [],
    gcTime: 0,
    enabled:
      typeof fetchDepartmentListParams.parentId === 'string' && !!tenantId,
    queryFn: async () => {
      const { data } = await listDepartment(
        tenantId,
        fetchDepartmentListParams,
      );

      return data?.data ?? [];
    },
  });

  return { data, loading, setFetchDepartmentListParams };
};

export const useCreateDepartment = () => {
  const { setPaginationParams } = useSetPaginationParams();
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TeamApiAction.CreateDepartment],
    mutationFn: async (params: ICreateDepartmentRequestBody) => {
      const { data } = await teamService.createDepartment(params);
      if (data.code === 0) {
        message.success(t('message.created'));
        setPaginationParams(1);
        queryClient.invalidateQueries({
          queryKey: [TeamApiAction.ListDepartment],
        });
      }
      return data.code;
    },
  });

  return { data, loading, createDepartment: mutateAsync };
};

export const useFetchDepartmentMemberList = () => {
  const [id, setId] = useState<string>('');
  const [teamId, setTeamId] = useState<string>('');

  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IMember[]>({
    queryKey: [TeamApiAction.ListDepartmentMember, id, teamId],
    initialData: [],
    gcTime: 0,
    enabled: !!id,
    queryFn: async () => {
      const { data } = await listDepartmentMember(teamId, id);

      return data?.data ?? [];
    },
  });

  return { data, loading, setId, refetch, setTeamId, departmentId: id };
};

export const useUpdateDepartment = (tenantId: string) => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TeamApiAction.UpdateDepartment],
    mutationFn: async (params: IUpdateDepartmentRequestBody) => {
      const departmentId = params.department_id;
      const { data } = await updateDepartment(tenantId, params);
      if (data.code === 0) {
        message.success(t('message.modified'));

        queryClient.invalidateQueries({
          queryKey: [TeamApiAction.ListDepartment],
        });
        queryClient.invalidateQueries({
          queryKey: [TeamApiAction.ListDepartmentMember, departmentId],
        });
      }
      return data.code;
    },
  });

  return { data, loading, updateDepartment: mutateAsync };
};

export const useDeleteDepartment = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TeamApiAction.DeleteDepartment],
    mutationFn: async (id: string) => {
      const { data } = await deleteDepartment(id);
      if (data.code === 0) {
        message.success(t('message.deleted'));
        queryClient.invalidateQueries({
          queryKey: [TeamApiAction.ListDepartment],
        });
      }
      return data.code;
    },
  });

  return { data, loading, deleteDepartment: mutateAsync };
};

export const useMoveDepartment = () => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TeamApiAction.MoveDepartment],
    mutationFn: async (params: IMoveDepartmentRequestBody) => {
      const { data } = await teamService.moveDepartment(params);
      if (data.code === 0) {
        message.success(t('message.operated'));
        queryClient.invalidateQueries({
          queryKey: [TeamApiAction.ListDepartment],
        });
      }
      return data.code;
    },
  });

  return { data, loading, moveDepartment: mutateAsync };
};

export const useDeleteDepartmentMember = (tenantId: string) => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TeamApiAction.DeleteDepartmentMember],
    mutationFn: async (params: IDeleteDepartmentMemberRequestBody) => {
      const { data } = await deleteDepartmentMember(tenantId, params);
      if (data.code === 0) {
        message.success(t('message.deleted'));
        queryClient.invalidateQueries({
          queryKey: [TeamApiAction.ListDepartmentMember, params.department_id],
        });
      }
      return data.code;
    },
  });

  return { data, loading, deleteDepartmentMember: mutateAsync };
};

export const useFetchGroupList = (tenantId: string) => {
  const { data, isFetching: loading } = useQuery<IGroup[]>({
    queryKey: [TeamApiAction.ListGroup, tenantId],
    initialData: [],
    gcTime: 0,
    enabled: !!tenantId,
    queryFn: async () => {
      const { data } = await listGroup(tenantId);

      return data?.data ?? [];
    },
  });

  return { data, loading };
};

export const useCreateGroup = () => {
  const { setPaginationParams } = useSetPaginationParams();
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TeamApiAction.CreateGroup],
    mutationFn: async (params: ICreateGroupRequestBody) => {
      const { data } = await teamService.createGroup(params);
      if (data.code === 0) {
        message.success(t('message.created'));
        setPaginationParams(1);
        queryClient.invalidateQueries({ queryKey: [TeamApiAction.ListGroup] });
      }
      return data.code;
    },
  });

  return { data, loading, createGroup: mutateAsync };
};

export const useUpdateGroup = (tenantId: string) => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TeamApiAction.UpdateGroup],
    mutationFn: async (params: IUpdateGroupRequestBody) => {
      const { data } = await updateGroup(tenantId, params);
      if (data.code === 0) {
        message.success(t('message.modified'));
        queryClient.invalidateQueries({ queryKey: [TeamApiAction.ListGroup] });
      }
      return data.code;
    },
  });

  return { data, loading, updateGroup: mutateAsync };
};

export const useDeleteGroup = (tenantId: string) => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TeamApiAction.DeleteGroup],
    mutationFn: async (id: string) => {
      const { data } = await deleteGroup(tenantId, id);
      if (data.code === 0) {
        message.success(t('message.deleted'));
        queryClient.invalidateQueries({ queryKey: [TeamApiAction.ListGroup] });
      }
      return data.code;
    },
  });

  return { data, loading, deleteGroup: mutateAsync };
};

export const useFetchGroupMemberList = (tenantId: string) => {
  const [id, setGroupId] = useState<string>('');

  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IMember[]>({
    queryKey: [TeamApiAction.ListGroupMember, id],
    initialData: [],
    gcTime: 0,
    enabled: !!id,
    queryFn: async () => {
      const { data } = await listGroupMember(tenantId, id);

      return data?.data ?? [];
    },
  });

  return { data, loading, setGroupId, refetch };
};

export const useTransferGroupOwner = (tenantId: string) => {
  const queryClient = useQueryClient();
  const { t } = useTranslation();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TeamApiAction.TransferGroupOwner],
    mutationFn: async (params: ITransferGroupOwnerRequestBody) => {
      const { data } = await transferGroupOwner(tenantId, params);
      if (data.code === 0) {
        message.success(t('message.operated'));
        queryClient.invalidateQueries({ queryKey: [TeamApiAction.ListGroup] });
      }
      return data.code;
    },
  });

  return { data, loading, transferGroupOwner: mutateAsync };
};

export const useUpdatePermission = () => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const { run } = useDebounceFn(
    () => {
      message.success(t('message.operated'));
      queryClient.invalidateQueries({
        queryKey: [TeamApiAction.ListPermission],
      });
    },
    {
      wait: 500,
    },
  );

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TeamApiAction.UpdatePermission],
    mutationFn: async (params: IUpdatePermission) => {
      const { data } = await teamService.updatePermission(params);

      if (data.code === 0) {
        run();
      }
      return data.code;
    },
  });

  return { data, loading, updatePermission: mutateAsync };
};

export const useFetchPermissionList = (
  tenantId: string,
  resourceIds: string[],
  resourceType: string = 'kb',
) => {
  const {
    data,
    isFetching: loading,
    refetch,
  } = useQuery<IPermission[]>({
    queryKey: [TeamApiAction.ListPermission, resourceIds, tenantId],
    initialData: [],
    gcTime: 0,
    enabled: !isEmpty(resourceIds) && !!tenantId,
    queryFn: async () => {
      const { data } = await teamService.listPermission({
        resource_ids: resourceIds,
        resource_type: resourceType,
        tenant_id: tenantId,
      });

      return data?.data ?? [];
    },
  });

  return { data, loading, refetch };
};

export const useUpdateDialogPermission = () => {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const {
    data,
    isPending: loading,
    mutateAsync,
  } = useMutation({
    mutationKey: [TeamApiAction.UpdateDialogPermission],
    mutationFn: async (params: IUpdateDialogPermission) => {
      const { data } = await teamService.updateDialogPermission(params);

      if (data.code === 0) {
        if (isEmpty(data?.data?.failed)) {
          message.success(t('message.operated'));
        }
        queryClient.invalidateQueries({
          queryKey: [TeamApiAction.ListPermission],
        });
      }
      return data;
    },
  });

  return { data, loading, updateDialogPermission: mutateAsync };
};
