package am.ghi.ide;
import com.intellij.testFramework.fixtures.BasePlatformTestCase;
import com.intellij.openapi.actionSystem.ActionManager;
import com.intellij.lang.LanguageCommenters;
import java.nio.file.*;
import java.util.concurrent.TimeUnit;
public class GhiIdeTest extends BasePlatformTestCase {
    private Path diskRoot;
    protected void setUp() throws Exception {super.setUp();diskRoot=Files.createTempDirectory("ghi-ide-test-");}
    protected void tearDown() throws Exception {
        try { if(diskRoot!=null) {try(var files=Files.walk(diskRoot)){for(Path file:files.sorted(java.util.Comparator.reverseOrder()).toList())Files.deleteIfExists(file);}} }
        finally {super.tearDown();}
    }
    public void testFileRecognitionAndServices(){
        var file=myFixture.configureByText("main.ghi","namespace main\nfunc main(){}\n");
        assertSame(GhiFileType.INSTANCE,file.getFileType());
        assertSame(GhiLanguage.INSTANCE,file.getLanguage());
        assertEquals("//",LanguageCommenters.INSTANCE.forLanguage(GhiLanguage.INSTANCE).getLineCommentPrefix());
        assertNotNull(ActionManager.getInstance().getAction("Ghi.Build"));
        assertNotNull(ActionManager.getInstance().getAction("Ghi.Run"));
        var settings=GhiSettings.get(getProject());settings.getState().arguments="one";
        assertEquals("one",GhiSettings.get(getProject()).getState().arguments);
    }
    public void testConsoleSourceLink() throws Exception {
        Path source=diskRoot.resolve("main.ghi");Files.writeString(source,"namespace main\nfunc main(){}\n");
        var file=com.intellij.openapi.vfs.LocalFileSystem.getInstance().refreshAndFindFileByNioFile(source);
        assertNotNull(file);
        String text=file.getPath()+":2:6: error\n";
        var filter=new GhiConsoleFilter(getProject(),Path.of(getProject().getBasePath()));
        var result=filter.applyFilter(text,text.length()+10);
        assertNotNull(result);assertEquals(10,result.getHighlightStartOffset());assertNotNull(result.getHyperlinkInfo());
    }
    public void testRealCompilerCommand() throws Exception {
        String compiler=System.getenv("GHI_TEST_COMPILER");
        if(compiler==null || compiler.isBlank())return;
        Path path=Files.createDirectory(diskRoot.resolve("real compiler project"));
        Files.writeString(path.resolve("main.ghi"),"namespace main\nimport fmt \"go:fmt\"\nimport os \"go:os\"\nclass User { public func name() string {return \"Ghi IDE\"}}\nfunc main(){fmt.Println(User().name(),os.Args[1])}\n");
        var process=GhiCommand.create(compiler,path,"run","\"two words\"").withRedirectErrorStream(true).createProcess();
        try{
            assertTrue("Compiler timed out",process.waitFor(90,TimeUnit.SECONDS));
            String output=new String(process.getInputStream().readAllBytes(),java.nio.charset.StandardCharsets.UTF_8);
            assertEquals(output,0,process.exitValue());assertTrue(output,output.contains("Ghi IDE two words"));
        }finally{process.destroyForcibly();}
    }
}
