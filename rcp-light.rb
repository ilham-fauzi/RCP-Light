cask "rcp-light" do
  version "1.4.0"
  sha256 "434b7b9087184d9983350b9c515cfb4a98ad979612ae3b1f24b1c5c336e956a4"

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
