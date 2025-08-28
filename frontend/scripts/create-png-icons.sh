#!/bin/bash

# 检查是否安装了ImageMagick
if ! command -v convert &> /dev/null; then
    echo "ImageMagick not found. Installing..."
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        brew install imagemagick
    elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
        # Linux
        sudo apt-get update && sudo apt-get install -y imagemagick
    else
        echo "Please install ImageMagick manually: https://imagemagick.org/script/download.php"
        exit 1
    fi
fi

echo "Generating PNG icons from SVG..."

# 生成不同尺寸的PNG图标
sizes=(16 32 48 64 128 256 512)

for size in "${sizes[@]}"; do
    echo "Generating ${size}x${size} PNG icon..."
    convert -background transparent -size "${size}x${size}" public/icon.svg "public/icon-${size}x${size}.png"
done

# 生成favicon.ico (16x16, 32x32, 48x48)
echo "Generating favicon.ico..."
convert public/icon-16x16.png public/icon-32x32.png public/icon-48x48.png public/favicon.ico

# 生成apple-touch-icon.png
echo "Generating apple-touch-icon.png..."
convert -background transparent -size "180x180" public/icon.svg public/apple-touch-icon.png

echo "PNG icon generation completed!"
echo "Generated files:"
ls -la public/*.png public/favicon.ico 