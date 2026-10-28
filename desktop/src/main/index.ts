import { app, BrowserWindow, ipcMain, Menu, protocol, net, dialog } from 'electron';
import { spawn, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { createInterface } from 'node:readline';
import { mkdirSync } from 'node:fs';
import { resolve, sep } from 'node:path';
import { pathToFileURL } from 'node:url';
import { normalizeLocale, translate, type Locale, type MessageKey } from '../../../locales';
import { RemoteAdapter } from './remote';

protocol.registerSchemesAsPrivileged([{ scheme: 'pero', privileges: { standard: true, secure: true, supportFetchAPI: true } }]);
if (process.env.PERO_DESKTOP_DATA_DIR) {
  mkdirSync(process.env.PERO_DESKTOP_DATA_DIR, { recursive: true });
  app.setPath('userData', process.env.PERO_DESKTOP_DATA_DIR);
}
const root = resolve(__dirname, '../..');
let window: BrowserWindow;
let host: ChildProcessWithoutNullStreams;
let ready = false;
let locale: Locale = 'en';
const t = (key: MessageKey) => translate(locale, key);
const send = (event: object) => { if (window && !window.isDestroyed()) window.webContents.send('pero:event', event); };
const remote = new RemoteAdapter(send);
const event = (kind: string, message_key: MessageKey) => ({ version: 1, kind, request_id: null, text: '', message_key });
const changeLocale = (value: string) => {
  if (!ready || !['en', 'zh-CN'].includes(value)) throw new Error('Invalid locale');
  host.stdin.write(JSON.stringify({ method: 'settings.locale', locale: value }) + '\n');
};
function updateMenus() {
  Menu.setApplicationMenu(Menu.buildFromTemplate([
    { label: 'Pero', submenu: [{ role: 'about', label: t('menu.about') }, { type: 'separator' }, { role: 'quit', label: t('menu.quit') }] },
    { label: t('menu.edit'), submenu: [
      { role: 'undo', label: t('menu.undo') }, { role: 'redo', label: t('menu.redo') }, { type: 'separator' },
      { role: 'cut', label: t('menu.cut') }, { role: 'copy', label: t('menu.copy') },
      { role: 'paste', label: t('menu.paste') }, { role: 'selectAll', label: t('menu.select_all') },
    ] },
    { label: t('menu.workbench'), submenu: [{ id: 'remote-tasks', label: t('nav.remote'), accelerator: 'CmdOrCtrl+Shift+R', click: () => send({version:1,kind:'remote_open',request_id:null,text:''}) }, { id: 'connection-message', label: t('menu.connection'), accelerator: 'CmdOrCtrl+Shift+E', click: () => send(event('notice', 'notice.desktop')) }, { role: 'reload', label: t('menu.reload') }, { role: 'toggleDevTools', label: t('menu.developer_tools') }] },
    { label: t('menu.language'), submenu: [
      { id: 'locale-zh-CN', label: '简体中文', type: 'radio', checked: locale === 'zh-CN', click: () => changeLocale('zh-CN') },
      { id: 'locale-en', label: 'English', type: 'radio', checked: locale === 'en', click: () => changeLocale('en') },
    ] },
  ]));
  if (window && !window.isDestroyed()) window.setTitle(t('window.title'));
}

app.whenReady().then(async () => {
  locale = normalizeLocale(process.env.PERO_LOCALE || app.getLocale());
  protocol.handle('pero', (request) => {
    const url = new URL(request.url);
    const directory = resolve(root, 'web/dist');
    const file = resolve(directory, '.' + decodeURIComponent(url.pathname === '/' ? '/index.html' : url.pathname));
    if (url.host !== 'workbench' || !file.startsWith(directory + sep)) return new Response('Not found', { status: 404 });
    return net.fetch(pathToFileURL(file).href).then((response) => {
      const headers = new Headers(response.headers);
      headers.set('Content-Security-Policy', "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; object-src 'none'");
      return new Response(response.body, { status: response.status, headers });
    });
  });
  // Local Agents must not inherit the Desktop's cloud operator/database credentials.
  const hostEnvironment = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('PERO_')));
  host = spawn(resolve(root, 'target/debug/pero-host'), ['--stdio'], { stdio: 'pipe', env: {
    ...hostEnvironment, PATH: [resolve(root,'node_modules/.bin'),process.env.PATH,resolve(app.getPath('home'),'.opencode/bin'),'/opt/homebrew/bin',resolve(app.getPath('home'),'.local/bin')].filter(Boolean).join(':'), PERO_LOCALE: locale,
    ...(process.env.PERO_AGENTS_FILE ? { PERO_AGENTS_FILE: process.env.PERO_AGENTS_FILE } : {}),
    PERO_SETTINGS_PATH: process.env.PERO_SETTINGS_PATH || resolve(app.getPath('userData'), 'runtime-settings.json'),
    PERO_HISTORY_PATH: process.env.PERO_HISTORY_PATH || resolve(app.getPath('userData'), 'sessions.jsonl'),
  } });
  createInterface({ input: host.stdout }).on('line', (line) => {
    try {
      const message = JSON.parse(line);
      if (message.kind === 'settings') { locale = normalizeLocale(message.text); updateMenus(); }
      if (message.kind === 'ready') ready = true;
      send(message);
    }
    catch { console.error('Invalid Host event'); }
  });
  host.stderr.on('data', (data) => console.error(String(data)));
  host.on('error', () => { ready = false; send(event('disconnected', 'error.host_start')); });
  host.on('exit', () => { ready = false; send(event('disconnected', 'error.host_stopped')); });
  const trusted = (sender: Electron.IpcMainInvokeEvent) => {
    if (sender.sender !== window.webContents || sender.senderFrame?.url !== 'pero://workbench/') throw new Error('Untrusted sender');
  };
  ipcMain.handle('pero:connect', async (sender) => {
    trusted(sender);
    // The child normally starts before the renderer; wait for its actual ready event.
    for (let i = 0; i < 100 && !ready; i++) await new Promise((resolveWait) => setTimeout(resolveWait, 20));
    return event(ready ? 'ready' : 'disconnected', ready ? 'status.host_ready' : 'error.host_wait');
  });
  ipcMain.handle('pero:locale:get', (sender) => { trusted(sender); return locale; });
  ipcMain.handle('pero:remote', (sender, command: unknown) => { trusted(sender); return remote.command(command); });
  ipcMain.handle('pero:locale:set', (sender, value: string) => { trusted(sender); changeLocale(value); });
  ipcMain.handle('pero:probe', (sender, request: unknown) => {
    trusted(sender);
    const r = request as { id?: unknown; text?: unknown };
    if (!r || typeof r.id !== 'string' || r.id.length > 128 || typeof r.text !== 'string' || Array.from(r.text).length > 4096 || !ready) throw new Error('Invalid probe');
    host.stdin.write(JSON.stringify({ id: r.id, text: r.text }) + '\n');
  });
  ipcMain.handle('pero:session', (sender, command: unknown) => {
    trusted(sender);
    if (!ready || !command || typeof command !== 'object') throw new Error('Invalid session command');
    const action = (command as { action?: string }).action;
    if (!['list_agents','new_session','prompt','cancel','permission','model','authenticate','close'].includes(action || '') || JSON.stringify(command).length > 260000) throw new Error('Invalid session command');
    host.stdin.write(JSON.stringify(command) + '\n');
  });
  ipcMain.handle('pero:workspace', async (sender) => {
    trusted(sender);
    const selected = await dialog.showOpenDialog(window, { properties: ['openDirectory'] });
    if (!selected.canceled && selected.filePaths[0]) host.stdin.write(JSON.stringify({action:'workspace',path:selected.filePaths[0]})+'\n');
  });
  ipcMain.handle('pero:context', async (sender) => {
    trusted(sender);
    const selected=await dialog.showOpenDialog(window,{properties:['openFile','multiSelections']});
    if(!selected.canceled && selected.filePaths.length)host.stdin.write(JSON.stringify({action:'attach_files',paths:selected.filePaths})+'\n');
  });
  window = new BrowserWindow({ width: 1488, height: 1086, minWidth: 1000, minHeight: 680,
    title: t('window.title'), backgroundColor: '#F7F9FC',
    webPreferences: { preload: resolve(__dirname, 'preload.cjs'), contextIsolation: true, nodeIntegration: false, sandbox: true } });
  window.webContents.setWindowOpenHandler(() => ({ action: 'deny' }));
  window.webContents.on('will-navigate', (event) => event.preventDefault());
  updateMenus();
  await window.loadURL('pero://workbench/');
});
app.on('window-all-closed', () => app.quit());
app.on('before-quit', () => { remote.close(); host?.stdin.end(); });
