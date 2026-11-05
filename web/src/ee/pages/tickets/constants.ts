const TicketStateI18nKeys: Record<string, string> = {
  new: 'tickets.states.new',
  open: 'tickets.states.open',
  'pending close': 'tickets.states.pendingClose',
  'pending reminder': 'tickets.states.pendingReminder',
  closed: 'tickets.states.closed',
};

const TicketPriorityI18nKeys: Record<string, string> = {
  '1 low': 'tickets.priorities.low',
  '2 normal': 'tickets.priorities.normal',
  '3 high': 'tickets.priorities.high',
  low: 'tickets.priorities.low',
  normal: 'tickets.priorities.normal',
  high: 'tickets.priorities.high',
};

export const getTicketStateI18nKey = (state?: string) => {
  if (!state) return undefined;
  return TicketStateI18nKeys[state.toLowerCase()];
};

export const getTicketPriorityI18nKey = (priority?: string) => {
  if (!priority) return undefined;
  return TicketPriorityI18nKeys[priority.toLowerCase()];
};
