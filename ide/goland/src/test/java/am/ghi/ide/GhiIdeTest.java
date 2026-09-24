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
        Files.writeString(path.resolve("main.ghi"),"namespace main\nimport fmt \"go:fmt\"\nimport os \"go:os\"\nclass User { public func name() string {return \"Ghi IDE\"}}\nfunc main(){fmt.Println(new User().name(),os.Args[1])}\n");
        var process=GhiCommand.create(compiler,path,"run","\"two words\"").withRedirectErrorStream(true).createProcess();
        try{
            assertTrue("Compiler timed out",process.waitFor(90,TimeUnit.SECONDS));
            String output=new String(process.getInputStream().readAllBytes(),java.nio.charset.StandardCharsets.UTF_8);
            assertEquals(output,0,process.exitValue());assertTrue(output,output.contains("Ghi IDE two words"));
        }finally{process.destroyForcibly();}
    }
    public void testScopedNavigationAndRenameIsolation(){
        var file=myFixture.configureByText("scope.ghi","namespace main\nfunc first(value string) { value = value; { value := \"value\"; value = value }; value = value }\nfunc second(value string) { value = value } // value\n");
        String before=file.getText();int declaration=before.indexOf("value");
        var target=com.intellij.psi.util.PsiTreeUtil.getParentOfType(file.findElementAt(declaration),GhiIdentifier.class,false);
        assertNotNull(target);
        assertSame(target,GhiSymbols.forFile(file).resolve(file,before.indexOf("value = value")));
        myFixture.getEditor().getCaretModel().moveToOffset(before.indexOf("value = value"));
        myFixture.renameElementAtCaret("input");
        assertEquals("namespace main\nfunc first(input string) { input = input; { value := \"value\"; value = value }; input = input }\nfunc second(value string) { value = value } // value\n",file.getText());
    }
    public void testCrossFileInheritedMemberNavigationAndCompletion(){
        var base=myFixture.addFileToProject("base.ghi","namespace main\nclass Base { public title string\n public func describe(prefix string) string { return prefix }\n private secret string\n hidden string\n protected guarded string\n}\n");
        var file=myFixture.configureByText("main.ghi","namespace main\nclass Child extends Base {}\nfunc main(){ item := new Child(); item.describe(\"x\"); item.<caret> }\n");
        var model=GhiSymbols.forFile(file);
        var target=model.resolve(file,file.getText().indexOf("describe"));assertNotNull(target);assertEquals(base,target.getContainingFile());assertEquals("describe",target.getText());
        var variants=myFixture.completeBasic();assertNotNull(variants);
        var names=java.util.Arrays.stream(variants).map(com.intellij.codeInsight.lookup.LookupElement::getLookupString).toList();
        assertTrue(names.toString(),names.contains("describe"));assertTrue(names.contains("title"));assertFalse(names.contains("secret"));assertFalse(names.contains("hidden"));assertFalse(names.contains("guarded"));
    }
    public void testQualifiedMemberAndParameterInfo(){
        var file=myFixture.configureByText("calls.ghi","namespace main\nclass Thing { public func call(name string, count int = 1) string { return name } }\nfunc main(){ value := new Thing(); value.call(\"a\", <caret>2) }\n");
        var model=GhiSymbols.forFile(file);var call=model.callAt(file,myFixture.getCaretOffset());assertNotNull(call);
        assertEquals("call",call.symbol().name);assertEquals(1,call.parameter());assertEquals(java.util.List.of("name string","count int = 1"),call.symbol().parameters);
        assertSame(model.resolve(file,file.getText().indexOf("call(name")),model.resolve(file,file.getText().indexOf("call(\"a\"")));
    }
    public void testNamespaceIsolationAndMemberRename(){
        var other=myFixture.addFileToProject("unrelated.ghi","namespace other\nclass Box { public size int }\nfunc f(){b := new Box(); b.size = 2}\n");
        var file=myFixture.configureByText("members.ghi","namespace main\nclass Box { public size int }\nclass Other { public size int }\nfunc f(){b := new Box(); b.size = 1; c := new Other(); c.size = 3}\n");
        var target=GhiSymbols.forFile(file).resolve(file,file.getText().indexOf("size"));assertNotNull(target);
        new com.intellij.refactoring.rename.RenameProcessor(getProject(),target,"length",false,false).run();
        assertTrue(file.getText(),file.getText().contains("b.length = 1; c := new Other(); c.size = 3"));assertTrue(other.getText().contains("b.size = 2"));
    }
    public void testCompilerDiagnosticsAndStaleDocumentGuard() throws Exception {
        String compiler=System.getenv("GHI_TEST_COMPILER");if(compiler==null||compiler.isBlank())return;
        Path root=Files.createDirectory(diskRoot.resolve("diagnostics"));Path path=root.resolve("main.ghi");
        String source="namespace main\nfunc main(){ value := new Missing() }\n";Files.writeString(path,source);
        var annotator=new GhiExternalAnnotator();var input=new GhiExternalAnnotator.Input(compiler,root,path,source,123);
        var problems=annotator.doAnnotate(input);assertFalse("ghi check must return a source diagnostic",problems.isEmpty());
        assertEquals(1,problems.getFirst().line());assertEquals(123,problems.getFirst().stamp());
        var edited=myFixture.configureByText("edited.ghi",source);var document=myFixture.getEditor().getDocument();
        var holder=(com.intellij.lang.annotation.AnnotationHolder)java.lang.reflect.Proxy.newProxyInstance(getClass().getClassLoader(),new Class[]{com.intellij.lang.annotation.AnnotationHolder.class},(proxy,method,args)->{fail("Stale diagnostics must not create editor annotations");return null;});
        annotator.apply(edited,java.util.List.of(new GhiExternalAnnotator.Problem(1,0,"old",document.getModificationStamp()-1)),holder);
        assertTrue(GhiExternalAnnotator.parse(root.resolve("else.ghi")+":2:3: other",input).isEmpty());
    }    public void testImportedNamespaceAndIncompleteMemberCompletion(){
        var library=myFixture.addFileToProject("users/person.ghi","namespace app.users\nclass Person { public name string }\nfunc create() Person { return new Person() }\n");
        var file=myFixture.configureByText("imports.ghi","namespace main\nimport users \"app.users\"\nfunc main(){ person := new users.Person(); person.<caret>\n");
        var model=GhiSymbols.forFile(file);var target=model.resolve(file,file.getText().indexOf("Person"));assertNotNull(target);assertEquals(library,target.getContainingFile());
        assertEquals("name",model.complete(file,myFixture.getCaretOffset()).getFirst().name);
        file=myFixture.configureByText("implicit.ghi","namespace main\nimport \"app.users\"\nfunc main(){ users.<caret> }\n");
        var names=GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset()).stream().map(symbol->symbol.name).toList();
        assertTrue(names.toString(),names.contains("Person"));assertTrue(names.contains("create"));
    }
    public void testRenameCollisionAndParameterInfoExtension(){
        var file=myFixture.configureByText("collision.ghi","namespace main\nfunc f(first string, second string){ first = second }\n");
        var target=GhiSymbols.forFile(file).resolve(file,file.getText().indexOf("first"));
        var conflicts=new com.intellij.util.containers.MultiMap<com.intellij.psi.PsiElement,String>();
        new GhiRenameProcessor().findExistingNameConflicts(target,"second",conflicts);assertFalse(conflicts.isEmpty());
        Object[] items=new Object[1];var callFile=myFixture.configureByText("hint.ghi","namespace main\nfunc f(name string, count int){}\nfunc main(){ f(\"a\", <caret>2) }\n");
        var context=(com.intellij.lang.parameterInfo.CreateParameterInfoContext)java.lang.reflect.Proxy.newProxyInstance(getClass().getClassLoader(),new Class[]{com.intellij.lang.parameterInfo.CreateParameterInfoContext.class},(proxy,method,args)->switch(method.getName()){
            case "getFile" -> callFile;
            case "getOffset" -> myFixture.getCaretOffset();
            case "setItemsToShow" -> {items[0]=args[0];yield null;}
            default -> null;
        });
        assertNotNull(new GhiParameterInfo().findElementForParameterInfo(context));
        assertEquals(java.util.List.of("name string","count int"),((GhiSymbols.Symbol)((Object[])items[0])[0]).parameters);
    }    public void testExplicitGenericConstructionHintsAndNavigation(){
        var library=myFixture.addFileToProject("models/box.ghi","namespace app.models\nclass Box[T any] { public value T\n constructor(value T, count int = 1){ this.value=value }\n}\nfunc build() {}\ninterface Reader {}\n");
        var file=myFixture.configureByText("construct.ghi","namespace main\nimport models \"app.models\"\nfunc main(){ value := new models.Box[string](\"hello\", <caret>2); value.value = \"world\" }\n");
        var model=GhiSymbols.forFile(file);var call=model.callAt(file,myFixture.getCaretOffset());assertNotNull(call);
        assertEquals("constructor",call.symbol().name);assertEquals(1,call.parameter());assertEquals(java.util.List.of("value T","count int = 1"),call.symbol().parameters);
        var target=model.resolve(file,file.getText().indexOf("Box"));assertNotNull(target);assertEquals(library,target.getContainingFile());
        assertEquals("value",model.resolve(file,file.getText().indexOf("value.value")+6).getText());
        file=myFixture.configureByText("construct-complete.ghi","namespace main\nimport models \"app.models\"\nfunc main(){ value := new models.<caret> }\n");
        var names=GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset()).stream().map(symbol->symbol.name).toList();assertEquals(java.util.List.of("Box"),names);
        file=myFixture.configureByText("construct-direct.ghi","namespace main\nclass Box[T any] {public value T}\nfunc main(){new Box[string]().<caret>}\n");
        assertEquals("value",GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset()).getFirst().name);        file=myFixture.configureByText("bare-generic.ghi","namespace main\nimport models \"app.models\"\nfunc main(){value := models.Box[string](\"hello\", <caret>2)}\n");
        var bareCall=GhiSymbols.forFile(file).callAt(file,myFixture.getCaretOffset());assertNotNull(bareCall);assertEquals(call.symbol().parameters,bareCall.symbol().parameters);
        assertEquals(library,GhiSymbols.forFile(file).resolve(file,file.getText().indexOf("Box")).getContainingFile());
    }
    public void testNewExceptionHintsAndConstructorOnlyCompletion(){
        var file=myFixture.configureByText("throw.ghi","namespace main\nfunc main(){throw new Exception(\"failure\", <caret>2)}\n");
        var call=GhiSymbols.forFile(file).callAt(file,myFixture.getCaretOffset());assertNotNull(call);assertEquals(java.util.List.of("message string = \"\"","code int = 0"),call.symbol().parameters);
        file=myFixture.configureByText("new-complete.ghi","namespace main\nclass User {}\ninterface Reader {}\nfunc build() {}\nfunc main(){new <caret>}\n");
        var names=GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset()).stream().map(symbol->symbol.name).toList();
        assertTrue(names.toString(),names.contains("User"));assertTrue(names.contains("Exception"));assertFalse(names.contains("Reader"));assertFalse(names.contains("build"));
        file=myFixture.configureByText("bare-construction.ghi","namespace main\nclass User {constructor(name string){}}\nfunc main(){User(<caret>)}\n");
        var bare=GhiSymbols.forFile(file).callAt(file,myFixture.getCaretOffset());assertNotNull(bare);assertEquals(java.util.List.of("name string"),bare.symbol().parameters);        file=myFixture.configureByText("bare-exception.ghi","namespace main\nfunc main(){throw Exception(\"failure\", <caret>2)}\n");
        assertEquals(call.symbol().parameters,GhiSymbols.forFile(file).callAt(file,myFixture.getCaretOffset()).symbol().parameters);
    }}
