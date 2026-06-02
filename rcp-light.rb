cask "rcp-light" do
  version "2.0.0"
  sha256 "5ab4f3135f5ec2e77528e7ca694f8806458920f1c927b472549f5bf8dfabe143"

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
