export interface QueueGuardOptions {
  baseUrl?: string;
  roomId?: string;
  sessionId?: string;
  autoRedirect?: boolean;
  playAudio?: boolean;
  notify?: boolean;
  onUpdate?: (data: QueueGuardUpdate) => void;
  onAdmitted?: (data: QueueGuardUpdate) => void;
  onError?: (error: any) => void;
}

export interface QueueGuardUpdate {
  ticket_number?: number;
  position: number;
  est_seconds: number;
  admitted: boolean;
  token?: string;
  is_prequeue?: boolean;
  room?: string;
  room_name?: string;
}

export interface QueueGuardState {
  position: number | null;
  estSeconds: number | null;
  ticketNumber: number | null;
  isAdmitted: boolean;
  isPreQueue: boolean;
  room: string;
  roomName: string;
  token: string | null;
  error: any | null;
}

export interface QueueGuardControls extends QueueGuardState {
  disconnect: () => void;
  reconnect: () => void;
  playChime: () => void;
  requestNotification: () => Promise<NotificationPermission | string>;
}

export declare class QueueGuardClient {
  constructor(options?: QueueGuardOptions);
  connect(): void;
  disconnect(): void;
}

export declare function playAdmissionChime(): void;
export declare function requestNotificationPermission(): Promise<NotificationPermission | string>;
export declare function triggerAdmissionNotification(title?: string, body?: string): Notification | null;
export declare function useQueueGuard(options?: QueueGuardOptions): QueueGuardControls;

declare const _default: {
  QueueGuardClient: typeof QueueGuardClient;
  useQueueGuard: typeof useQueueGuard;
  playAdmissionChime: typeof playAdmissionChime;
  requestNotificationPermission: typeof requestNotificationPermission;
  triggerAdmissionNotification: typeof triggerAdmissionNotification;
};

export default _default;
