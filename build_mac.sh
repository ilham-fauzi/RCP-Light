#!/bin/bash

APP_NAME="RCP Light"
APP_DIR="${APP_NAME}.app"
BINARY_NAME="rcp-light"

echo "------------------------------------------"
echo "🚀 Building ${APP_NAME} - Lite Edition"
echo "------------------------------------------"

# 1. Cleanup
echo "1. Cleaning up previous builds..."
rm -rf "${APP_DIR}"
rm -f "${BINARY_NAME}"

# 2. Build Go Binary
echo "2. Building Go binary..."
go build -o "${BINARY_NAME}" main.go dashboard.go login_window.go
if [ $? -ne 0 ]; then
    echo "❌ Error: Failed to build Go binary."
    exit 1
fi

# 3. Create Structure
echo "3. Creating App Bundle structure..."
mkdir -p "${APP_DIR}/Contents/MacOS"
mkdir -p "${APP_DIR}/Contents/Resources"

# 4. Move Binary
echo "4. Installing binary to bundle..."
mv "${BINARY_NAME}" "${APP_DIR}/Contents/MacOS/"


# 5. Info.plist
echo "5. Creating Info.plist..."
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
    <string>1.3.0</string>
    <key>LSUIElement</key>
    <true/>
</dict>
</plist>

echo "------------------------------------------"
echo "✅ SUCCESS! Lite Bundle created: ${APP_DIR}"
echo "------------------------------------------"
