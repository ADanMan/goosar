'use client';

export interface SystemNotificationPayload {
  slug: string;
  itemId: string;
  issueKey: string;
  title: string;
  body: string;
}

type ClickHandler = (payload: SystemNotificationPayload) => void;

let clickHandler: ClickHandler | null = null;

export function registerSystemNotificationClickHandler(handler: ClickHandler | null): void {
  clickHandler = handler;
}

function getNotificationCtor(): typeof Notification | null {
  if (typeof window === 'undefined') return null;
  const ctor = (window as { Notification?: typeof Notification }).Notification;
  return typeof ctor === 'function' ? ctor : null;
}

export function isWebNotificationSupported(): boolean {
  return getNotificationCtor() !== null;
}

export type WebNotificationPermission = NotificationPermission | 'unsupported';

export function getWebNotificationPermission(): WebNotificationPermission {
  const ctor = getNotificationCtor();
  return ctor ? ctor.permission : 'unsupported';
}

export async function requestWebNotificationPermission(): Promise<WebNotificationPermission> {
  const ctor = getNotificationCtor();
  if (!ctor) return 'unsupported';
  if (ctor.permission !== 'default') return ctor.permission;
  try {
    return await ctor.requestPermission();
  } catch {
    return ctor.permission;
  }
}

export function showWebNotification(payload: SystemNotificationPayload): void {
  const ctor = getNotificationCtor();
  if (!ctor || ctor.permission !== 'granted') return;
  let notification: Notification;
  try {
    notification = new ctor(payload.title, {
      body: payload.body,
      tag: payload.itemId,
    });
  } catch {
    return;
  }
  notification.onclick = () => {
    try {
      window.focus();
    } catch {
      // Best-effort; some browsers disallow programmatic focus.
    }
    notification.close();
    clickHandler?.(payload);
  };
}
