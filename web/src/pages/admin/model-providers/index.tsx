/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

import Spotlight from '@/components/spotlight';
import message from '@/components/ui/message';
import { useTranslate } from '@/hooks/common-hooks';
import {
  LlmKeys,
  useAddProviderInstance,
  useFetchAddedProviders,
  useFetchAvailableProviders,
  useFetchProviderInstances,
  useUpdateProviderInstance,
} from '@/hooks/use-llm-request';
import { IProviderInstance } from '@/interfaces/database/llm';
import { ProviderInstanceCardRef } from '@/pages/user-setting/setting-model/instance-card/interface';
import { ProviderInstanceCard } from '@/pages/user-setting/setting-model/instance-card/provider-instance-card';
import { ProviderHeaderBar } from '@/pages/user-setting/setting-model/layout/provider-header-bar';
import { Sidebar } from '@/pages/user-setting/setting-model/layout/sidebar';
// Registers the axios interceptor that redirects the shared model-provider
// hooks onto the admin gateway. Import for side effects; must stay above the
// components whose queries it rewrites.
import '@/services/admin-llm-scope';
import { useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { FC, useCallback, useEffect, useMemo, useRef, useState } from 'react';

/**
 * Admin-side model-provider management.
 *
 * The UI is the tenant settings page's, reused as-is: the same `Sidebar`,
 * `ProviderHeaderBar` and `ProviderInstanceCard` components and the same
 * batch-save flow. Two things differ, and both are handled *without* editing
 * the open-source components, so future syncs from upstream stay clean:
 *
 *  1. No "Default models" entry. The admin gateway manages providers across
 *     every tenant and has no per-tenant default-model view, so
 *     `SystemSetting` is never rendered here and `selection` is always a
 *     provider name. The upstream entry button is hidden by the scoped
 *     `[&_[data-testid=sidebar-default-models]]:hidden` class below, and
 *     `handleSelect` drops the `'default'` sentinel, so the button is both
 *     invisible and inert if upstream ever changes its markup.
 *  2. Requests go to the admin gateway. That switch is invisible here:
 *     because this page renders under `/admin`, the interceptor registered
 *     by `services/admin-llm-scope` rewrites the `/api/v1/providers` and
 *     `/api/v1/models` URLs those shared components use onto
 *     `/api/v1/admin/...`.
 *
 * Special-case providers (Bedrock, SoMark, custom-model providers) are
 * handled inside `ProviderInstanceCard` exactly as on the tenant page.
 */
const AdminModelProviders: FC = () => {
  const { t: tSetting } = useTranslate('setting');
  // Always a provider name — never the 'default' sentinel the tenant page
  // uses for its system-settings view.
  const [selection, setSelection] = useState<string>('');
  // Stack of draft-instance identifiers, rendered as `ProviderInstanceCard`
  // entries below the persisted instances. Each draft can be cancelled
  // independently; saving is driven by the top Save button.
  const [draftIds, setDraftIds] = useState<string[]>([]);

  const [saving, setSaving] = useState(false);

  // Tracks the instance name that was just persisted by the top Save
  // button. The corresponding saved card mounts expanded so the user
  // can immediately see (and edit) what was just saved. Reset on every
  // selection change so it does not bleed across providers.
  const [newlySavedInstanceName, setNewlySavedInstanceName] = useState<
    string | null
  >(null);

  // Monotonic counter so each draft card has a stable, unique React key.
  const draftIdCounterRef = useRef(0);
  // Tracks whether the user explicitly cancelled the auto-shown draft
  // for the current selection. Reset on every selection change.
  const cancelledRef = useRef(false);

  // Imperative refs to every visible card, keyed by the card's React key
  // (instance name for saved cards, draft id for drafts). Used by the
  // top Save button to validate + collect payloads in a single batch.
  const cardRefs = useRef<Map<string, ProviderInstanceCardRef | null>>(
    new Map(),
  );
  const setCardRef = useCallback(
    (id: string) => (ref: ProviderInstanceCardRef | null) => {
      if (ref) {
        cardRefs.current.set(id, ref);
      } else {
        cardRefs.current.delete(id);
      }
    },
    [],
  );

  const { data: addedProviders } = useFetchAddedProviders();
  const { data: availableProviders } = useFetchAvailableProviders();
  // Gate the instance query on `has_instance`, exactly as the tenant page
  // does. Providers the admin has never configured have no instance row at
  // all, and `GET /providers/{name}/instances` errors for them rather than
  // returning an empty list - so the query stays disabled until
  // `addedProviders` reports the provider as configured. Saving the first
  // instance invalidates `addedProviders`, which flips the flag and opens
  // the gate, so the new card surfaces.
  const providerQueryName = useMemo(() => {
    return addedProviders.some((p) => p.name === selection && p.has_instance)
      ? selection
      : '';
  }, [selection, addedProviders]);
  const { data: instances, loading: instancesLoading } =
    useFetchProviderInstances(providerQueryName);
  const queryClient = useQueryClient();

  // Append a new draft id to the visible list.
  const addDraft = useCallback(() => {
    draftIdCounterRef.current += 1;
    const id = `draft-${draftIdCounterRef.current}`;
    setDraftIds((prev) => [...prev, id]);
  }, []);

  // Remove a draft id from the visible list (called on cancel).
  const removeDraft = useCallback((id: string) => {
    setDraftIds((prev) => prev.filter((d) => d !== id));
  }, []);

  // `Sidebar` still offers its upstream `'default'` sentinel. Nothing here
  // can render that view, so ignore it rather than let it reach
  // `useFetchProviderInstances`.
  const handleSelect = useCallback((next: string) => {
    if (next === 'default') return;
    setSelection(next);
  }, []);

  // Land on a real provider as soon as the sidebar has something to show:
  // prefer a provider that already has an instance, otherwise fall back to
  // the full available list. `addedProviders` is empty on a fresh install,
  // so relying on it alone would leave the right pane blank forever.
  useEffect(() => {
    if (selection) return;
    const configured = addedProviders.find((p) => p.has_instance);
    const first = configured ?? addedProviders[0] ?? availableProviders[0];
    if (first) setSelection(first.name);
  }, [selection, addedProviders, availableProviders]);

  // When the selection changes, clear the cancelled flag, drop any
  // in-flight drafts, and reset the ref registry so stale refs from
  // the previous provider don't leak into the next save batch.
  useEffect(() => {
    cancelledRef.current = false;
    setDraftIds([]);
    cardRefs.current.clear();
    setNewlySavedInstanceName(null);
  }, [selection]);

  // If the user switches to a provider with no existing instances and
  // no drafts already on screen, auto-show a "new instance" draft so
  // they can fill it in immediately. If they have explicitly cancelled,
  // do not re-show.
  //
  // Wait for the provider-instances query to settle before deciding:
  // `initialData: []` on the hook means `instances` is an empty array
  // from the first render, so a naive length check would auto-spawn a
  // draft during the brief loading window even when the provider
  // already has saved instances, only to remove it once the query
  // resolves. Gating on `!instancesLoading` skips that flicker.
  useEffect(() => {
    if (!selection || cancelledRef.current) return;
    if (instancesLoading) return;
    if (instances.length === 0 && draftIds.length === 0) {
      addDraft();
    }
  }, [selection, instances, instancesLoading, draftIds, addDraft]);

  const { addProviderInstance } = useAddProviderInstance();
  const { updateProviderInstance } = useUpdateProviderInstance();

  // Batch save handler, wired to the top Save button: collect every card
  // ref, ask each for its payload (non-dirty saved cards return null and
  // are skipped), reject duplicate instance names up front, validate the
  // rest, then dispatch one API call per dirty card. Drafts go through
  // `addProviderInstance`, saved cards through `updateProviderInstance`.
  const handleSaveAll = useCallback(async () => {
    // Keep each card's registry key alongside its ref: for drafts the
    // key is the draft id, so a successfully-saved draft can be removed
    // from `draftIds` individually even when a later card in the batch
    // fails (the early return must not strand already-persisted drafts).
    const refs = Array.from(cardRefs.current.entries()).flatMap(([key, ref]) =>
      ref ? [{ key, ref }] : [],
    );
    if (refs.length === 0) return;

    const dirty = refs
      .map(({ key, ref }) => ({ key, ref, payload: ref.getSavePayload() }))
      .filter((e) => e.payload !== null);
    if (dirty.length === 0) return;

    // Reject duplicate instance names before any API call: a draft may
    // not reuse the name of a persisted instance, nor of another draft
    // in the same batch. Without this the backend would either error or
    // silently create a second instance sharing the name.
    const takenNames = new Set(instances.map((i) => i.instance_name));
    for (const { payload } of dirty) {
      if (!payload) continue;
      const name = payload.instanceName.trim();
      if (payload.isDraft && takenNames.has(name)) {
        message.error(tSetting('instanceNameExists'));
        return;
      }
      takenNames.add(name);
    }

    const validations = await Promise.all(
      dirty.map(async (e) => ({ ...e, valid: await e.ref.validate() })),
    );
    if (validations.some((v) => !v.valid)) {
      return;
    }

    setSaving(true);
    // Pin the auto-show guard so a draft isn't re-spawned while the
    // providerInstances refetch is in flight (during that window both
    // `instances` and `draftIds` are empty and would otherwise re-trigger
    // `addDraft()`).
    cancelledRef.current = true;
    try {
      // Sequential so a backend error stops the batch and the user can
      // retry the remaining cards after fixing the issue. Each
      // successfully persisted draft is removed immediately so it never
      // lingers beside its now-saved card when a later card fails.
      for (const { key, ref, payload } of validations) {
        if (!payload) continue;
        if (payload.apiKind === 'add') {
          const ret = await addProviderInstance(payload.payload as any);
          if (ret?.code !== 0) {
            return;
          }
          if (payload.isDraft) {
            setDraftIds((prev) => prev.filter((id) => id !== key));
            // Remember the just-saved name so the persisted card mounts
            // expanded once it surfaces via the invalidated instances
            // query.
            setNewlySavedInstanceName(payload.instanceName);
          }
        } else {
          const ret = await updateProviderInstance(payload.payload as any);
          if (ret?.code !== 0) {
            return;
          }
          // Saved card: update its dirty baseline so the next save
          // short-circuits.
          ref.markSaved();
        }
      }
      // Refresh the persisted instance list (covers updates; adds are
      // already invalidated by the add mutation) so saved cards show.
      queryClient.invalidateQueries({
        queryKey: LlmKeys.providerInstances(selection),
      });
    } finally {
      setSaving(false);
    }
  }, [
    addProviderInstance,
    updateProviderInstance,
    queryClient,
    selection,
    instances,
    tSetting,
  ]);

  // Whether the Save button should be enabled. We avoid an O(n) ref
  // scan on every render by treating "has any draft OR any saved
  // instance" as a conservative proxy - if there is nothing on screen
  // there is nothing to save, and if there is something the user can
  // always attempt a save (dirty saved cards short-circuit inside
  // `getSavePayload`). The button is disabled while a save is in flight.
  const canSave = !saving && (draftIds.length > 0 || instances.length > 0);

  // User clicked Cancel on a specific draft - remove it from the list
  // and stop the auto-show effect from re-opening it for the current
  // empty-instance selection.
  const handleDraftCancel = useCallback(
    (id: string) => {
      cancelledRef.current = true;
      removeDraft(id);
    },
    [removeDraft],
  );

  const draftInstance: IProviderInstance = useMemo(
    () => ({ instance_name: '' }) as IProviderInstance,
    [],
  );

  return (
    <div className="flex w-full h-full border-[0.5px] border-border-button rounded-lg relative overflow-hidden">
      <Spotlight />
      {/* The scoped `hidden` variant suppresses the upstream "Default
          models" entry, which has no meaning on the admin side. */}
      <section className="flex flex-col gap-4 w-[320px] shrink-0 px-5 border-r-[0.5px] border-border-button overflow-auto scrollbar-auto [&_[data-testid=sidebar-default-models]]:hidden">
        <Sidebar selection={selection} onSelect={handleSelect} />
      </section>
      <section className="flex-1 flex flex-col overflow-hidden">
        {!selection ? (
          <div className="flex-1 overflow-auto scrollbar-auto" />
        ) : (
          <>
            {/* Sticky top: provider name + doc-link arrow + batch Save */}
            <ProviderHeaderBar
              providerName={selection}
              onSave={handleSaveAll}
              saving={saving}
              canSave={canSave}
            />

            {/* Scrollable middle: instance cards + optional draft cards */}
            <div className="flex-1 overflow-auto scrollbar-auto p-4 flex flex-col gap-4">
              {instances.length === 0 && draftIds.length === 0 && (
                <div className="text-text-secondary text-sm py-6 text-center">
                  {tSetting('noInstancesConfigured')}
                </div>
              )}
              {instances.map((instance, index) => (
                <ProviderInstanceCard
                  key={instance.instance_name}
                  ref={setCardRef(instance.instance_name)}
                  providerName={selection}
                  instance={instance}
                  defaultOpen={
                    index === 0 ||
                    instance.instance_name === newlySavedInstanceName
                  }
                />
              ))}
              {draftIds.map((id) => (
                <ProviderInstanceCard
                  key={id}
                  ref={setCardRef(id)}
                  providerName={selection}
                  instance={draftInstance}
                  isDraft
                  onDelete={() => handleDraftCancel(id)}
                />
              ))}
              <div className="z-10 border-border-button py-4">
                <button
                  type="button"
                  className="w-full flex items-center justify-center gap-2 px-3 py-1 rounded-md border border-dashed border-border-button text-text-secondary hover:bg-bg-input hover:text-text-primary transition-colors"
                  onClick={addDraft}
                  data-testid="add-instance-bottom"
                >
                  <Plus className="size-4" />
                  <span className="text-sm">{tSetting('addInstanceText')}</span>
                </button>
              </div>
            </div>
          </>
        )}
      </section>
    </div>
  );
};

export default AdminModelProviders;
