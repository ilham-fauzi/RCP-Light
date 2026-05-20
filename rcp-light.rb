cask "rcp-light" do
  version "1.4.2"
  sha256 "8c478a89ab2533498b6ba75eb05ab93275e7ddf7f49b9f8c9ec16bf6205dfe72"

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
