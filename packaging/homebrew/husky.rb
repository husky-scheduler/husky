class Husky < Formula
  desc "Local-first job scheduler with dependency graphs"
  homepage "https://github.com/husky-scheduler/husky"
  version "0.1.0-alpha.1"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/husky-scheduler/husky/releases/download/v#{version}/husky_darwin_arm64.tar.gz"
      sha256 "f96ddee1523a49eac3421aa82eacd0ad04aa4cbef732328ded3c91692832141f"
    else
      url "https://github.com/husky-scheduler/husky/releases/download/v#{version}/husky_darwin_amd64.tar.gz"
      sha256 "a59db9e5ae917f3ac95edeab0f5baa32f45805423ba25800c15026a23171df27"
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
