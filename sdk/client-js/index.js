/**
 * @queueguard/client - Official Client SDK for QueueGuard Virtual Waiting Room
 * Zero external dependencies. Works in Vanilla JS, React, Vue, Angular, Next.js.
 */

// --- Audio & Notification Helpers ---

export function playAdmissionChime() {
  try {
    const AudioContext = window.AudioContext || window.webkitAudioContext;
    if (!AudioContext) return;
    const ctx = new AudioContext();
    const now = ctx.currentTime;
    
    // Two-tone bell chime (E5 -> A5)
    const notes = [659.25, 880.0];
    notes.forEach((freq, idx) => {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = 'sine';
      osc.frequency.setValueAtTime(freq, now + idx * 0.15);
      gain.gain.setValueAtTime(0.25, now + idx * 0.15);
      gain.gain.exponentialRampToValueAtTime(0.001, now + idx * 0.15 + 0.4);
      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.start(now + idx * 0.15);
      osc.stop(now + idx * 0.15 + 0.4);
    });
  } catch (e) {
    console.warn('[QueueGuard] Audio chime failed:', e);
  }
}

export function requestNotificationPermission() {
  if (typeof window !== 'undefined' && 'Notification' in window && Notification.permission === 'default') {
    return Notification.requestPermission();
  }
  return Promise.resolve(typeof window !== 'undefined' && 'Notification' in window ? Notification.permission : 'denied');
}

export function triggerAdmissionNotification(title = 'QueueGuard - Đến lượt bạn!', body = 'Hệ thống đã sẵn sàng đón nhận bạn.') {
  if (typeof window !== 'undefined' && 'Notification' in window && Notification.permission === 'granted') {
    try {
      return new Notification(title, {
        body,
        icon: 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><text y=".9em" font-size="90">🎉</text></svg>'
      });
    } catch (e) {
      console.warn('[QueueGuard] Notification failed:', e);
    }
  }
  return null;
}

// --- Vanilla JavaScript QueueGuardClient ---

export class QueueGuardClient {
  constructor(options = {}) {
    this.baseUrl = options.baseUrl || '';
    this.roomId = options.roomId || '';
    this.sessionId = options.sessionId || '';
    this.autoRedirect = options.autoRedirect !== false;
    this.playAudio = options.playAudio !== false;
    this.notify = options.notify !== false;

    this.onUpdate = options.onUpdate || (() => {});
    this.onAdmitted = options.onAdmitted || (() => {});
    this.onError = options.onError || (() => {});

    this.eventSource = null;
    this.isClosed = false;
  }

  connect() {
    if (typeof window === 'undefined' || typeof EventSource === 'undefined') {
      return;
    }

    this.isClosed = false;
    const params = new URLSearchParams();
    if (this.roomId) params.set('room', this.roomId);
    if (this.sessionId) params.set('session_id', this.sessionId);

    const qs = params.toString();
    const url = `${this.baseUrl}/queueguard/sse${qs ? '?' + qs : ''}`;

    this.eventSource = new EventSource(url);

    this.eventSource.onmessage = (e) => {
      try {
        const data = JSON.parse(e.data);
        this.onUpdate(data);

        if (data.admitted) {
          if (this.playAudio) playAdmissionChime();
          if (this.notify) triggerAdmissionNotification();

          // Set cookie if token provided
          if (data.token) {
            document.cookie = `queueguard_ticket=${data.token}; path=/; max-age=600; SameSite=Lax`;
          }

          this.onAdmitted(data);

          this.disconnect();

          if (this.autoRedirect) {
            setTimeout(() => {
              window.location.reload();
            }, 500);
          }
        }
      } catch (err) {
        this.onError(err);
      }
    };

    this.eventSource.onerror = (err) => {
      this.onError(err);
    };
  }

  disconnect() {
    this.isClosed = true;
    if (this.eventSource) {
      this.eventSource.close();
      this.eventSource = null;
    }
  }
}

// --- React Hook: useQueueGuard ---

export function useQueueGuard(options = {}) {
  // Dynamically require React if available
  let React;
  try {
    React = require('react');
  } catch (e) {
    throw new Error('[QueueGuard] useQueueGuard requires "react" to be installed as a peer dependency.');
  }

  const { useState, useEffect, useRef } = React;

  const [state, setState] = useState({
    position: null,
    estSeconds: null,
    ticketNumber: null,
    isAdmitted: false,
    isPreQueue: false,
    room: options.roomId || '',
    roomName: '',
    token: null,
    error: null,
  });

  const clientRef = useRef(null);

  useEffect(() => {
    const client = new QueueGuardClient({
      ...options,
      onUpdate: (data) => {
        setState((prev) => ({
          ...prev,
          position: data.position,
          estSeconds: data.est_seconds,
          ticketNumber: data.ticket_number,
          isAdmitted: !!data.admitted,
          isPreQueue: !!data.is_prequeue,
          room: data.room || prev.room,
          roomName: data.room_name || prev.roomName,
          token: data.token || prev.token,
          error: null,
        }));
        if (options.onUpdate) options.onUpdate(data);
      },
      onAdmitted: (data) => {
        setState((prev) => ({ ...prev, isAdmitted: true, token: data.token }));
        if (options.onAdmitted) options.onAdmitted(data);
      },
      onError: (err) => {
        setState((prev) => ({ ...prev, error: err }));
        if (options.onError) options.onError(err);
      },
    });

    clientRef.current = client;
    client.connect();

    return () => {
      client.disconnect();
    };
  }, [options.baseUrl, options.roomId, options.sessionId]);

  return {
    ...state,
    disconnect: () => clientRef.current && clientRef.current.disconnect(),
    reconnect: () => clientRef.current && clientRef.current.connect(),
    playChime: playAdmissionChime,
    requestNotification: requestNotificationPermission,
  };
}

export default {
  QueueGuardClient,
  useQueueGuard,
  playAdmissionChime,
  requestNotificationPermission,
  triggerAdmissionNotification,
};
