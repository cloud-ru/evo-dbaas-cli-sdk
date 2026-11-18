# Файл генерирует scripts/brew-formula.sh при каждом релизе — руками не править.
class CloudruDbaas < Formula
  desc "CLI для Managed ClickHouse, Redis, Kafka, PostgreSQL и OpenSearch в cloud.ru"
  homepage "https://github.com/cloud-ru/evo-dbaas-cli-sdk"
  version "0.6.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/cloud-ru/evo-dbaas-cli-sdk/releases/download/v0.6.0/cloudru-dbaas_darwin_arm64"
      sha256 "8fa6de9054872378b2c2568bbdc18381f6b51a9cb2fc999563fc6c9bea7f6a42"
    end
    on_intel do
      url "https://github.com/cloud-ru/evo-dbaas-cli-sdk/releases/download/v0.6.0/cloudru-dbaas_darwin_amd64"
      sha256 "92c88178fa0c1c97eb1d0deca6bb0cf04bb83c06bfb85a59f85804ef2e738d85"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/cloud-ru/evo-dbaas-cli-sdk/releases/download/v0.6.0/cloudru-dbaas_linux_arm64"
      sha256 "510a28372ed1802ff4a2f2aca7a96beca381a46734dc43dc14ae14212af398f4"
    end
    on_intel do
      url "https://github.com/cloud-ru/evo-dbaas-cli-sdk/releases/download/v0.6.0/cloudru-dbaas_linux_amd64"
      sha256 "f74773236b37793969f6587d6697c4b56a27a0dfce6c1b22329180e377b31ba1"
    end
  end

  def install
    bin.install Dir["cloudru-dbaas_*"].first => "cloudru-dbaas"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/cloudru-dbaas version")
  end
end
