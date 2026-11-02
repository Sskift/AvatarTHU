'use strict';
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const {execFileSync} = require('node:child_process');
const assets = require('./assets.cjs');
const arg = name => process.argv[process.argv.indexOf(name) + 1];

(async () => {
  const {packager} = await import('@electron/packager');
  const platform = arg('--platform'), arch = arg('--arch'), out = path.resolve(arg('--out'));
  const backend = path.resolve(arg('--backend')), version = arg('--version') || '0.3.0';
  if (!['darwin', 'win32', 'linux'].includes(platform) || !['x64', 'arm64'].includes(arch)) throw new Error('Unsupported desktop target');
  await assets();
  const tmp = await fs.mkdtemp(path.join(os.tmpdir(), 'avatarthu-desktop-'));
  try {
    const resources = path.join(tmp, 'backend'); await fs.mkdir(resources);
    await fs.copyFile(backend, path.join(resources, path.basename(backend)));
    await fs.chmod(path.join(resources, path.basename(backend)), 0o755);
    const ui = path.join(tmp, 'ui'); await fs.mkdir(ui);
    for (const name of ['workspace_ui.html', 'workspace_ui.js', 'workspace_ui.css', 'editor_ui.html', 'editor_ui.js', 'editor_ui.css', 'desktop_ui.js', 'desktop_ui.css']) {
      await fs.copyFile(path.join(__dirname, '../internal/app', name), path.join(ui, name));
    }
    const paths = await packager({
      dir: __dirname, name: 'AvatarTHU', executableName: 'AvatarTHU', platform, arch,
      appVersion: version.split(/[+-]/)[0], appBundleId: 'com.local.avatarthu.desktop',
      appCategoryType: 'public.app-category.education', asar: true,
      icon: path.join(__dirname, 'assets', platform === 'darwin' ? 'icon.icns' : platform === 'win32' ? 'icon.ico' : 'icon.png'),
      out: path.join(tmp, 'output'), overwrite: true, extraResource: [resources, ui, path.join(out, 'licenses')],
      ...(process.env.AVATARTHU_ELECTRON_ZIP_DIR ? {electronZipDir: process.env.AVATARTHU_ELECTRON_ZIP_DIR} : {}),
      ignore: [/^\/test(?:\/|$)/, /^\/(?:assets|package)\.cjs$/, /^\/package-lock\.json$/],
      ...(platform === 'darwin' ? {extendInfo: {NSHighResolutionCapable: true}} : {})
    });
    const from = platform === 'darwin' ? path.join(paths[0], 'AvatarTHU.app') : paths[0];
    const target = path.join(out, platform === 'darwin' ? 'AvatarTHU.app' : 'AvatarTHU');
    await fs.cp(from, target, {recursive: true, verbatimSymlinks: true});
    const licenseDir = path.join(target, platform === 'darwin' ? 'Contents/Resources/licenses' : 'resources/licenses');
    await fs.mkdir(licenseDir, {recursive: true});
    for (const name of ['LICENSE', 'LICENSES.chromium.html']) {
      await fs.copyFile(path.join(paths[0], name), path.join(licenseDir, name === 'LICENSE' ? 'Electron-LICENSE' : name));
    }
    if (platform === 'darwin') execFileSync('codesign', ['--force', '--deep', '--sign', '-', target], {stdio: 'inherit'});
    console.log(target);
  } finally { await fs.rm(tmp, {recursive: true, force: true}); }
})().catch(error => { console.error(error.stack); process.exitCode = 1; });
