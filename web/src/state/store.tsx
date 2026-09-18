import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { api, ApiError, type ActivityEntry, type Connection, type SessionInfo, type Settings, type TransferJob } from '../api/client';

// Engine availability surfaced to the UI.
export type EngineState = 'checking' | 'unavailable' | 'ready' | 'unauthorized';

// Pending trust prompt for an unknown host key / certificate.
export interface TrustPrompt {
  kind: 'ssh' | 'tls';
  hostPort: string;
  fingerprint: string;
  keyType?: string;
  subject?: string;
  detail?: string;
  onTrust: () => void;
}

interface Toast {
  id: number;
  tone: 'info' | 'success' | 'error';
  message: string;
}

interface StoreValue {
  engine: EngineState;
  authenticated: boolean;
  settings: Settings | null;
  connections: Connection[];
  sessions: SessionInfo[];
  transfers: TransferJob[];
  activity: ActivityEntry[];
  toasts: Toast[];
  trustPrompt: TrustPrompt | null;
  onboarded: boolean;
  markOnboarded: () => Promise<void>;
  refreshConnections: () => Promise<void>;
  refreshSessions: () => Promise<void>;
  refreshTransfers: () => Promise<void>;
  refreshActivity: () => Promise<void>;
  setTrustPrompt: (p: TrustPrompt | null) => void;
  trustFromError: (e: unknown, retryConnectionId: string) => void;
  notify: (tone: Toast['tone'], message: string) => void;
  dismissToast: (id: number) => void;
  setSettings: (s: Settings) => Promise<void>;
}

interface HandshakeData {
  settings: Settings;
  onboarded: boolean;
}

const StoreContext = createContext<StoreValue | null>(null);

