import { zodResolver } from '@hookform/resolvers/zod';
import z from 'zod';

import { useId } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router';

import {
  LucideDot,
  LucideEdit3,
  LucideSettings,
  LucideTrash2,
  LucideUserPlus,
} from 'lucide-react';

import Spotlight from '@/components/spotlight';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Switch } from '@/components/ui/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

import Empty from '@/components/empty/empty';
import { useSetModalState } from '@/hooks/common-hooks';
import { Routes } from '@/routes';

import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form';
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';
import useCreateRoleForm from '../forms/role-form';
import { useRoleDefaultModels } from '../hooks/useLlm';
import {
  useCreateRole,
  useMutateRole,
  useRoleList,
  useRoleResourceTypes,
} from '../hooks/useRole';
import { RESOURCE_PERMISSIONS } from '../utils';

const editFormSchema = z.object({
  description: z.string().optional(),
});

function RoleCard({ role }: { role: AdminService.ListRoleItemWithPermission }) {
  const { t } = useTranslation();

  const editModalState = useSetModalState();
  const deleteModalState = useSetModalState();
  const { setupStatus } = useRoleDefaultModels(role.role_name);

  const editFormId = useId();

  const { resourceTypes, isFetching: isFetchingResourceTypes } =
    useRoleResourceTypes();

  const {
    updateDescription,
    updatePermission,
    delete: deleteRole,

    isUpdatingDescription,
    isUpdatingPermission,
    isDeleting,
  } = useMutateRole(role.role_name);

  const editForm = useForm<z.infer<typeof editFormSchema>>({
    resolver: zodResolver(editFormSchema),
    defaultValues: {
      description: role.description || '',
    },
  });

  return (
    <Card
      key={role.id}
      className="group/role border-0.5 border-border-default bg-transparent dark:hover:bg-bg-card transition-color duration-150"
    >
      <CardHeader className="space-y-0 flex flex-row gap-4 items-start border-b-0.5 border-border-button">
        <div className="space-y-1.5 w-0 flex-1">
          <CardTitle className="font-normal text-xl">
            {role.role_name}
          </CardTitle>

          <div className="text-sm text-text-secondary break-words">
            {role.description || (
              <i className="text-muted-foreground">
                {t('admin.noDescription')}
              </i>
            )}

            <Button
              variant="transparent"
              className="
                ml-2 p-0 border-0 size-[1em] align-middle opacity-0
                group-hover/role:opacity-100 group-focus-within/role:opacity-100
              "
              onClick={editModalState.showModal}
            >
              <LucideEdit3 className="!size-[1em]" />
            </Button>
          </div>
        </div>

        <Button
          variant="danger"
          size="icon"
          className="border-0 ml-auto opacity-0 group-hover/role:opacity-100 group-focus-within/role:opacity-100"
          disabled={isDeleting}
          onClick={deleteModalState.showModal}
        >
          <LucideTrash2 />
        </Button>
      </CardHeader>

      <CardContent className="p-6">
        {!isFetchingResourceTypes && (
          <Tabs
            className="h-full flex flex-col"
            defaultValue={resourceTypes?.[0]}
          >
            <TabsList className="p-0 mb-2 gap-4 bg-transparent justify-start">
              {resourceTypes?.map((resourceName) => (
                <TabsTrigger
                  key={resourceName}
                  value={resourceName}
                  className="text-text-secondary border-0.5 border-border-button data-[state=active]:bg-bg-card"
                >
                  {t(`admin.resourceType.${resourceName}`)}
                </TabsTrigger>
              ))}
            </TabsList>

            {resourceTypes?.map((resourceName) => {
              const permission = role.permissions[resourceName];

              if (resourceName === 'model_provider') {
                return null;
              }

              return (
                <TabsContent key={resourceName} value={resourceName}>
                  <Card className="border-0 bg-bg-card !shadow-none">
                    <CardContent className="py-0 h-16 flex gap-8">
                      {RESOURCE_PERMISSIONS[resourceName]?.map(
                        (permissionType) => (
                          <Label
                            key={permissionType}
                            className="flex items-center gap-2"
                          >
                            {t(`admin.${permissionType}`)}

                            <Switch
                              disabled={isUpdatingPermission}
                              checked={!!permission?.[permissionType]}
                              onCheckedChange={(value) =>
                                updatePermission({
                                  resourceName,
                                  permissionType,
                                  value,
                                })
                              }
                            />
                          </Label>
                        ),
                      )}
                    </CardContent>
                  </Card>
                </TabsContent>
              );
            })}

            <TabsContent value="model_provider">
              <Card className="border-0 bg-bg-card !shadow-none">
                <CardContent className="py-0 h-16 flex items-center gap-8">
                  <Label className="flex items-center gap-2">
                    {t('admin.permissionType.accessControl')}

                    <Tooltip>
                      <TooltipTrigger>
                        <LucideDot
                          className={cn(
                            'stroke-[4] stroke-current',
                            setupStatus === 'not_set' && 'text-state-error',
                            setupStatus === 'partial' && 'text-state-warning',
                            setupStatus === 'complete' && 'text-state-success',
                          )}
                        />
                      </TooltipTrigger>

                      <TooltipContent>
                        {t(
                          `admin.roleModelProviders.setupStatus.${setupStatus}`,
                        )}
                      </TooltipContent>
                    </Tooltip>
                  </Label>

                  <div className="ml-auto flex items-center gap-4">
                    <Link
                      to={Routes.AdminRoleConfigModelProviders.replace(
                        ':roleName',
                        role.role_name,
                      )}
                    >
                      <Button
                        variant="transparent"
                        className="size-8 bg-transparent border-0"
                      >
                        <LucideSettings />
                      </Button>
                    </Link>

                    <Switch
                      disabled={isUpdatingPermission}
                      checked={!!role.permissions.model_provider?.enable}
                      onCheckedChange={(value) => {
                        updatePermission({
                          resourceName: 'model_provider',
                          permissionType: 'enable',
                          value,
                        });
                      }}
                    />
                  </div>
                </CardContent>
              </Card>
            </TabsContent>
          </Tabs>
        )}
      </CardContent>

      {/* Modify role description modal */}
      <Dialog
        open={editModalState.visible}
        onOpenChange={editModalState.setVisible}
      >
        <DialogContent aria-describedby={undefined}>
          <DialogHeader>
            <DialogTitle>{t('admin.editRoleDescription')}</DialogTitle>
          </DialogHeader>

          <section className="px-6">
            <Form {...editForm}>
              <form
                id={editFormId}
                onSubmit={editForm.handleSubmit((data) => {
                  updateDescription(data.description);
                  editModalState.hideModal();
                })}
              >
                <FormField
                  control={editForm.control}
                  name="description"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel className="text-sm font-medium">
                        {t('admin.description')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          className="mt-2 px-3 h-10 bg-bg-input border-border-button"
                          {...field}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
              </form>
            </Form>
          </section>

          <DialogFooter className="gap-4 px-6 py-4">
            <Button
              className="px-4 h-10 dark:border-border-button"
              variant="outline"
              onClick={() => editModalState.hideModal()}
              disabled={isUpdatingDescription}
            >
              {t('admin.cancel')}
            </Button>

            <Button
              type="submit"
              form={editFormId}
              className="px-4 h-10"
              loading={isUpdatingDescription}
            >
              {t('admin.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete role modal */}
      <Dialog
        open={deleteModalState.visible}
        onOpenChange={deleteModalState.setVisible}
      >
        <DialogContent aria-describedby={undefined}>
          <DialogHeader>
            <DialogTitle>{t('admin.deleteRole')}</DialogTitle>
          </DialogHeader>

          <section className="px-6">
            <DialogDescription className="text-text-primary">
              {t('admin.deleteRoleConfirmation')}
            </DialogDescription>

            <div className="rounded-lg mt-6 p-4 border-0.5 border-border-button">
              {role.role_name}
            </div>
          </section>

          <DialogFooter className="gap-4 px-6 py-4">
            <Button
              className="px-4 h-10 dark:border-border-button"
              variant="outline"
              onClick={() => deleteModalState.hideModal()}
              disabled={isDeleting}
            >
              {t('admin.cancel')}
            </Button>

            <Button
              className="px-4 h-10"
              variant="destructive"
              onClick={() => deleteRole()}
              loading={isDeleting}
            >
              {t('admin.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

function AdminRoles() {
  const { t } = useTranslation();

  const createRoleForm = useCreateRoleForm();
  const createModalState = useSetModalState();

  const { roleList } = useRoleList();

  const { createRole: createRoleMutation, isPending: isCreatingRole } =
    useCreateRole();

  return (
    <>
      <Card className="!shadow-none relative w-full h-full border-0.5 border-border-button bg-transparent rounded-xl">
        <Spotlight />

        <ScrollArea className="size-full">
          <CardHeader className="space-y-0 flex flex-row justify-between items-center">
            <CardTitle>{t('admin.roles')}</CardTitle>

            <Button
              className="h-10 px-4"
              onClick={() => createModalState.showModal()}
            >
              <LucideUserPlus />
              {t('admin.newRole')}
            </Button>
          </CardHeader>

          <CardContent className="space-y-6">
            {roleList?.length ? (
              roleList.map((role) => <RoleCard key={role.id} role={role} />)
            ) : (
              <Empty className="py-24" />
            )}
          </CardContent>
        </ScrollArea>
      </Card>

      {/* Add role modal */}
      <Dialog
        open={createModalState.visible}
        onOpenChange={createModalState.setVisible}
      >
        <DialogContent
          aria-describedby={undefined}
          className="w-auto max-w-full"
          onAnimationEnd={() => {
            if (!createModalState.visible) {
              createRoleForm.form.reset();
            }
          }}
        >
          <DialogHeader>
            <DialogTitle>{t('admin.addNewRole')}</DialogTitle>
          </DialogHeader>

          <section className="px-6">
            <createRoleForm.FormComponent
              onSubmit={async (data) => {
                await createRoleMutation(data);
                createModalState.hideModal();
                createRoleForm.form.reset();
              }}
            />
          </section>

          <DialogFooter className="gap-4 px-6 py-4">
            <Button
              className="px-4 h-10 dark:border-border-button"
              variant="outline"
              onClick={() => createModalState.hideModal()}
              disabled={isCreatingRole}
            >
              {t('admin.cancel')}
            </Button>

            <Button
              type="submit"
              form={createRoleForm.id}
              className="px-4 h-10"
              variant="default"
              loading={isCreatingRole}
            >
              {t('admin.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

export default AdminRoles;
