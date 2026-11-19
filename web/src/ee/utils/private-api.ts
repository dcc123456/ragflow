import { restAPIv1, webAPI } from '@/utils/api';

export default {
  // next team
  listDepartment: (tenantId: string) =>
    `${webAPI}/team/${tenantId}/departments`,
  createDepartment: `${webAPI}/team/department/create`,
  updateDepartment: (tenantId: string) =>
    `${webAPI}/team/${tenantId}/department/update`,
  deleteDepartment: (id: string) => `${webAPI}/team/department/delete/${id}`,
  listDepartmentMember: (tenantId: string, id: string) =>
    `${webAPI}/team/${tenantId}/department/members/${id}`,
  createDepartmentMember: (tenantId: string) =>
    `${webAPI}/team/${tenantId}/department/member/create`,
  deleteDepartmentMember: (tenantId: string) =>
    `${webAPI}/team/${tenantId}/department/member/delete`,
  moveDepartment: `${webAPI}/team/department/move`,
  listGroup: (tenantId: string) => `${webAPI}/team/${tenantId}/groups`,
  createGroup: `${webAPI}/team/group/create`,
  updateGroup: (tenantId: string) => `${webAPI}/team/${tenantId}/group/update`,
  deleteGroup: (tenantId: string, id: string) =>
    `${webAPI}/team/${tenantId}/group/delete/${id}`,
  listGroupMember: (tenantId: string, id: string) =>
    `${webAPI}/team/${tenantId}/group/members/${id}`,
  createGroupMember: (tenantId: string) =>
    `${webAPI}/team/${tenantId}/group/member/create`,
  deleteGroupMember: (tenantId: string) =>
    `${webAPI}/team/${tenantId}/group/member/delete`,
  transferGroupOwner: (tenantId: string) =>
    `${webAPI}/team/${tenantId}/group/owner`,

  // tenant groups
  tenantGroups: `${restAPIv1}/tenant/groups`,
  tenantGroup: (groupId: string) => `${restAPIv1}/tenant/groups/${groupId}`,
  tenantGroupParent: (groupId: string) =>
    `${restAPIv1}/tenant/groups/${groupId}/parent`,
  tenantGroupMembers: (groupId: string) =>
    `${restAPIv1}/tenant/groups/${groupId}/members`,
  tenantGroupMember: (groupId: string, userId: string) =>
    `${restAPIv1}/tenant/groups/${groupId}/members/${userId}`,
  tenantGroupMemberPermissions: (groupId: string, userId: string) =>
    `${restAPIv1}/tenant/groups/${groupId}/members/${userId}/permissions`,
  tenantGroupOwner: (groupId: string) =>
    `${restAPIv1}/tenant/groups/${groupId}/owner`,

  // tenant switching
  tenantSwitch: (tenantId: string) => `${restAPIv1}/tenants/${tenantId}/switch`,

  // tenant roles
  tenantRoles: `${restAPIv1}/tenant/roles`,
  tenantRole: (roleId: number) => `${restAPIv1}/tenant/roles/${roleId}`,
  tenantRolePermissions: (roleId: number) =>
    `${restAPIv1}/tenant/roles/${roleId}/permissions`,
  tenantRoleResources: `${restAPIv1}/tenant/role-resources`,
  tenantDefaultRole: `${restAPIv1}/tenant/default-role`,
  tenantManagementPermissions: `${restAPIv1}/tenant/management-permissions`,
  tenantInviteLinks: (tenantId: string) =>
    `${restAPIv1}/tenants/${encodeURIComponent(tenantId)}/invitation-links`,
  tenantInviteLinkInfo: (token: string) =>
    `${restAPIv1}/tenant-invitations/${encodeURIComponent(token)}`,
  tenantInviteLinkMembership: (token: string) =>
    `${restAPIv1}/tenant-invitations/${encodeURIComponent(token)}/status`,
  requestTenantInviteLink: (token: string) =>
    `${restAPIv1}/tenant-invitations/${encodeURIComponent(token)}/request`,
  approveTenantMember: (userId: string) =>
    `${restAPIv1}/tenant/members/${encodeURIComponent(userId)}/approve`,
  tenantMemberRole: (userId: string) =>
    `${restAPIv1}/tenant/members/${userId}/role`,
  tenantMyPermissions: `${restAPIv1}/tenant/me/permissions`,

  // permission
  updatePermission: `${restAPIv1}/permission/update`,
  transferResourceOwner: `${restAPIv1}/permission/transfer_owner`,
  listPermission: `${restAPIv1}/permission/list`,
  listPermissionByTarget: `${restAPIv1}/permission/list_by_target`,

  // billing
  billing_checkout: `${restAPIv1}/billing/subscription`,
  current_plan: `${restAPIv1}/billing/subscription`,
  createPortalSession: `${restAPIv1}/billing/portal-sessions`,
  plan_list: `${restAPIv1}/billing/plans`,
  plan_spend_overview: `${restAPIv1}/billing/spend-metrics`,
  billing_base_overview: `${restAPIv1}/billing/addons/overview`,
  plan_overview: `${restAPIv1}/billing/subscription/overview`,
  getUpcoming: `${restAPIv1}/billing/subscription/preview`,
  spendHistory: `${restAPIv1}/billing/spend-overview`,
  addonPlans: `${restAPIv1}/billing/addons`,
  storageCurrent: `${restAPIv1}/billing/storage`,
  storageSetTarget: `${restAPIv1}/billing/storage`,
  deepdocUsage: `${restAPIv1}/billing/usages/deepdoc`,
  pointsCheckout: `${restAPIv1}/billing/points/checkout`,
  pointsPrice: `${restAPIv1}/billing/points/price`,
  pointsBalance: `${restAPIv1}/billing/points/balance`,
  pointsOverview: `${restAPIv1}/billing/points/overview`,
  pointsLedger: `${restAPIv1}/billing/points/ledger`,
  pointsHolds: `${restAPIv1}/billing/points/holds`,
  billingStatus: `${restAPIv1}/billing/status`,
  session: (sessionId: string) => `${restAPIv1}/billing/checkouts/${sessionId}`,
  directPaymentStatus: (subscriptionId: string) =>
    `${restAPIv1}/billing/subscriptions/${subscriptionId}/payment-status`,
  billingSetupIntent: `${restAPIv1}/billing/setup-intents`,

  // premise
  setDefaultLlm: `${webAPI}/llm/set_default_llm`, // 添加一个按钮：重置成默认,需要弹框确认。

  // tickets
  ticketGroups: `${webAPI}/ticket/groups`,
  tickets: `${webAPI}/ticket/`,
  ticketDetail: (id: number) => `${webAPI}/ticket/${id}`,
  ticketArticles: (id: number) => `${webAPI}/ticket/${id}/articles`,
  ticketClose: (id: number) => `${webAPI}/ticket/${id}/close`,
  ticketAttachment: (
    ticketId: number,
    articleId: number,
    attachmentId: number,
  ) =>
    `${webAPI}/ticket/${ticketId}/articles/${articleId}/attachments/${attachmentId}`,

  // chunk version history
  chunkVersions: (datasetId: string, documentId: string, chunkId: string) =>
    `${restAPIv1}/datasets/${datasetId}/documents/${documentId}/chunks/${chunkId}/versions`,
  chunkVersion: (
    datasetId: string,
    documentId: string,
    chunkId: string,
    version: number,
  ) =>
    `${restAPIv1}/datasets/${datasetId}/documents/${documentId}/chunks/${chunkId}/versions/${version}`,
  chunkVersionDiff: (
    datasetId: string,
    documentId: string,
    chunkId: string,
    from: number,
    to?: number,
  ) => {
    const params = new URLSearchParams({ from: String(from) });
    if (to !== undefined) {
      params.set('to', String(to));
    }
    return `${restAPIv1}/datasets/${datasetId}/documents/${documentId}/chunks/${chunkId}/versions/diff?${params.toString()}`;
  },
  restoreChunkVersion: (
    datasetId: string,
    documentId: string,
    chunkId: string,
    version: number,
  ) =>
    `${restAPIv1}/datasets/${datasetId}/documents/${documentId}/chunks/${chunkId}/versions/${version}/restore`,
  // A version's picture stays in storage after it is replaced, but the live
  // chunk no longer references it, so it is read through the version that
  // recorded it rather than through the document image endpoint.
  chunkVersionImage: (
    datasetId: string,
    documentId: string,
    chunkId: string,
    version: number,
  ) =>
    `${restAPIv1}/datasets/${datasetId}/documents/${documentId}/chunks/${chunkId}/versions/${version}/image`,

  // dataset configuration history
  datasetVersions: (
    datasetId: string,
    options?: {
      before_version?: number;
      kind?: string;
      limit?: number;
    },
  ) => {
    const params = new URLSearchParams();
    if (options?.before_version !== undefined) {
      params.set('before_version', String(options.before_version));
    }
    if (options?.kind) {
      params.set('kind', options.kind);
    }
    if (options?.limit !== undefined) {
      params.set('limit', String(options.limit));
    }
    const query = params.toString();
    return `${restAPIv1}/datasets/${datasetId}/versions${query ? `?${query}` : ''}`;
  },
  datasetVersion: (datasetId: string, version: number) =>
    `${restAPIv1}/datasets/${datasetId}/versions/${version}`,
  restoreDatasetVersion: (datasetId: string, version: number) =>
    `${restAPIv1}/datasets/${datasetId}/versions/${version}/restore`,
};
