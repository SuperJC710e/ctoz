const fs = require('fs');
const path = require('path');

// 读取SVG文件内容
const svgContent = fs.readFileSync(path.join(__dirname, '../public/icon.svg'), 'utf8');

// 生成不同尺寸的SVG图标
const sizes = [16, 32, 48, 64, 128, 256, 512];

// 确保public目录存在
const publicDir = path.join(__dirname, '../public');
if (!fs.existsSync(publicDir)) {
  fs.mkdirSync(publicDir, { recursive: true });
}

// 生成不同尺寸的图标
sizes.forEach(size => {
  const iconPath = path.join(publicDir, `icon-${size}x${size}.svg`);
  const resizedSvg = svgContent.replace('width="512" height="512"', `width="${size}" height="${size}"`);
  fs.writeFileSync(iconPath, resizedSvg);
  console.log(`Generated: ${iconPath}`);
});

// 生成favicon.ico的替代方案 - 使用SVG
const faviconPath = path.join(publicDir, 'favicon.svg');
fs.writeFileSync(faviconPath, svgContent);
console.log(`Generated: ${faviconPath}`);

// 生成apple-touch-icon
const appleIconPath = path.join(publicDir, 'apple-touch-icon.svg');
fs.writeFileSync(appleIconPath, svgContent);
console.log(`Generated: ${appleIconPath}`);

console.log('\nIcon generation completed!');
console.log('Note: For production, consider converting SVG to ICO/PNG using online tools or ImageMagick.'); 