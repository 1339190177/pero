import { contextBridge, ipcRenderer } from 'electron';
contextBridge.exposeInMainWorld('product', {
  connect: () => ipcRenderer.invoke('product:connect'),
  open: () => ipcRenderer.invoke('product:open'),
  onEvent: (receive: (event: unknown) => void) => ipcRenderer.on('product:event', (_event, value) => receive(value)),
  onContext: (receive: (context: unknown) => void) => ipcRenderer.on('product:context', (_event, value) => receive(value)),
});
