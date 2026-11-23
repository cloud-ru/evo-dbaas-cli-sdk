# Файл генерирует scripts/brew-formula.sh при каждом релизе — руками не править.
class CloudruDbaas < Formula
  desc "CLI для Managed ClickHouse, Redis, Kafka, PostgreSQL и OpenSearch в cloud.ru"
  homepage "https://github.com/cloud-ru/evo-dbaas-cli-sdk"
  version "0.6.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/cloud-ru/evo-dbaas-cli-sdk/releases/download/v0.6.1/cloudru-dbaas_darwin_arm64"
      sha256 "7ec91bc9827da314ae8469d9c8c3ca676296eb546ff5dcad1b87ba9284cd6a63"
    end
    on_intel do
      url "https://github.com/cloud-ru/evo-dbaas-cli-sdk/releases/download/v0.6.1/cloudru-dbaas_darwin_amd64"
      sha256 "f3facca9a48b45719c2b99d46a542ae6bee6450a8d767d46490108421d7415fa"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/cloud-ru/evo-dbaas-cli-sdk/releases/download/v0.6.1/cloudru-dbaas_linux_arm64"
      sha256 "026676b852013b46b327881a71eed51a14f8cdfe96ea2c3efe14e4e71ec4d227"
    end
    on_intel do
      url "https://github.com/cloud-ru/evo-dbaas-cli-sdk/releases/download/v0.6.1/cloudru-dbaas_linux_amd64"
      sha256 "01db08680ab54c6e5795ac4e6eb5a1a302ba51a36b461bc4c4deef909e1cd054"
    end
  end

  def install
    bin.install Dir["cloudru-dbaas_*"].first => "cloudru-dbaas"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/cloudru-dbaas version")
  end
end
