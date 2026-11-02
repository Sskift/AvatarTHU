'use strict';
const {contextBridge, ipcRenderer} = require('electron');
if (location.protocol === 'avatarthu:' && location.hostname === 'workspace' && ['/', '/editor', '/offline'].includes(location.pathname)) {
  contextBridge.exposeInMainWorld('avatarDesktop', Object.freeze({
    platform: process.platform,
    control: action => ipcRenderer.invoke('avatar:control', action),
    reveal: place => ipcRenderer.invoke('avatar:reveal', place),
    reconnect: () => ipcRenderer.invoke('avatar:reconnect'),
    refresh: () => ipcRenderer.invoke('avatar:refresh'),
    onState: callback => {
      if (typeof callback !== 'function') throw new TypeError('Expected a state listener');
      const listener = (_event, state) => callback(state);
      ipcRenderer.on('avatar:state', listener);
      return () => ipcRenderer.removeListener('avatar:state', listener);
    }
  }));
  window.addEventListener('DOMContentLoaded', () => { document.documentElement.dataset.desktop = process.platform; });
}
