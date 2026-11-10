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
  tenantMemberRole: (userId: string) =>
    `${restAPIv1}/tenant/members/${userId}/role`,
  tenantMyPermissions: `${restAPIv1}/tenant/me/permissions`,

  // permission
  updatePermission: `${restAPIv1}/permission/update`,
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
};
