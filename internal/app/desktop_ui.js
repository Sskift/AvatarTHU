'use strict';
if (window.avatarDesktop) {
  document.documentElement.dataset.desktop = window.avatarDesktop.platform;
  const drag = document.createElement('div'); drag.className = 'desktop-titlebar'; drag.setAttribute('aria-hidden', 'true'); document.body.prepend(drag);
  const paths = {
    overview: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
    courses: '<path d="M12 5v16M3 4c4-1 7 0 9 2 2-2 5-3 9-2v15c-4-1-7 0-9 2-2-2-5-3-9-2Z"/>',
    files: '<path d="M3 7V5a2 2 0 0 1 2-2h5l3 3h6a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/><path d="M3 9h18"/>',
    artifacts: '<path d="m12 3 9 5-9 5-9-5Zm-9 9 9 5 9-5M3 16l9 5 9-5"/>',
    settings: '<path d="M4 6h16M4 12h16M4 18h16"/><circle cx="9" cy="6" r="2" fill="currentColor"/><circle cx="15" cy="12" r="2" fill="currentColor"/><circle cx="8" cy="18" r="2" fill="currentColor"/>'
  };
  document.querySelectorAll('[data-nav]').forEach(link => {
    const span = link.querySelector('span'); if (span) span.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' + paths[link.dataset.nav] + '</svg>';
  });
  const seal = document.querySelector('.seal'); if (seal) seal.innerHTML = '<img src="/desktop/seal.svg" alt="">';
  const brand = document.querySelector('.workspace') && document.querySelector('.brand');
  if (brand && brand.firstChild?.nodeType === Node.TEXT_NODE) {
    const logo = document.createElement('img'); logo.src = '/desktop/seal.svg'; logo.alt = ''; logo.className = 'desktop-editor-seal';
    brand.firstChild.replaceWith(logo);
  }
  const foot = document.querySelector('.sidebar-foot small'); if (foot) foot.textContent = '课程与报告，保存在本机';
  if (typeof renderSettings === 'function') {
    const render = renderSettings;
    renderSettings = function (health) {
      render(health);
      const panel = document.createElement('section'); panel.className = 'panel desktop-controls';
      panel.innerHTML = '<div><span class="eyebrow">DESKTOP</span><h2>后台与连接</h2><p class="muted">关闭 App 窗口后，课程后台仍会继续工作。</p></div><div class="desktop-control-grid"><button data-desktop-action="start">启动后台</button><button data-desktop-action="stop">停止后台</button><button data-desktop-action="doctor">检查 Claude / Codex</button><button data-desktop-action="keepalive">检查学堂连接</button><button data-desktop-action="retry-codex">恢复 Codex 任务</button><button data-desktop-action="retry-claude">恢复 Claude 任务</button><button data-desktop-action="reconnect-lark">重连飞书</button><button data-desktop-action="login">登录网络学堂</button></div><div class="desktop-folder-actions"><button data-desktop-reveal="courses">在文件管理器中打开课程</button><button data-desktop-reveal="logs">查看日志目录</button></div><p class="desktop-command-result" role="status" hidden></p>';
      document.getElementById('content').append(panel);
    };
  }
  document.addEventListener('click', async event => {
    const button = event.target.closest('[data-desktop-action],[data-desktop-reveal]'); if (!button) return;
    const output = document.querySelector('.desktop-command-result'); button.disabled = true;
    if (output) { output.hidden = false; output.textContent = '正在处理…'; }
    try {
      const result = button.dataset.desktopAction ? await window.avatarDesktop.control(button.dataset.desktopAction) : await window.avatarDesktop.reveal(button.dataset.desktopReveal);
      if (output) output.textContent = result || '已打开';
    } catch (error) { if (output) output.textContent = error.message; }
    finally { button.disabled = false; }
  });
}
