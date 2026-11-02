'use strict';
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const {execFileSync} = require('node:child_process');
const sharp = require('sharp');

async function assets() {
  const dir = path.join(__dirname, 'assets');
  const source = await fs.readFile(path.join(dir, 'seal.svg'), 'utf8');
  const black = Buffer.from(source.replaceAll('#920783', '#000000'));
  for (const [name, size] of [['trayTemplate.png', 18], ['trayTemplate@2x.png', 36]]) await sharp(black).resize(size, size).png().toFile(path.join(dir, name));
  const seal = await sharp(Buffer.from(source.replaceAll('#920783', '#ffffff'))).resize(680, 680).png().toBuffer();
  const background = Buffer.from('<svg width="1024" height="1024"><rect x="24" y="24" width="976" height="976" rx="216" fill="#73518a"/><rect x="40" y="40" width="944" height="944" rx="204" fill="none" stroke="#ffffff" stroke-opacity=".15" stroke-width="3"/></svg>');
  const icon = await sharp(background).composite([{input: seal, left: 172, top: 172}]).png().toBuffer();
  await fs.writeFile(path.join(dir, 'icon.png'), icon);
  const png = await sharp(icon).resize(256, 256).png().toBuffer(), header = Buffer.alloc(22);
  header.writeUInt16LE(1, 2); header.writeUInt16LE(1, 4); header.writeUInt16LE(1, 10); header.writeUInt16LE(32, 12); header.writeUInt32LE(png.length, 14); header.writeUInt32LE(22, 18);
  await fs.writeFile(path.join(dir, 'icon.ico'), Buffer.concat([header, png]));
  if (process.platform === 'darwin') {
    const tmp = await fs.mkdtemp(path.join(os.tmpdir(), 'avatarthu-icon-'));
    try {
      const set = path.join(tmp, 'AvatarTHU.iconset'); await fs.mkdir(set);
      for (const size of [16, 32, 128, 256, 512]) for (const scale of [1, 2]) await sharp(icon).resize(size * scale, size * scale).png().toFile(path.join(set, `icon_${size}x${size}${scale === 2 ? '@2x' : ''}.png`));
      execFileSync('iconutil', ['-c', 'icns', set, '-o', path.join(dir, 'icon.icns')]);
    } finally { await fs.rm(tmp, {recursive: true, force: true}); }
  }
}
if (require.main === module) assets().catch(error => { console.error(error.message); process.exitCode = 1; });
module.exports = assets;
