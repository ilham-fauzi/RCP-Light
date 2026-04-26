# RCP Light 🚀

**RCP Light** is a high-efficiency TUI, GUI, and System Tray OpenVPN client. As the latest, optimized version of [RCP Network](https://github.com/ilham-fauzi/rcp-network), it’s rebuilt in **Go** for ultra-low CPU/RAM usage while keeping professional features like simultaneous connections and Touch ID support.

## 🌟 Why RCP Light?

While the original RCP Network provides a rich GUI experience, it can sometimes be resource-heavy for a background utility. **RCP Light** is built for users who:
- Prioritize performance and battery life.
- Primarily work within the terminal environment.
- Need a stable, lightweight VPN connection with a modern and elegant visual interface.

---

## ✨ Key Features

### 1. **Multiple Simultaneous Connections** 🌐
Unlike standard VPN clients, RCP Light supports connecting to multiple VPN profiles independently and simultaneously. You can stay connected to `Production` and `Staging` environments at the same time without any conflicts.

### 2. **Sudo Keep-alive & Touch ID Support** ☝️
Fully optimized for the macOS ecosystem:
- **Automatic Onboarding:** On the first run, the app offers to set up Touch ID support for `sudo` commands.
- **Single Verification:** Simply tap your finger once when the app opens. The *Keep-alive* feature maintains the authorization in the background, so you won't be prompted for passwords/fingerprints repeatedly when switching profiles.

### 3. **Professional Terminal UI (TUI)** 💻
A modern terminal interface powered by `Bubble Tea` and `Lipgloss`:
- **Fluid Activity Scanner:** Smooth, asynchronous "glint" animations for each active profile.
- **High-Res Pulse:** Connection indicators that pulse between green and blue for clear visual feedback.
- **Responsive Layout:** A clean, adaptive UI that scales its width based on your profile names and connection details.

### 4. **Real-time Traffic Monitoring** 📊
Monitor your data usage live:
- **Individual Speeds:** Real-time Download (↓) and Upload (↑) speeds for every connected profile.
- **Smooth Decay:** Speed numbers glide down gracefully using an *Exponential Moving Average (EMA)* algorithm, providing an organic feel.
- **Dynamic Units:** Automatically switches between Bytes, KB, and MB depending on traffic volume.

### 5. **Profile Management** 📁
Manage your VPN profiles directly within the TUI:
- **Import (`i`):** Add new `.ovpn` files using the native macOS file picker.
- **Delete (`x`):** Easily remove profiles you no longer need with a confirmation prompt.
- **Auto-Sync:** Changes in the TUI are automatically reflected in the System Tray menu.

### 6. **Professional Dashboard (GUI)** 🖥️
For users who prefer a graphical interface, RCP Light now includes a stunning **Webview Dashboard**:
- **Modern Aesthetics:** Vibrant colors, glassmorphism, and smooth animations.
- **Interactive Ring Status:** Visual power-button design with dynamic hover effects.
- **Synchronized State:** Any connection made in the Dashboard is instantly reflected in the TUI and System Tray.

### 7. **Advanced Credential Management** 🔐
- **Apply for this Session:** Enter your credentials once and apply them to all profiles for the current session.
- **Per-Profile Overrides:** Need different credentials for a specific server? You can easily override the default username/password at the time of connection.
- **Default Global Username:** Set your primary username once (Hotkey `u` in TUI or Profile icon in Dashboard) and it will be used as the default for all future connection prompts.

### 8. **System Tray Integration** 🖱️
Full background operation via the macOS Menu Bar:
- Centralized connection status.
- Quick-action menu for connecting/disconnecting profiles.
- One-click shortcuts to launch the Dashboard or Terminal UI.
- **Auto-Close:** The TUI is designed to intelligently close its own window/tab upon quitting (`q`) to keep your workspace clean.

---

## 🛠 Installation & Setup

### Prerequisites
1. **Go** (version 1.20+)
2. **OpenVPN** (Installed and accessible via terminal)
3. **macOS** (Optimized for Touch ID and AppleScript integration)

### Installation Steps
1. Clone this repository.
2. Navigate to the directory: `cd rcp-light`.
3. Build the application using the provided script:
   ```bash
   ./build_mac.sh
   ```
4. Move the resulting **`RCP Light.app`** to your `/Applications` folder.

---

### Launching the Dashboard (GUI)
The most visual way to interact with RCP Light:
```bash
go run main.go dashboard
```

### Running via Terminal (TUI)
For the keyboard-focused experience:
```bash
go run main.go ui
```

### Mini Mode
A super-compact status line for sidebars or small windows:
```bash
go run main.go mini
```

The app stores all configuration and logs in the `~/.rcp-network/` directory:
- **username.cred:** Stores your global default VPN username.
- **[profile].ovpn:** Your OpenVPN configuration files.
- **[profile].pid & [profile].log:** Internal process tracking and connection logs.
- **.tmp_auth:** Temporary secure file for OpenVPN authentication (automatically cleaned up).

---

## ⚖️ License
This application is developed for the internal efficiency of RCP Network. Please use responsibly.

---
*Built with ❤️ for lighter performance.*
