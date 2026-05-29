import { restAPIv1, webAPI } from './api';

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

  // permission
  updatePermission: `${webAPI}/permission/update`,
  listPermission: `${webAPI}/permission/list`,
  listPermissionByTarget: `${webAPI}/permission/list_by_target`,
  updateDialogPermission: `${webAPI}/permission/share_dialog`,

  // billing
  billing_checkout: `${webAPI}/billing/subscription`,
  current_plan: `${webAPI}/billing/subscription`,
  createPortalSession: `${webAPI}/billing/portal-sessions`,
  plan_list: `${webAPI}/billing/plans`,
  plan_spend_overview: `${webAPI}/billing/spend_metrics`,
  billing_base_overview: `${webAPI}/billing/addons/overview`,
  plan_overview: `${webAPI}/billing/subscription/overview`,
  getUpcoming: `${webAPI}/billing/subscription/preview`,
  spendHistory: `${webAPI}/billing/spend_overview`,
  addonPlans: `${webAPI}/billing/addons`,
  storageCurrent: `${webAPI}/billing/storage`,
  storageSetTarget: `${webAPI}/billing/storage`,
  deepdocUsage: `${webAPI}/billing/usages/deepdoc`,
  pointsCheckout: `${webAPI}/billing/points/checkout`,
  pointsPrice: `${webAPI}/billing/points/price`,
  pointsBalance: `${webAPI}/billing/points/balance`,
  pointsOverview: `${webAPI}/billing/points/overview`,
  pointsLedger: `${webAPI}/billing/points/ledger`,
  pointsHolds: `${webAPI}/billing/points/holds`,
  billingStatus: `${webAPI}/billing/status`,
  session: (sessionId: string) => `${webAPI}/billing/checkouts/${sessionId}`,
  billingSetupIntent: `${webAPI}/billing/setup-intents`,

  // premise
  enableAdmin: `${restAPIv1}/enable_admin`, // enable 为true且 is_admin 为false 隐藏model provide
  isAdmin: `${restAPIv1}/is_admin`, // 非admin用户不给显示model providers页面
  setDefaultLlm: `${webAPI}/llm/set_default_llm`, // 添加一个按钮：重置成默认,需要弹框确认。

  // heart beat
  heartBeat: `${restAPIv1}/heartbeat`,

  // tickets
  ticketGroups: `${webAPI}/ticket/groups`,
  tickets: `${webAPI}/ticket`,
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
