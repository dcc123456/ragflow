import {
  ITenantInviteLink,
  ITenantInviteLinkInfo,
  ITenantInviteLinkMembership,
} from '@/ee/interfaces/database/team';
import api from '@/ee/utils/private-api';
import request from '@/utils/next-request';

type Result<T> = {
  code: number;
  message?: string;
  data: T;
};

export const createTenantInviteLink = (tenantId: string) =>
  request.post<Result<ITenantInviteLink>>(api.tenantInviteLinks(tenantId));

export const getTenantInviteLinkInfo = (token: string) =>
  request.get<Result<ITenantInviteLinkInfo>>(api.tenantInviteLinkInfo(token));

export const getTenantInviteLinkMembership = (token: string) =>
  request.get<Result<ITenantInviteLinkMembership>>(
    api.tenantInviteLinkMembership(token),
  );

export const requestTenantInviteLink = (token: string) =>
  request.post<Result<boolean>>(api.requestTenantInviteLink(token));
