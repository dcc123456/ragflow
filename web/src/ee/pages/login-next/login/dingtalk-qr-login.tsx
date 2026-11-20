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

import type { ChannelLoginInitiation } from '@/ee/services/user-service';

// Official DingTalk QR login SDK, loaded from the DingTalk static CDN. Keep
// the URL in sync with the embedded login documentation on open.dingtalk.com.
// The script exposes window.DTFrameLogin and renders the QR code for the
// enterprise's own login page inside the container element; nothing about it
// is bundled locally.
const QR_SDK_SRC =
  'https://g.alicdn.com/dingding/h5-dingtalk-login/0.0.5/ddlogin.js';

interface DTFrameLoginContainer {
  id: string;
  width?: number;
  height?: number;
}

interface DTFrameLoginParams {
  client_id: string;
  redirect_uri: string;
  scope: string;
  state: string;
  prompt?: string;
}

interface DTFrameLoginResult {
  redirect_url: string;
  authCode: string;
  state: string;
}

declare global {
  interface Window {
    DTFrameLogin?: (
      container: DTFrameLoginContainer,
      params: DTFrameLoginParams,
      onSuccess: (result: DTFrameLoginResult) => void,
      onError: (errorMessage: string) => void,
    ) => void;
  }
}

let sdkPromise: Promise<NonNullable<Window['DTFrameLogin']>> | null = null;

// Load the SDK script once per page; concurrent mounts share the promise.
const loadQrSdk = () => {
  if (sdkPromise) {
    return sdkPromise;
  }

  sdkPromise = new Promise((resolve, reject) => {
    if (window.DTFrameLogin) {
      resolve(window.DTFrameLogin);
      return;
    }

    const script = document.createElement('script');
    script.src = QR_SDK_SRC;
    script.async = true;
    script.onload = () => {
      if (window.DTFrameLogin) {
        resolve(window.DTFrameLogin);
      } else {
        sdkPromise = null;
        reject(
          new Error(
            'DingTalk QR SDK loaded but window.DTFrameLogin is missing',
          ),
        );
      }
    };
    script.onerror = () => {
      sdkPromise = null;
      reject(new Error('Failed to load the DingTalk QR SDK'));
    };
    document.head.appendChild(script);
  });
  return sdkPromise;
};

type DingtalkQrLoginProps = {
  // What initiate returned: client_id, redirect_uri and the server-written
  // state feed the widget, and the scope is read back out of the authorize
  // URL so the channel's configured scope drives the QR code too.
  initiation: ChannelLoginInitiation;
  onBack: () => void;
};

export default function DingtalkQrLogin({
  initiation,
  onBack,
}: DingtalkQrLoginProps) {
  const { t } = useTranslate('login');
  const [failed, setFailed] = useState(false);
  const containerId = `dingtalk-qr-${useId().replace(/:/g, '')}`;

  const {
    client_id: clientId,
    redirect_uri: redirectUri,
    state,
    auth_url,
  } = initiation;

  useEffect(() => {
    if (!clientId || !redirectUri) {
      // initiate only sends these when the channel is fully configured; a
      // bare fallback means the operator has no callback URL set, which the
      // widget cannot run without.
      setFailed(true);
      return;
    }

    let scope = 'openid';
    try {
      scope = new URL(auth_url).searchParams.get('scope') || scope;
    } catch {
      // auth_url stays the fallback scope if it is not a parseable URL.
    }

    let cancelled = false;

    loadQrSdk()
      .then((DTFrameLogin) => {
        if (cancelled) {
          return;
        }

        // Unlike the Feishu widget, the success callback hands the authCode
        // over in JS instead of redirecting the page: the navigation back to
        // our callback route (which validates the state cookie) is ours to
        // make, and redirect_url is the URL the provider itself would have
        // bounced to.
        DTFrameLogin(
          { id: containerId, width: 300, height: 300 },
          {
            client_id: clientId,
            redirect_uri: redirectUri,
            scope,
            state,
            prompt: 'consent',
          },
          (result) => {
            window.location.href = result.redirect_url;
          },
          () => {
            if (!cancelled) {
              setFailed(true);
            }
          },
        );
      })
      .catch(() => {
        if (!cancelled) {
          setFailed(true);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [auth_url, clientId, containerId, redirectUri, state]);

  return (
    <div className="flex flex-col items-center gap-4 py-4">
      <p className="text-sm text-text-secondary">{t('dingtalkQrTitle')}</p>
      {failed ? (
        <p className="max-w-72 text-center text-sm text-state-error">
          {t('dingtalkQrFailed')}
        </p>
      ) : (
        <div id={containerId} />
      )}
      <Button variant="ghost" size="sm" onClick={onBack}>
        <ArrowLeft className="size-4" />
        {t('back')}
      </Button>
    </div>
  );
}