export function StoreProvider({ children }: { children: React.ReactNode }) {
  const [engine, setEngine] = useState<EngineState>('checking');
  const [authenticated, setAuthenticated] = useState(false);
  const [settings, setSettingsState] = useState<Settings | null>(null);
  const [onboarded, setOnboarded] = useState(false);
  const [connections, setConnections] = useState<Connection[]>([]);
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [transfers, setTransfers] = useState<TransferJob[]>([]);
  const [activity, setActivity] = useState<ActivityEntry[]>([]);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [trustPrompt, setTrustPrompt] = useState<TrustPrompt | null>(null);
  const tokenReady = useRef(false);
  const wsRef = useRef<WebSocket | null>(null);

  const notify = useCallback((tone: Toast['tone'], message: string) => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t, { id, tone, message }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 6000);
  }, []);
  const dismissToast = useCallback((id: number) => {
    setToasts((t) => t.filter((x) => x.id !== id));
  }, []);

  const refreshConnections = useCallback(async () => {
    try {
      const r = await api.connections();
      setConnections(r.connections ?? []);
    } catch {
      /* surfaced elsewhere */
    }
  }, []);
  const refreshSessions = useCallback(async () => {
    try {
      const r = await api.sessions();
      setSessions(r.sessions ?? []);
    } catch {
      /* ignore */
    }
  }, []);
  const refreshTransfers = useCallback(async () => {
    try {
      const r = await api.transfers();
      setTransfers(r.transfers ?? []);
    } catch {
      /* ignore */
    }
  }, []);
  const refreshActivity = useCallback(async () => {
    try {
      const r = await api.activity();
      setActivity(r.activity ?? []);
    } catch {
      /* ignore */
    }
  }, []);

  // trustFromError builds the trust dialog directly from a 409 connect
  // response (untrusted host key / cert), so the prompt works even if the
  // WebSocket event stream isn't up yet. After trusting, it retries the
  // connection.
  const trustFromError = useCallback(
    (e: unknown, retryConnectionId: string) => {
      if (
        !(e instanceof ApiError) ||
        (e.code !== 'untrusted-host-key' && e.code !== 'untrusted-certificate')
      ) {
        return;
      }
      const d = ((e.data as Record<string, unknown>) ?? {}) as Record<string, unknown>;
      setTrustPrompt({
        kind:
          (d.kind as TrustPrompt['kind']) ??
          (e.code === 'untrusted-certificate' ? 'tls' : 'ssh'),
        hostPort: (d.hostPort as string) ?? '',
        fingerprint: (d.fingerprint as string) ?? '',
        keyType: d.keyType as string | undefined,
        subject: d.subject as string | undefined,
        detail: d.detail as string | undefined,
        onTrust: async () => {
          try {
            await api.trust(String(d.hostPort), String(d.fingerprint));
            notify('success', 'Identity trusted');
            try {
              await api.connect(retryConnectionId);
              await refreshSessions();
            } catch (inner) {
              trustFromError(inner, retryConnectionId);
            }
          } catch {
            notify('error', 'Could not record trust decision');
          }
        },
      });
    },
    [notify, refreshSessions],
  );

  // Bootstrap: if opened on /bootstrap/<code>, exchange the single-use code
  // for a token, then replace the URL so the code never stays in history.
  const doBootstrap = useCallback(async () => {
    const m = window.location.pathname.match(/^\/bootstrap\/([A-Za-z0-9_-]+)/);
    if (m) {
      try {
        await api.bootstrap(m[1]);
        window.history.replaceState(null, '', '/');
        tokenReady.current = true;
        return true;
      } catch {
        return false;
      }
    }
    return api.hasToken();
  }, []);

  const connectEvents = useCallback(() => {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
    const url = `${proto}://${window.location.host}/api/events`;
    const token = sessionStorage.getItem('remorasftp.token');
    const ws = new WebSocket(url, token ? [`remorasftp.${token}`] : ['remorasftp']);
    wsRef.current = ws;
    ws.onmessage = (ev) => {
      try {
        const e = JSON.parse(ev.data);
        switch (e.type) {
          case 'transfer.update': {
            const job = e.data;
            setTransfers((prev) => {
              const idx = prev.findIndex((j) => j.id === job.id);
              if (idx >= 0) {
                const copy = [...prev];
                copy[idx] = job;
                return copy;
              }
              return [job, ...prev];
            });
            break;
          }
          case 'connection.state': {
            const state = e.data?.state;
            if (state === 'connected') notify('success', e.message || 'Connected');
            if (state === 'disconnected') notify('info', e.message || 'Disconnected');
            void refreshSessions();
            break;
          }
          case 'auth.failed':
            notify('error', e.message || 'Authentication failed');
            break;
          case 'hostkey.prompt': {
            const prompt: TrustPrompt = {
              kind: e.data?.kind,
              hostPort: e.data?.hostPort,
              fingerprint: e.data?.fingerprint,
              keyType: e.data?.keyType,
              subject: e.data?.subject,
              detail: e.data?.detail,
              onTrust: async () => {
                try {
                  await api.trust(e.data.hostPort, e.data.fingerprint);
                  notify('success', 'Identity trusted');
                  if (e.data?.retryConnectionId) {
                    try {
                      await api.connect(e.data.retryConnectionId);
                      await refreshSessions();
                    } catch {
                      /* handled by further prompts */
                    }
                  }
                } catch {
                  notify('error', 'Could not record trust decision');
                }
              },
            };
            setTrustPrompt(prompt);
            break;
          }
          default:
            void refreshActivity();
        }
      } catch {
        /* ignore malformed events */
      }
    };
    ws.onclose = () => {
      // Reconnect after a short delay, unless the engine is gone.
      setTimeout(() => {
        if (document.visibilityState !== 'hidden' && sessionStorage.getItem('remorasftp.token')) {
          connectEvents();
        }
      }, 3000);
    };
  }, [notify, refreshSessions, refreshActivity]);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    async function check() {
      // 1. Is the engine reachable at all?
      let reachable = false;
      try {
        await api.health();
        reachable = true;
      } catch {
        reachable = false;
      }
      if (cancelled) return;
      if (!reachable) {
        setEngine('unavailable');
        setAuthenticated(false);
        timer = setTimeout(check, 2000);
        return;
      }
      setEngine('ready');
      // 2. Authenticate (one-time bootstrap or a token from a prior tab).
      const authed = await doBootstrap();
      if (cancelled) return;
      setAuthenticated(authed);
      if (!authed) {
        // Engine is up but the token is missing/stale (engine restarted).
        // Surface a dedicated "restart required" state rather than a
        // misleading connected UI.
        setEngine('unauthorized');
        timer = setTimeout(check, 3000);
        return;
      }
      // 3. Load initial state; a 401 here means the token is stale.
      try {
        const hs: HandshakeData = await api.handshake();
        if (cancelled) return;
        setSettingsState(hs.settings);
        setOnboarded(hs.onboarded);
        await Promise.all([refreshConnections(), refreshSessions(), refreshTransfers(), refreshActivity()]);
        connectEvents();
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) {
          api.clearToken();
          setAuthenticated(false);
        }
        timer = setTimeout(check, 3000);
      }
    }
    check();
    return () => {
      cancelled = true;
      clearTimeout(timer);
      wsRef.current?.close();
    };
  }, [doBootstrap, connectEvents, refreshConnections, refreshSessions, refreshTransfers, refreshActivity]);

  const setSettings = useCallback(
    async (s: Settings) => {
      await api.saveSettings(s);
      setSettingsState(s);
    },
    [],
  );

  const value = useMemo<StoreValue>(
    () => ({
      engine,
      authenticated,
      settings,
      connections,
      sessions,
      transfers,
      activity,
      toasts,
      trustPrompt,
      onboarded,
      markOnboarded: async () => {
        try {
          await api.onboard();
          setOnboarded(true);
        } catch {
          /* ignore */
        }
      },
      refreshConnections,
      refreshSessions,
      refreshTransfers,
      refreshActivity,
      setTrustPrompt,
      trustFromError,
      notify,
      dismissToast,
      setSettings,
    }),
    [
      engine, authenticated, settings, connections, sessions, transfers, activity, toasts,
      trustPrompt, onboarded, refreshConnections, refreshSessions, refreshTransfers, refreshActivity,
      trustFromError, notify, dismissToast, setSettings,
    ],
  );

  return <StoreContext.Provider value={value}>{children}</StoreContext.Provider>;
}

export function useStore() {
  const v = useContext(StoreContext);
  if (!v) throw new Error('useStore must be used inside StoreProvider');
  return v;
}

// isUnauthorized helps components detect token failures (e.g. engine
// restarted, new instance token).
export function isUnauthorized(err: unknown): boolean {
  return err instanceof ApiError && err.status === 401;
}
