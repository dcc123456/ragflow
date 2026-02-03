import { api_host } from './api';

export default {
  // next team
  listDepartment: (tenantId: string) =>
    `${api_host}/team/${tenantId}/departments`,
  createDepartment: `${api_host}/team/department/create`,
  updateDepartment: (tenantId: string) =>
    `${api_host}/team/${tenantId}/department/update`,
  deleteDepartment: (id: string) => `${api_host}/team/department/delete/${id}`,
  listDepartmentMember: (tenantId: string, id: string) =>
    `${api_host}/team/${tenantId}/department/members/${id}`,
  createDepartmentMember: (tenantId: string) =>
    `${api_host}/team/${tenantId}/department/member/create`,
  deleteDepartmentMember: (tenantId: string) =>
    `${api_host}/team/${tenantId}/department/member/delete`,
  moveDepartment: `${api_host}/team/department/move`,
  listGroup: (tenantId: string) => `${api_host}/team/${tenantId}/groups`,
  createGroup: `${api_host}/team/group/create`,
  updateGroup: (tenantId: string) =>
    `${api_host}/team/${tenantId}/group/update`,
  deleteGroup: (tenantId: string, id: string) =>
    `${api_host}/team/${tenantId}/group/delete/${id}`,
  listGroupMember: (tenantId: string, id: string) =>
    `${api_host}/team/${tenantId}/group/members/${id}`,
  createGroupMember: (tenantId: string) =>
    `${api_host}/team/${tenantId}/group/member/create`,
  deleteGroupMember: (tenantId: string) =>
    `${api_host}/team/${tenantId}/group/member/delete`,
  transferGroupOwner: (tenantId: string) =>
    `${api_host}/team/${tenantId}/group/owner`,

  // permission
  updatePermission: `${api_host}/permission/update`,
  listPermission: `${api_host}/permission/list`,
  updateDialogPermission: `${api_host}/permission/share_dialog`,

  // billing
  billin_checkout: `${api_host}/billing/checkout`,
  current_plan: `${api_host}/billing/current_plan`,
  cancel_scheduled_subscription_change: `${api_host}/billing/cancel-scheduled-subscription-change`,
  plan_list: `${api_host}/billing/plans`,
  plan_spend_overview: `${api_host}/billing/spend_metrics`,
  blling_base_overview: `${api_host}/billing/usage_based_overview`,
  plan_overview: `${api_host}/billing/plan_overview`,
  getUpComming: `${api_host}/billing/upcoming`,
  spendHistory: `${api_host}/billing/spend_overview`,
  usageBasedPlans: `${api_host}/billing/usage_based_plans`,

  // premise
  enableAdmin: `${api_host}/user/enable_admin`, // enable 为true且 is_admin 为false 隐藏model provide
  isAdmin: `${api_host}/user/is_admin`, // 非admin用户不给显示model providers页面
  setDefaultLlm: `${api_host}/llm/set_default_llm`, // 添加一个按钮：重置成默认,需要弹框确认。
};
