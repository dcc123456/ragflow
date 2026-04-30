import { ExternalApi, api_host, webAPI } from './api';

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
  billing_checkout: `${api_host}/billing/checkout`,
  current_plan: `${api_host}/billing/current_plan`,
  createPortalSession: `${api_host}/billing/create-portal-session`,
  cancel_scheduled_subscription_change: `${api_host}/billing/cancel-scheduled-subscription-change`,
  plan_list: `${api_host}/billing/plans`,
  plan_spend_overview: `${api_host}/billing/spend_metrics`,
  billing_base_overview: `${api_host}/billing/addon_overview`,
  plan_overview: `${api_host}/billing/plan_overview`,
  getUpcoming: `${api_host}/billing/upcoming`,
  spendHistory: `${api_host}/billing/spend_overview`,
  addonPlans: `${api_host}/billing/addon_plans`,
  storageCurrent: `${api_host}/billing/storage/current`,
  storageSetTarget: `${api_host}/billing/storage/set-target`,
  storageAbandonPending: `${api_host}/billing/storage/abandon-pending`,
  deepdocUsage: `${api_host}/billing/deepdoc/usage`,
  pointsCheckout: `${api_host}/billing/points/checkout`,
  pointsPrice: `${api_host}/billing/points/price`,
  pointsBalance: `${api_host}/billing/points/balance`,
  pointsOverview: `${api_host}/billing/points/overview`,
  pointsLedger: `${api_host}/billing/points/ledger`,
  pointsHolds: `${api_host}/billing/points/holds`,
  session: (sessionId: string) => `${api_host}/billing/session/${sessionId}`,

  // premise
  enableAdmin: `${api_host}/user/enable_admin`, // enable 为true且 is_admin 为false 隐藏model provide
  isAdmin: `${api_host}/user/is_admin`, // 非admin用户不给显示model providers页面
  setDefaultLlm: `${api_host}/llm/set_default_llm`, // 添加一个按钮：重置成默认,需要弹框确认。

  // heart beat
  heartBeat: `${ExternalApi}${api_host}/heartbeat`,
};
