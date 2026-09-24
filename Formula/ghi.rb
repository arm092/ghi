class Ghi < Formula
  desc "Statically typed backend language with classes, compiling to Go"
  homepage "https://github.com/arm092/ghi"
  url "https://github.com/arm092/ghi/archive/refs/tags/v0.2.0.tar.gz"
  sha256 "e742056ede6e29daacf12de88a115d0d85d97e8a222bb64bd071247b610e29e0"
  license "MIT"

  depends_on "go"

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=v#{version}"), "./cmd/ghi"
    system "go", "build", *std_go_args(output: bin/"mojave"), "./cmd/mojave"
    pkgshare.install "LICENSE", "THIRD_PARTY_NOTICES"
  end

  test do
    assert_match "ghi v#{version}", shell_output("#{bin}/ghi version")
    assert_match "mojave", shell_output("#{bin}/mojave help").downcase
    (testpath/"main.ghi").write <<~GHI
      namespace main
      import fmt "go:fmt"
      func main() {
        fmt.Println("hello from Homebrew")
      }
    GHI
    system bin/"ghi", "build", "-o", testpath/"hello", testpath
    assert_equal "hello from Homebrew\n", shell_output(testpath/"hello")
  end
end
