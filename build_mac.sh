#!/bin/bash

APP_NAME="RCP Light"
APP_DIR="${APP_NAME}.app"
BINARY_NAME="rcp-light"
VERSION="1.5.0"

echo "------------------------------------------"
echo "🚀 Building ${APP_NAME} v${VERSION}"
echo "------------------------------------------"

# 1. Cleanup
echo "1. Cleaning up previous builds..."
rm -rf "${APP_DIR}"
rm -f "${BINARY_NAME}"

# 2. Build Go Binary
echo "2. Building Go binary..."
go build -o "${BINARY_NAME}" main.go dashboard.go login_window.go icon.go
if [ $? -ne 0 ]; then
    echo "❌ Error: Failed to build Go binary."
    exit 1
fi

echo "3. Generating App Icon..."
if [ -f "icon.png" ]; then
    mkdir -p icon.iconset
    sips -z 16 16     icon.png --out icon.iconset/icon_16x16.png &> /dev/null
    sips -z 32 32     icon.png --out icon.iconset/icon_16x16@2x.png &> /dev/null
    sips -z 32 32     icon.png --out icon.iconset/icon_32x32.png &> /dev/null
    sips -z 64 64     icon.png --out icon.iconset/icon_32x32@2x.png &> /dev/null
    sips -z 128 128   icon.png --out icon.iconset/icon_128x128.png &> /dev/null
    sips -z 256 256   icon.png --out icon.iconset/icon_128x128@2x.png &> /dev/null
    sips -z 256 256   icon.png --out icon.iconset/icon_256x256.png &> /dev/null
    sips -z 512 512   icon.png --out icon.iconset/icon_256x256@2x.png &> /dev/null
    sips -z 512 512   icon.png --out icon.iconset/icon_512x512.png &> /dev/null
    sips -z 1024 1024 icon.png --out icon.iconset/icon_512x512@2x.png &> /dev/null
    iconutil -c icns icon.iconset
    rm -R icon.iconset
else
    echo "⚠️ icon.png not found! Skipping icon generation."
fi

# 4. Create Structure
echo "4. Creating App Bundle structure..."
mkdir -p "${APP_DIR}/Contents/MacOS"
mkdir -p "${APP_DIR}/Contents/Resources"

# 5. Move Binary and Icon
echo "5. Installing binary to bundle..."
mv "${BINARY_NAME}" "${APP_DIR}/Contents/MacOS/"
if [ -f "icon.icns" ]; then
    mv "icon.icns" "${APP_DIR}/Contents/Resources/"
fi

# 6. Info.plist
echo "6. Creating Info.plist..."
cat << PLIST > "${APP_DIR}/Contents/Info.plist"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleExecutable</key>
    <string>${BINARY_NAME}</string>
    <key>CFBundleIconFile</key>
    <string>icon.icns</string>
    <key>CFBundleIdentifier</key>
    <string>com.rcp.network.light</string>
    <key>CFBundleName</key>
    <string>${APP_NAME}</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>${VERSION}</string>
    <key>LSUIElement</key>
    <true/>
</dict>
</plist>

echo "------------------------------------------"
echo "✅ SUCCESS! ${APP_DIR} created (v${VERSION})"
echo "  open ${APP_DIR}"
echo "------------------------------------------"
