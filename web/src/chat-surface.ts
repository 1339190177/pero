import { normalizeLocale, translate } from '../../locales';
import type { HostEvent } from './bootstrap';

export type ChatSurfaceContext = { dark: boolean; locale: string; active: boolean; scope?: { app: string; panel: string } };
export type ChatSurface = ReturnType<typeof mountChatSurface>;

/** The trusted shell owns commands. Domain App frames are never message sources here. */
export function mountChatSurface(container: HTMLElement, options: {
  command(value: object): Promise<void>; ready(): void; context: ChatSurfaceContext;
  url?: string;
}) {
  const url = new URL(options.url || 'pero://chat/chat.html');
  const origin = `${url.protocol}//${url.host}`;
  const frame = document.createElement('iframe');
  frame.dataset.peroChat = '';
  frame.title = translate(normalizeLocale(options.context.locale), 'chat.panel_title');
  frame.sandbox.add('allow-scripts', 'allow-same-origin');
  const status = document.createElement('div');
  status.className = 'chat-panel-status';
  status.setAttribute('role', 'status');
  const label = document.createElement('span');
  const retry = document.createElement('button');
  retry.textContent = translate(normalizeLocale(options.context.locale), 'apps.refresh');
  retry.hidden = true;
  status.append(label, retry);
  container.append(frame, status);
  let context = options.context;
  let connected = false;
  let failed = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const pending: HostEvent[] = [];
  let draft: string | undefined;
  const post = (type: string, value: object) => frame.contentWindow?.postMessage({ type, ...value }, origin);
  const renderStatus = () => {
    frame.title = translate(context.locale, 'chat.panel_title');
    label.textContent = translate(context.locale, failed ? 'chat.panel_timeout' : 'chat.panel_loading');
    retry.textContent = translate(context.locale, 'apps.refresh');
    retry.hidden = !failed;
    status.hidden = connected;
  };
  const start = () => {
    clearTimeout(timer);
    connected = false; failed = false; renderStatus();
    frame.src = url.href;
    timer = setTimeout(() => { if (!connected) { failed = true; renderStatus(); } }, 30000);
  };
  retry.onclick = start;
  const receive = (event: MessageEvent) => {
    if (event.source !== frame.contentWindow || event.origin !== origin) return;
    if (event.data?.type === 'pero:chat:connect') post('pero:chat:context', { context });
    else if (event.data?.type === 'pero:chat:ready') {
      connected = true; clearTimeout(timer); renderStatus();
      post('pero:chat:context', { context });
      for (const value of pending.splice(0)) post('pero:chat:event', { event: value });
      if (draft !== undefined) { post('pero:chat:compose', { text: draft }); draft = undefined; }
      options.ready();
    } else if (event.data?.type === 'pero:chat:error') {
      clearTimeout(timer); connected = false; failed = true; renderStatus();
    } else if (event.data?.type === 'pero:chat:command') {
      const command = event.data.command;
      if (!command || typeof command !== 'object' || Array.isArray(command)) return;
      void options.command(command).catch(error => post('pero:chat:event', { event: {
        version: 1, kind: command.thread_id ? 'session_error' : command.request_id ? 'context_error' : 'error',
        thread_id: command.thread_id, request_id: command.request_id ?? null, text: error instanceof Error ? error.message : String(error),
      } }));
    }
  };
  window.addEventListener('message', receive);
  start();
  return {
    frame,
    context(value: ChatSurfaceContext) { context = value; renderStatus(); post('pero:chat:context', { context }); },
    event(event: HostEvent) { if (connected) post('pero:chat:event', { event }); else pending.push(event); },
    compose(text: string) { if (connected) post('pero:chat:compose', { text }); else draft = text; },
    dispose() { clearTimeout(timer); window.removeEventListener('message', receive); frame.remove(); status.remove(); },
  };
}
