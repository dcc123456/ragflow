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

import { Button } from '@/components/ui/button';
import { useTranslate } from '@/hooks/common-hooks';
import { ArrowLeft } from 'lucide-react';
import { useEffect, useId, useState } from 'react';

// Official WeCom login JS-SDK, loaded from the WeCom static CDN. Chrome 142+
// rejects older builds, so the version here must be >= 2.3.2. Keep the URL in
// sync with the login component documentation on developer.work.weixin.qq.com;
// nothing about it is bundled locally.
const QR_SDK_SRC =
  'https://wwcdn.weixin.qq.com/node/open/js/wecom-jssdk-2.3.2.js';

type WWLoginPanelParams = {
  login_type: 'CorpApp';
  appid: string;
  agentid: string;
  redirect_uri: string;
  state: string;
  // 'top' sends the code back by redirecting the whole page to the callback
  // URL, the exact contract the full-page redirect flow already serves. The
  // 'callback' variant delivers the code in JS but demands that the panel's
  // page and the redirect share a domain, which single-host deployments do
  // not always satisfy.
  redirect_type: 'top';
};

type WWLoginSdk = {
  createWWLoginPanel: (options: {
    el: string | HTMLElement;
    params: WWLoginPanelParams;
    onLoginSuccess?: (result: { code: string }) => void;
    onLoginFail?: (result: unknown) => void;
    onError?: (error: unknown) => void;
  }) => void;
};

declare global {
  interface Window {
    ww?: WWLoginSdk;
  }
}

let sdkPromise: Promise<WWLoginSdk> | null = null;

// Load the SDK script once per page; concurrent mounts share the promise.
const loadQrSdk = () => {
  if (sdkPromise) {
    return sdkPromise;
  }

  sdkPromise = new Promise((resolve, reject) => {
    if (window.ww?.createWWLoginPanel) {
      resolve(window.ww);
      return;
    }

    const script = document.createElement('script');
    script.src = QR_SDK_SRC;
    script.async = true;
    script.onload = () => {
      if (window.ww?.createWWLoginPanel) {
        resolve(window.ww);
      } else {
        sdkPromise = null;
        reject(new Error('WeCom login SDK loaded but window.ww is missing'));
      }
    };
    script.onerror = () => {
      sdkPromise = null;
      reject(new Error('Failed to load the WeCom login SDK'));
    };
    document.head.appendChild(script);
  });
  return sdkPromise;
};

type WecomQrLoginProps = {
  // Authorization URL from the initiate endpoint, with the state baked in.
  // The panel parameters (appid, agentid, redirect_uri, state) are parsed
  // out of it, so no extra API surface is needed.
  authUrl: string;
  onBack: () => void;
};

export default function WecomQrLogin({ authUrl, onBack }: WecomQrLoginProps) {
  const { t } = useTranslate('login');
  const [failed, setFailed] = useState(false);
  const containerId = `wecom-qr-${useId().replace(/:/g, '')}`;

  useEffect(() => {
    let cancelled = false;
    let el: HTMLElement | null = null;

    const auth = new URL(authUrl);
    const appid = auth.searchParams.get('appid') ?? '';
    const agentid = auth.searchParams.get('agentid') ?? '';
    const redirectUri = auth.searchParams.get('redirect_uri') ?? '';
    const state = auth.searchParams.get('state') ?? '';

    loadQrSdk()
      .then((sdk) => {
        if (cancelled) {
          return;
        }
        el = document.getElementById(containerId);
        if (!el) {
          setFailed(true);
          return;
        }
        sdk.createWWLoginPanel({
          el,
          params: {
            login_type: 'CorpApp',
            appid,
            agentid,
            redirect_uri: redirectUri,
            state,
            redirect_type: 'top',
          },
          onError: () => setFailed(true),
        });
      })
      .catch(() => {
        if (!cancelled) {
          setFailed(true);
        }
      });

    return () => {
      cancelled = true;
      // The panel is an iframe owned by the SDK; dropping the container
      // unmounts it.
      if (el) {
        el.replaceChildren();
      }
    };
  }, [authUrl, containerId]);

  return (
    <div className="flex flex-col items-center gap-4 py-4">
      <p className="text-sm text-text-secondary">{t('wecomQrTitle')}</p>
      {failed ? (
        <p className="max-w-72 text-center text-sm text-state-error">
          {t('wecomQrFailed')}
        </p>
      ) : (
        // The SDK iframe measures about 300x400; give the container the
        // exact box so the card does not jump when the panel mounts.
        <div id={containerId} className="h-[400px] w-[300px]" />
      )}
      <Button variant="ghost" size="sm" onClick={onBack}>
        <ArrowLeft className="size-4" />
        {t('back')}
      </Button>
    </div>
  );
}
