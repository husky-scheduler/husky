class Husky < Formula
  desc "Local-first job scheduler with dependency graphs"
  homepage "https://github.com/husky-scheduler/husky"
  version "0.0.0-dev"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/husky-scheduler/husky/releases/download/v#{version}/husky_darwin_arm64.tar.gz"
      sha256 "aa1a3e554a7bcd6d0c2aa8a7a42ffc4e013cc8d0e9dc1d798a6fbbefe69ee833"
    else
      url "https://github.com/husky-scheduler/husky/releases/download/v#{version}/husky_darwin_amd64.tar.gz"
      sha256 "e7c8b87eef020891977e41a6ae85e421913b0eda59fb5751dd664bac8978f153"
    end
  end

  def install
    bin.install "husky"
  end

  service do
    run [opt_bin/"husky", "daemon", "run"]
    keep_alive true
    working_dir var
    log_path var/"log/huskyd.log"
    error_log_path var/"log/huskyd.error.log"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/husky version")
  end
end
