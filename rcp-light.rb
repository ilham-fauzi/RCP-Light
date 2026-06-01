cask "rcp-light" do
  version "1.5.0"
  sha256 "bf5b3df0873104f9a6c2ddfd0025b52dc74777105a3a01c224d3f38c40e8731c"

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
