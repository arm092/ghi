class Ghi < Formula
  desc "Statically typed backend language with classes, compiling to Go"
  homepage "https://github.com/arm092/ghi"
  url "https://github.com/arm092/ghi/archive/refs/tags/v0.2.2.tar.gz"
  sha256 "7e94731ca7d5701fed470ee0bcdb0d9c6f238a20c903d886f110bbb033efa723"
  license "MIT"

  depends_on "go"

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=v#{version}"), "./cmd/ghi"
    system "go", "build", *std_go_args(output: bin/"mojave", ldflags: "-s -w -X main.version=v0.1.0"), "github.com/arm092/mojave/cmd/mojave"
    pkgshare.install "LICENSE", "THIRD_PARTY_NOTICES"
  end

  test do
    assert_match "ghi v#{version}", shell_output("#{bin}/ghi version")
    assert_match "mojave v0.1.0", shell_output("#{bin}/mojave --version")
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
