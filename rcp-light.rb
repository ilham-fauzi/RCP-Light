cask "rcp-light" do
  version "1.3.1"
  sha256 "691ac85100917bc224cc408659e71f87e822d04022f6aa39d08acb04e499aa55"

  url "https://github.com/ilham-fauzi/RCP-Light/releases/download/v#{version}/rcp-light-v#{version}.zip"
  name "RCP Light"
  desc "High-performance, ultra-lightweight OpenVPN client for macOS"
  homepage "https://github.com/ilham-fauzi/RCP-Light"

  app "RCP Light.app"

  zap trash: [
    "~/.rcp-network",
    "~/Library/Preferences/com.rcp.network.light.plist",
  ]
end
