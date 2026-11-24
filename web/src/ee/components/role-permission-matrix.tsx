import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import {
  Eye,
  EyeOff,
  Pencil,
  PencilOff,
  ScreenShare,
  ScreenShareOff,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';

const ACTION_ORDER = ['write', 'read', 'share', 'enable'];
const ACTION_ICONS: Record<
  string,
  { enabled: LucideIcon; disabled: LucideIcon }
> = {
  read: { enabled: Eye, disabled: EyeOff },
  write: { enabled: Pencil, disabled: PencilOff },
  share: { enabled: ScreenShare, disabled: ScreenShareOff },
};

export interface RolePermissionResource {
  resource_type: string;
  actions: readonly string[];
  level?: number;
}

export function RolePermissionMatrix({
  resources,
  permissions,
  getResourceLabel,
  resourceHeader,
  getActionLabel,
  onChange,
  disabled = false,
  renderResourceExtra,
  renderExpandedResource,
  actionOrder = ACTION_ORDER,
  iconActions = false,
  isActionDisabled,
}: {
  resources: RolePermissionResource[];
  permissions: Record<string, Record<string, boolean | undefined>>;
  getResourceLabel: (resource: string) => string;
  resourceHeader: string;
  getActionLabel: (action: string) => string;
  onChange: (resource: string, action: string, checked: boolean) => void;
  disabled?: boolean;
  renderResourceExtra?: (resource: string) => ReactNode;
  renderExpandedResource?: (resource: string) => ReactNode;
  actionOrder?: string[];
  iconActions?: boolean;
  isActionDisabled?: (resource: string, action: string) => boolean;
}) {
  const actions = actionOrder.filter((action) =>
    resources.some((resource) => resource.actions.includes(action)),
  );
  const columns = `minmax(180px, 1fr) repeat(${actions.length}, 88px)`;

  return (
    <div className="max-h-[60vh] overflow-auto rounded-lg border border-border-button">
      <div
        className="sticky top-0 z-10 grid border-b border-border-button bg-bg-title px-4 py-3 text-sm text-text-secondary"
        style={{ gridTemplateColumns: columns }}
      >
        <span>{resourceHeader}</span>
        {actions.map((action) => (
          <span key={action} className="text-center">
            {getActionLabel(action)}
          </span>
        ))}
      </div>
      {resources.map((resource) => (
        <div key={resource.resource_type}>
          <div
            className="grid items-center border-b border-border-button px-4 py-3"
            style={{ gridTemplateColumns: columns }}
          >
            <div
              className="flex min-w-0 items-center gap-2 pr-3"
              style={{ paddingLeft: (resource.level ?? 0) * 20 }}
            >
              <span className="min-w-0 truncate text-sm text-text-primary">
                {getResourceLabel(resource.resource_type)}
              </span>
              {renderResourceExtra?.(resource.resource_type)}
            </div>
            {actions.map((action) => (
              <div key={action} className="flex justify-center">
                {resource.actions.includes(action) ? (
                  iconActions && action !== 'enable' ? (
                    (() => {
                      const checked =
                        !!permissions[resource.resource_type]?.[action];
                      const Icon =
                        ACTION_ICONS[action]?.[
                          checked ? 'enabled' : 'disabled'
                        ];

                      if (!Icon) return null;

                      return (
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`${getResourceLabel(resource.resource_type)} ${getActionLabel(action)}`}
                          aria-pressed={checked}
                          title={getActionLabel(action)}
                          disabled={
                            disabled ||
                            !!isActionDisabled?.(resource.resource_type, action)
                          }
                          className={
                            checked
                              ? 'text-accent-primary hover:text-accent-primary'
                              : 'text-text-secondary'
                          }
                          onClick={() =>
                            onChange(resource.resource_type, action, !checked)
                          }
                        >
                          <Icon />
                        </Button>
                      );
                    })()
                  ) : (
                    <Switch
                      aria-label={`${getResourceLabel(resource.resource_type)} ${getActionLabel(action)}`}
                      checked={!!permissions[resource.resource_type]?.[action]}
                      disabled={
                        disabled ||
                        !!isActionDisabled?.(resource.resource_type, action)
                      }
                      onCheckedChange={(checked) =>
                        onChange(resource.resource_type, action, checked)
                      }
                    />
                  )
                ) : (
                  <span aria-hidden="true" className="text-text-disabled">
                    —
                  </span>
                )}
              </div>
            ))}
          </div>
          {renderExpandedResource?.(resource.resource_type)}
        </div>
      ))}
    </div>
  );
}
