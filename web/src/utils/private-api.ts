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
  billing_checkout: `${webAPI}/billing/checkout`,
  current_plan: `${webAPI}/billing/current_plan`,
  createPortalSession: `${webAPI}/billing/create-portal-session`,
  cancel_scheduled_subscription_change: `${webAPI}/billing/cancel-scheduled-subscription-change`,
  plan_list: `${webAPI}/billing/plans`,
  plan_spend_overview: `${webAPI}/billing/spend_metrics`,
  billing_base_overview: `${webAPI}/billing/addon_overview`,
  plan_overview: `${webAPI}/billing/plan_overview`,
  getUpcoming: `${webAPI}/billing/upcoming`,
  spendHistory: `${webAPI}/billing/spend_overview`,
  addonPlans: `${webAPI}/billing/addon_plans`,
  storageCurrent: `${webAPI}/billing/storage/current`,
  storageSetTarget: `${webAPI}/billing/storage/set-target`,
  deepdocUsage: `${webAPI}/billing/deepdoc/usage`,
  pointsCheckout: `${webAPI}/billing/points/checkout`,
  pointsPrice: `${webAPI}/billing/points/price`,
  pointsBalance: `${webAPI}/billing/points/balance`,
  pointsOverview: `${webAPI}/billing/points/overview`,
  pointsLedger: `${webAPI}/billing/points/ledger`,
  pointsHolds: `${webAPI}/billing/points/holds`,
  session: (sessionId: string) => `${webAPI}/billing/session/${sessionId}`,

  // premise
  enableAdmin: `${restAPIv1}/enable_admin`, // enable 为true且 is_admin 为false 隐藏model provide
  isAdmin: `${restAPIv1}/is_admin`, // 非admin用户不给显示model providers页面
  setDefaultLlm: `${webAPI}/llm/set_default_llm`, // 添加一个按钮：重置成默认,需要弹框确认。

  // heart beat
  heartBeat: `${restAPIv1}/heartbeat`,
};
