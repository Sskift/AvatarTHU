'use strict';
const {contextBridge, ipcRenderer} = require('electron');
if (location.protocol === 'avatarthu:' && location.hostname === 'workspace' && ['/', '/editor', '/offline'].includes(location.pathname)) {
  contextBridge.exposeInMainWorld('avatarDesktop', Object.freeze({
    platform: process.platform,
    control: action => ipcRenderer.invoke('avatar:control', action),
    reveal: place => ipcRenderer.invoke('avatar:reveal', place),
    reconnect: () => ipcRenderer.invoke('avatar:reconnect')
  }));
  window.addEventListener('DOMContentLoaded', () => { document.documentElement.dataset.desktop = process.platform; });
}
