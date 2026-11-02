document.getElementById('reconnect').addEventListener('click', async () => {
  const button = document.getElementById('reconnect'); button.disabled = true;
  document.getElementById('message').textContent = '正在连接…';
  try { await window.avatarDesktop.reconnect(); }
  catch (error) { document.getElementById('message').textContent = error.message; }
  finally { button.disabled = false; }
});
