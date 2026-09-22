'use client';

import { useEffect } from 'react';
import type { WSEventType } from '../types';
import { useWS } from './provider';

type EventHandler = (payload: unknown, actorId?: string, actorType?: string) => void;

export function useWSEvent(event: WSEventType, handler: EventHandler) {
  const { subscribe } = useWS();

  useEffect(() => {
    const unsub = subscribe(event, handler);
    return unsub;
  }, [event, handler, subscribe]);
}

export function useWSReconnect(callback: () => void) {
  const { onReconnect } = useWS();

  useEffect(() => {
    const unsub = onReconnect(callback);
    return unsub;
  }, [callback, onReconnect]);
}
