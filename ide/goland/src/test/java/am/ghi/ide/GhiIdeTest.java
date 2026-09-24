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
        var file=myFixture.configureByText("imports.ghi","namespace main\nimport app.users as users\nfunc main(){ person := new users.Person(); person.<caret>\n");
        var model=GhiSymbols.forFile(file);var target=model.resolve(file,file.getText().indexOf("Person"));assertNotNull(target);assertEquals(library,target.getContainingFile());
        assertEquals("name",model.complete(file,myFixture.getCaretOffset()).getFirst().name);
        file=myFixture.configureByText("implicit.ghi","namespace main\nimport app.users\nfunc main(){ users.<caret> }\n");
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
        var file=myFixture.configureByText("construct.ghi","namespace main\nimport app.models as models\nfunc main(){ value := new models.Box[string](\"hello\", <caret>2); value.value = \"world\" }\n");
        var model=GhiSymbols.forFile(file);var call=model.callAt(file,myFixture.getCaretOffset());assertNotNull(call);
        assertEquals("constructor",call.symbol().name);assertEquals(1,call.parameter());assertEquals(java.util.List.of("value string","count int = 1"),call.symbol().parameters);
        var target=model.resolve(file,file.getText().indexOf("Box"));assertNotNull(target);assertEquals(library,target.getContainingFile());
        assertEquals("value",model.resolve(file,file.getText().indexOf("value.value")+6).getText());
        file=myFixture.configureByText("construct-complete.ghi","namespace main\nimport app.models as models\nfunc main(){ value := new models.<caret> }\n");
        var names=GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset()).stream().map(symbol->symbol.name).toList();assertEquals(java.util.List.of("Box"),names);
        file=myFixture.configureByText("construct-direct.ghi","namespace main\nclass Box[T any] {public value T}\nfunc main(){new Box[string]().<caret>}\n");
        assertEquals("value",GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset()).getFirst().name);        file=myFixture.configureByText("bare-generic.ghi","namespace main\nimport app.models as models\nfunc main(){value := models.Box[string](\"hello\", <caret>2)}\n");
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
    }
    public void testGenericFunctionAndReceiverHintSubstitution(){
        var file=myFixture.configureByText("generic-hints.ghi","namespace main\nclass Box[T any] {public func put(value []T, label string = \"T\") {}}\nfunc pair[K comparable,V any](key K,value map[K][]V) {}\nfunc main(){pair[string,int](\"key\", <caret>nil)}\n");
        var call=GhiSymbols.forFile(file).callAt(file,myFixture.getCaretOffset());assertNotNull(call);
        assertEquals(java.util.List.of("key string","value map[string][]int"),call.symbol().parameters);
        file=myFixture.configureByText("generic-member.ghi","namespace main\nclass Box[T any] {public func put(value []T, label string = \"T\") {}}\nfunc main(){box:=new Box[string]();box.put(<caret>nil)}\n");
        call=GhiSymbols.forFile(file).callAt(file,myFixture.getCaretOffset());assertNotNull(call);
        assertEquals(java.util.List.of("value []string","label string = \"T\""),call.symbol().parameters);
    }
    public void testCoordinatedContractRenamePreservesUnrelatedMethods(){
        var contract=myFixture.addFileToProject("contract.ghi","namespace main\ninterface Reader {func read() string}\nclass Base {public func read() string{return \"read\"}}\nclass Child extends Base implements Reader {}\nclass Other {public func read() int{return 1}}\n");
        var file=myFixture.configureByText("rename-family.ghi","namespace main\nclass Override extends Child {public override func read() string{return parent.read()}}\nfunc use(value Reader, other Other){value.<caret>read();other.read()} // read\n");
        myFixture.renameElementAtCaret("fetch");
        assertTrue(contract.getText(),contract.getText().contains("interface Reader {func fetch()"));
        assertTrue(contract.getText(),contract.getText().contains("class Base {public func fetch()"));
        assertTrue(contract.getText(),contract.getText().contains("class Other {public func read()"));
        assertTrue(contract.getText().contains("return \"read\""));
        assertTrue(file.getText(),file.getText().contains("override func fetch()"));
        assertTrue(file.getText().contains("parent.fetch()"));
        assertTrue(file.getText().contains("value.fetch();other.read()"));assertTrue(file.getText().endsWith("// read\n"));
    }
    public void testNativeGoSdkSymbolsAndNavigation(){
        String root=System.getenv("GHI_TEST_GO_ROOT");assertNotNull("Set GHI_TEST_GO_ROOT to exercise real Go SDK PSI",root);
        com.intellij.openapi.vfs.newvfs.impl.VfsRootAccess.allowRootAccess(getTestRootDisposable(),root);
        var sdk=com.goide.sdk.GoSdkService.getInstance(getProject());sdk.setSdk(com.goide.sdk.GoSdk.fromHomePath(root));
        var file=myFixture.configureByText("native.ghi","namespace main\nimport fmt \"go:fmt\"\nimport bytes \"go:bytes\"\nfunc main(){fmt.Println(\"hello\");var buffer bytes.Buffer;buffer.<caret>}\n");
        var model=GhiSymbols.forFile(file);var target=model.resolve(file,file.getText().indexOf("Println"));assertNotNull("native function navigation",target);
        assertTrue(target instanceof com.goide.psi.GoFunctionDeclaration);assertTrue(target.getContainingFile().getVirtualFile().getPath().endsWith("/fmt/print.go"));
        var names=model.complete(file,myFixture.getCaretOffset()).stream().map(symbol->symbol.name).toList();assertTrue(names.toString(),names.contains("WriteString"));assertFalse(names.contains("grow"));
        file=myFixture.configureByText("native-method.ghi","namespace main\nimport bytes \"go:bytes\"\nfunc main(){buffer:=bytes.NewBufferString(\"x\");buffer.WriteString(<caret>\"y\")}\n");
        model=GhiSymbols.forFile(file);target=model.resolve(file,file.getText().indexOf("WriteString"));assertNotNull(target);assertTrue(target instanceof com.goide.psi.GoMethodDeclaration);
        assertEquals(java.util.List.of("s string"),model.callAt(file,myFixture.getCaretOffset()).symbol().parameters);
        assertTrue(GhiGoSymbols.importPaths(file,"go:net/h").contains("go:net/http"));
        file=myFixture.configureByText("native-import.ghi","namespace main\nimport \"go:net/h<caret>\"\n");
        var variants=myFixture.completeBasic();
        if(variants!=null){var item=java.util.Arrays.stream(variants).filter(candidate->candidate.getLookupString().equals("go:net/http")).findFirst().orElseThrow();myFixture.getLookup().setCurrentItem(item);myFixture.finishLookup(com.intellij.codeInsight.lookup.Lookup.NORMAL_SELECT_CHAR);}
        assertEquals("namespace main\nimport \"go:net/http\"\n",file.getText());
    }

    public void testPinnedMojaveGoModuleWithoutGoMod(){
        String root=System.getenv("GHI_TEST_GO_ROOT"),cache=System.getenv("GOMODCACHE");assertNotNull(root);assertNotNull(cache);
        com.intellij.openapi.vfs.newvfs.impl.VfsRootAccess.allowRootAccess(getTestRootDisposable(),root,cache);
        com.goide.sdk.GoSdkService.getInstance(getProject()).setSdk(com.goide.sdk.GoSdk.fromHomePath(root));
        var lock=myFixture.addFileToProject("mojave.lock","{\"version\":1,\"go\":{\"modules\":[{\"path\":\"github.com/go-chi/chi/v5\",\"version\":\"v5.3.2\"}]}}");
        var file=myFixture.configureByText("chi.ghi","namespace main\nimport chi \"go:github.com/go-chi/chi/v5\"\nfunc main(){router:=chi.NewRouter();router.<caret>}\n");
        assertNull(file.getContainingDirectory().findFile("go.mod"));
        assertTrue(GhiGoSymbols.importPaths(file,"go:github.com/go-chi/chi/v5/m").contains("go:github.com/go-chi/chi/v5/middleware"));
        var target=GhiSymbols.forFile(file).resolve(file,file.getText().indexOf("NewRouter"));assertNotNull("locked Chi function",target);
        assertTrue(target.getContainingFile().getVirtualFile().getPath(),target.getContainingFile().getVirtualFile().getPath().contains("chi/v5@v5.3.2/"));
        var names=GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset()).stream().map(symbol->symbol.name).toList();assertTrue(names.toString(),names.contains("Get"));assertTrue(names.contains("Route"));
        com.intellij.openapi.command.WriteCommandAction.runWriteCommandAction(getProject(),()->{
            var document=com.intellij.psi.PsiDocumentManager.getInstance(getProject()).getDocument(lock);document.setText("{\"version\":1,\"go\":{\"modules\":[{\"path\":\"github.com/go-chi/chi/v5\",\"version\":\"v99.0.0\"}]}}");com.intellij.psi.PsiDocumentManager.getInstance(getProject()).commitDocument(document);com.intellij.openapi.fileEditor.FileDocumentManager.getInstance().saveDocument(document);
        });
        assertNull("missing pinned version must not select another cache version",GhiSymbols.forFile(file).resolve(file,file.getText().indexOf("NewRouter")));
    }

    public void testStructuralInterfaceRenameUpdatesImplementation() throws Exception {
        var file=myFixture.configureByText("structural.ghi","namespace main\ninterface Reader {func read() string}\nclass File {public func read() string{return \"x\"}}\nclass Different {public func read() int{return 1}}\nfunc use(r Reader){r.<caret>read()}\nfunc main(){use(new File());new File().read();new Different().read()}\n");
        myFixture.renameElementAtCaret("fetch");
        assertTrue(file.getText(),file.getText().contains("class File {public func fetch()"));
        assertTrue(file.getText().contains("interface Reader {func fetch()"));assertTrue(file.getText().contains("r.fetch()"));
        assertTrue(file.getText().contains("new File().fetch()"));assertTrue(file.getText().contains("new Different().read()"));
        String compiler=System.getenv("GHI_TEST_COMPILER");if(compiler!=null&&!compiler.isBlank()){
            Path project=Files.createDirectory(diskRoot.resolve("renamed-structural"));Files.writeString(project.resolve("main.ghi"),file.getText());
            var process=GhiCommand.create(compiler,project,"check","").withRedirectErrorStream(true).createProcess();
            try{assertTrue(process.waitFor(30,TimeUnit.SECONDS));String output=new String(process.getInputStream().readAllBytes(),java.nio.charset.StandardCharsets.UTF_8);assertEquals(output,0,process.exitValue());}finally{process.destroyForcibly();}
        }
    }

    public void testGenericStructuralRenameReportsConflict(){
        var file=myFixture.configureByText("generic-contract.ghi","namespace main\ninterface Reader[T any] {func read() T}\nclass File {public func read() string{return \"x\"}}\n");
        var target=GhiSymbols.forFile(file).resolve(file,file.getText().indexOf("read"));
        var conflicts=new com.intellij.util.containers.MultiMap<com.intellij.psi.PsiElement,String>();
        new GhiRenameProcessor().findExistingNameConflicts(target,"fetch",conflicts);assertFalse(conflicts.isEmpty());
        assertTrue(file.getText().contains("func read()"));
    }

    public void testStructuralSliceSpellingAndScalarIsolation(){
        var file=myFixture.configureByText("slice-contract.ghi","namespace main\ninterface Reader {func read(value[] string) string}\nclass Sliced {public func read(value []string) string{return \"slice\"}}\nclass Scalar {public func read(value string) string{return value}}\nfunc use(reader Reader){reader.<caret>read(nil)}\nfunc main(){use(new Sliced());new Sliced().read(nil);new Scalar().read(\"x\")}\n");
        myFixture.renameElementAtCaret("fetch");
        assertTrue(file.getText(),file.getText().contains("interface Reader {func fetch(value[] string)"));
        assertTrue(file.getText().contains("class Sliced {public func fetch(value []string)"));
        assertTrue(file.getText().contains("new Sliced().fetch(nil)"));assertTrue(file.getText().contains("new Scalar().read(\"x\")"));
    }
    public void testNamedResultStructuralRenameReportsConflict(){
        var file=myFixture.configureByText("named-result.ghi","namespace main\ninterface Reader {func read() string}\nclass File {public func read() (result string){return \"x\"}}\n");
        var target=GhiSymbols.forFile(file).resolve(file,file.getText().indexOf("read"));
        var conflicts=new com.intellij.util.containers.MultiMap<com.intellij.psi.PsiElement,String>();new GhiRenameProcessor().findExistingNameConflicts(target,"fetch",conflicts);assertFalse(conflicts.isEmpty());
    }
    public void testGenericClassStructuralRenameReportsConflict(){
        var file=myFixture.configureByText("generic-implementation.ghi","namespace main\ninterface Reader {func read() string}\nclass Box[T any] {public func read() T{panic(\"unused\")}}\nfunc use(reader Reader){reader.read()}\nfunc main(){use(new Box[string]())}\n");
        var target=GhiSymbols.forFile(file).resolve(file,file.getText().indexOf("read"));
        var conflicts=new com.intellij.util.containers.MultiMap<com.intellij.psi.PsiElement,String>();new GhiRenameProcessor().findExistingNameConflicts(target,"fetch",conflicts);assertFalse(conflicts.isEmpty());
    }
    public void testSelectedTypeImportBindingHintsAndFileIsolation(){
        var library=myFixture.addFileToProject("migrations/types.ghi","namespace migrations\nclass Migrator[T any] {public value T\nconstructor(value T){this.value=value}}\ninterface Runner {func run()}\ntype Version int\nfunc helper(){}\nclass Hidden {}\n");
        var other=myFixture.addFileToProject("other.ghi","namespace main\nfunc other(){Migrator[string](\"x\")}\n");
        var file=myFixture.configureByText("selected.ghi","namespace main\nimport migrations.Migrator\nimport migrations.Runner\nimport migrations.Version\nfunc use(r ?Runner, v Version){var m ?Migrator[string];m.value}\nfunc main(){new Migrator[string](<caret>\"x\")}\n");
        var model=GhiSymbols.forFile(file);
        for(String name:java.util.List.of("Migrator","Runner","Version")){
            assertEquals(library,model.resolve(file,file.getText().indexOf(name)).getContainingFile());
            assertEquals(library,model.resolve(file,file.getText().lastIndexOf(name)).getContainingFile());
        }
        assertNull(model.resolve(file,file.getText().indexOf("migrations")));
        assertNull(model.resolve(other,other.getText().indexOf("Migrator")));
        assertEquals(java.util.List.of("value string"),model.callAt(file,myFixture.getCaretOffset()).symbol().parameters);
        assertEquals("value",model.resolve(file,file.getText().indexOf("m.value")+2).getText());
        var names=model.complete(file,file.getText().indexOf("new Migrator")).stream().map(symbol->symbol.name).toList();
        assertTrue(names.toString(),names.containsAll(java.util.List.of("Migrator","Runner","Version")));assertFalse(names.contains("Hidden"));assertFalse(names.contains("helper"));
        file=myFixture.configureByText("bare-selected.ghi","namespace main\nimport migrations.Migrator\nfunc main(){Migrator[int](<caret>1)}\n");
        assertEquals(java.util.List.of("value int"),GhiSymbols.forFile(file).callAt(file,myFixture.getCaretOffset()).symbol().parameters);
    }
    public void testSelectedImportCompletionAndRename(){
        var library=myFixture.addFileToProject("migrations/type.ghi","namespace migrations\nclass Migrator {}\ninterface Runner {}\ntype Version int\nfunc helper(){}\n");
        var unrelated=myFixture.addFileToProject("other/type.ghi","namespace other\nclass Migrator {}\n");
        var file=myFixture.configureByText("import-complete.ghi","namespace main\nimport migrations.Mig<caret>\n");
        myFixture.completeBasic();assertEquals("namespace main\nimport migrations.Migrator\n",file.getText());
        file=myFixture.configureByText("selected-rename.ghi","namespace main\nimport migrations.Migrator\nfunc main(){new <caret>Migrator();Migrator()} // Migrator\nconst label = \"Migrator\"\n");
        myFixture.renameElementAtCaret("Executor");
        assertTrue(library.getText().contains("class Executor"));assertTrue(unrelated.getText().contains("class Migrator"));
        assertTrue(file.getText(),file.getText().contains("import migrations.Executor"));assertTrue(file.getText().contains("new Executor();Executor()"));
        assertTrue(file.getText().contains("// Migrator"));assertTrue(file.getText().contains("\"Migrator\""));
    }
    public void testSelectedImportAmbiguityAndRenameCapture(){
        var library=myFixture.addFileToProject("one.ghi","namespace one\nclass Item {}\n");
        myFixture.addFileToProject("two.ghi","namespace two\nclass Item {}\n");
        var file=myFixture.configureByText("ambiguous.ghi","namespace main\nimport one.Item\nimport two.Item\nfunc main(){new Item()}\n");
        var model=GhiSymbols.forFile(file);assertNull(model.resolve(file,file.getText().lastIndexOf("Item")));
        assertFalse(model.complete(file,file.getText().lastIndexOf("Item")).stream().anyMatch(symbol->symbol.name.equals("Item")));
        var target=model.resolve(library,library.getText().indexOf("Item"));
        var conflicts=new com.intellij.util.containers.MultiMap<com.intellij.psi.PsiElement,String>();new GhiRenameProcessor().findExistingNameConflicts(target,"Renamed",conflicts);assertFalse(conflicts.isEmpty());
        file=myFixture.configureByText("capture.ghi","namespace app\nimport one.Item\nclass Taken {}\nfunc main(){new Item()}\n");
        target=GhiSymbols.forFile(file).resolve(file,file.getText().lastIndexOf("Item"));conflicts.clear();new GhiRenameProcessor().findExistingNameConflicts(target,"Taken",conflicts);assertFalse(conflicts.isEmpty());
    }

    public void testSelectedImportAliasRenameIsLocal() throws Exception {
        var library=myFixture.addFileToProject("domain.ghi","namespace domain\nclass User {constructor(name string){}}\n");
        var file=myFixture.configureByText("alias.ghi","namespace main\nimport domain.User as Account\nfunc main(){new <caret>Account(\"x\")}\n");
        var model=GhiSymbols.forFile(file);assertEquals("Account",model.resolve(file,file.getText().lastIndexOf("Account")).getText());
        assertEquals(library,model.resolve(file,file.getText().indexOf("User")).getContainingFile());
        assertEquals(java.util.List.of("name string"),model.callAt(file,file.getText().indexOf("\"x\"")).symbol().parameters);
        myFixture.renameElementAtCaret("Member");assertTrue(file.getText(),file.getText().contains("import domain.User as Member"));assertTrue(file.getText().contains("new Member("));assertTrue(library.getText().contains("class User"));
        myFixture.getEditor().getCaretModel().moveToOffset(file.getText().indexOf("User"));myFixture.renameElementAtCaret("Person");
        assertTrue(library.getText().contains("class Person"));assertTrue(file.getText(),file.getText().contains("import domain.Person as Member"));assertTrue(file.getText().contains("new Member("));
        checkImportedProject("alias-renamed",file.getText(),library.getText());
    }
    public void testExplicitImportActionsAndCollisionAlias() throws Exception {
        myFixture.addFileToProject("users.ghi","namespace app.users\nclass User {}\n");
        var file=myFixture.configureByText("auto.ghi","namespace main\nfunc main(){new Us<caret>er()}\n");
        var action=new GhiImportIntention.Import();assertTrue(action.isAvailable(getProject(),myFixture.getEditor(),file));action.invoke(getProject(),myFixture.getEditor(),file);
        assertEquals("namespace main\nimport app.users.User\nfunc main(){new User()}\n",file.getText());
        file=myFixture.configureByText("shorten.ghi","namespace main\nclass User {}\nfunc main(){new app.users.Us<caret>er()}\n");
        var shorten=new GhiImportIntention.Shorten();assertTrue(shorten.isAvailable(getProject(),myFixture.getEditor(),file));shorten.invoke(getProject(),myFixture.getEditor(),file);
        assertEquals("namespace main\nimport app.users.User as UsersUser\nclass User {}\nfunc main(){new UsersUser()}\n",file.getText());
        assertNotNull(GhiSymbols.forFile(file).resolve(file,file.getText().lastIndexOf("UsersUser")));
        checkImportedProject("shortened-import",file.getText(),"namespace app.users\nclass User {}\n");
    }
    public void testCompletionAutoImportAndMultipleCandidates(){
        myFixture.addFileToProject("one/user.ghi","namespace one\nclass User {}\n");
        myFixture.addFileToProject("two/user.ghi","namespace two\nclass User {}\n");
        var file=myFixture.configureByText("auto-complete.ghi","namespace main\nfunc main(){new Us<caret>}\n");
        var variants=myFixture.completeBasic();assertNotNull(variants);
        var choices=java.util.Arrays.stream(variants).filter(item->item.getObject() instanceof GhiTypeImports.Choice).toList();assertEquals(2,choices.size());
        var item=choices.stream().filter(candidate->((GhiTypeImports.Choice)candidate.getObject()).path().equals("two.User")).findFirst().orElseThrow();
        myFixture.getLookup().setCurrentItem(item);myFixture.finishLookup(com.intellij.codeInsight.lookup.Lookup.NORMAL_SELECT_CHAR);
        assertEquals("namespace main\nimport two.User\nfunc main(){new User}\n",file.getText());
    }
    public void testImportActionAvoidsShadowedAliasAndTypeParameter() throws Exception {
        myFixture.addFileToProject("users.ghi","namespace app.users\nclass User {}\n");
        var file=myFixture.configureByText("shadowed-import.ghi","namespace main\nimport app.users.User\nfunc use[UsersUser any](){User:=1;_ = User;new app.users.Us<caret>er()}\nfunc main(){new User();use[int]()}\n");
        var action=new GhiImportIntention.Shorten();assertTrue(action.isAvailable(getProject(),myFixture.getEditor(),file));action.invoke(getProject(),myFixture.getEditor(),file);
        assertTrue(file.getText(),file.getText().contains("import app.users.User as UsersUser2"));
        assertTrue(file.getText().contains("_ = User;new UsersUser2()"));assertTrue(file.getText().contains("new User();use[int]()"));
        checkImportedProject("shadowed-import",file.getText(),"namespace app.users\nclass User {}\n");
    }
    public void testImportedTypeRenameRejectsTypeParameterCapture(){
        myFixture.addFileToProject("user.ghi","namespace domain\nclass User {}\n");
        var file=myFixture.configureByText("capture-type-parameter.ghi","namespace main\nimport domain.User\nfunc use[T any](){new User()}\n");
        var target=GhiSymbols.forFile(file).resolve(file,file.getText().lastIndexOf("User"));
        var conflicts=new com.intellij.util.containers.MultiMap<com.intellij.psi.PsiElement,String>();new GhiRenameProcessor().findExistingNameConflicts(target,"T",conflicts);assertFalse(conflicts.isEmpty());
    }
    public void testImportInsertionPreservesCommentsAndRawStrings(){
        myFixture.addFileToProject("user.ghi","namespace users\nclass User {}\n");
        var file=myFixture.configureByText("literal.ghi","namespace main // heading\nconst sample = `first\nimport fake.Trap\nlast`\nfunc main(){new users.Us<caret>er()}\n");
        var action=new GhiImportIntention.Shorten();assertTrue(action.isAvailable(getProject(),myFixture.getEditor(),file));action.invoke(getProject(),myFixture.getEditor(),file);
        assertEquals("namespace main // heading\nimport users.User\nconst sample = `first\nimport fake.Trap\nlast`\nfunc main(){new User()}\n",file.getText());
    }
    public void testSelectedImportsDoNotLeakIntoMemberCompletion(){
        myFixture.addFileToProject("models.ghi","namespace models\nclass Box {public value string}\ninterface Reader {}\n");
        var file=myFixture.configureByText("member-imports.ghi","namespace main\nimport models.Box\nimport models.Reader as Contract\nfunc main(){box:=new Box();box.<caret>}\n");
        var names=GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset()).stream().map(symbol->symbol.name).toList();
        assertEquals(java.util.List.of("value"),names);
        var variants=myFixture.completeBasic();
        if(variants!=null)for(var item:variants){assertFalse(item.getLookupString().equals("Box"));assertFalse(item.getLookupString().equals("Contract"));}
    }
    public void testUnifiedNamespaceAndSelectedTypeImports(){
        var http=myFixture.addFileToProject("presentation/httpapi/api.ghi","namespace presentation.httpapi\n"
            +"class Handler {constructor(port int){}}\nfunc serve(address string){}\n");
        var config=myFixture.addFileToProject("infrastructure/config/config.ghi","namespace infrastructure.config\n"
            +"class Settings {}\nfunc load() Settings{return new Settings()}\n");
        var models=myFixture.addFileToProject("models/model.ghi","namespace models\nclass Record {}\n");
        var service=myFixture.addFileToProject("application/tasks/service.ghi","namespace application.tasks\n"
            +"class Service {constructor(name string){}}\n");
        var file=myFixture.configureByText("unified-imports.ghi","namespace main\n"
            +"import presentation.httpapi\nimport infrastructure.config as cfg\n"
            +"import models\nimport application.tasks.Service as TaskService\n"
            +"func main(){httpapi.serve(\"x\");new httpapi.Handler(80);cfg.load();new models.Record();new TaskService(\"job\")}\n");
        var model=GhiSymbols.forFile(file);
        assertEquals(http,model.resolve(file,file.getText().indexOf("httpapi.serve")+8).getContainingFile());
        assertEquals(config,model.resolve(file,file.getText().indexOf("cfg.load")+4).getContainingFile());
        assertEquals(models,model.resolve(file,file.getText().indexOf("models.Record")+7).getContainingFile());
        assertEquals(service,model.resolve(file,file.getText().indexOf("Service as")).getContainingFile());
        assertEquals(java.util.List.of("address string"),model.callAt(file,file.getText().indexOf("\"x\"")).symbol().parameters);
        assertEquals(java.util.List.of("port int"),model.callAt(file,file.getText().indexOf("80)")).symbol().parameters);
        assertEquals(java.util.List.of("name string"),model.callAt(file,file.getText().indexOf("\"job\"")).symbol().parameters);
        assertEquals(http,model.complete(file,file.getText().indexOf("httpapi.serve")+8).stream()
            .filter(symbol->symbol.name.equals("serve")).findFirst().orElseThrow().file);
        file=myFixture.configureByText("namespace-completion.ghi","namespace main\nimport presentation.htt<caret>\n");
        assertTrue(GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset()).stream()
            .anyMatch(symbol->symbol.name.equals("httpapi")&&symbol.kind.equals("namespace")));
        file=myFixture.configureByText("selected-completion.ghi","namespace main\nimport application.tasks.Ser<caret>\n");
        assertTrue(GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset()).stream()
            .anyMatch(symbol->symbol.name.equals("Service")&&symbol.kind.equals("class")));
        file=myFixture.configureByText("unified-rename.ghi","namespace main\n"
            +"import presentation.httpapi\nimport infrastructure.config as cfg\n"
            +"import models\nimport application.tasks.Service as TaskService\n"
            +"func main(){httpapi.serve(\"x\");new httpapi.Handler(80);cfg.load();new models.Record();new TaskService(\"job\")}\n");
        myFixture.getEditor().getCaretModel().moveToOffset(file.getText().indexOf("cfg.load"));
        myFixture.renameElementAtCaret("settings");
        assertTrue(file.getText(),file.getText().contains("import infrastructure.config as settings"));
        assertTrue(file.getText(),file.getText().contains("settings.load()"));
        assertTrue(config.getText().contains("namespace infrastructure.config"));
    }
    public void testUnifiedImportAmbiguousPathHasNoBinding(){
        myFixture.addFileToProject("task/type.ghi","namespace application.tasks\nclass Service {}\n");
        myFixture.addFileToProject("task/namespace.ghi","namespace application.tasks.Service\nclass Runner {}\n");
        var file=myFixture.configureByText("ambiguous-unified.ghi","namespace main\nimport application.tasks.Service\n"
            +"func main(){new Service()}\n");
        var model=GhiSymbols.forFile(file);
        assertNull(model.resolve(file,file.getText().indexOf("Service\n")));
        assertNull(model.resolve(file,file.getText().lastIndexOf("Service")));
        assertFalse(model.complete(file,file.getText().lastIndexOf("Service")).stream()
            .anyMatch(symbol->symbol.name.equals("Service")));
        file=myFixture.configureByText("ambiguous-completion.ghi","namespace main\nimport application.tasks.Ser<caret>\n");
        assertFalse(GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset()).stream()
            .anyMatch(symbol->symbol.name.equals("Service")));
    }
    public void testLegacyQuotedGhiImportDoesNotBind(){
        myFixture.addFileToProject("legacy/library.ghi","namespace legacy.library\nclass Item {}\n");
        var file=myFixture.configureByText("legacy-import.ghi","namespace main\nimport lib \"legacy.library\"\n"
            +"func main(){new lib.Item()}\n");
        assertNull(GhiSymbols.forFile(file).resolve(file,file.getText().indexOf("lib.Item")+4));
    }
    public void testReformatUsesCompilerForUnsavedBuffer() throws Exception {
        String source="namespace main\nfunc main(){println(\"format me\")}\n";
        var file=myFixture.configureByText("unsaved-format.ghi",source);
        assertNotNull(com.intellij.formatting.service.FormattingServiceUtil.findService(GhiFormattingService.class));
        assertTrue(com.intellij.formatting.service.FormattingServiceUtil.findService(file,false,true) instanceof GhiFormattingService);
        String compiler=System.getenv("GHI_TEST_COMPILER");if(compiler==null||compiler.isBlank())return;
        Path missing=diskRoot.resolve("unsaved-format.ghi");assertFalse(Files.exists(missing));
        String formatted=GhiFormattingService.format(compiler,source,missing.toString(),new java.util.concurrent.atomic.AtomicReference<>());
        assertTrue(formatted,formatted.contains("func main() {\n\tprintln(\"format me\")\n}"));
        assertFalse(source.equals(formatted));
        assertFalse(Files.exists(missing));
        var settings=GhiSettings.get(getProject()).getState();String previous=settings.executable;settings.executable=compiler;
        try{
            myFixture.performEditorAction("ReformatCode");
            long deadline=System.nanoTime()+java.util.concurrent.TimeUnit.SECONDS.toNanos(5);
            while(source.equals(myFixture.getEditor().getDocument().getText())&&System.nanoTime()<deadline){
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();Thread.sleep(20);
            }
            assertEquals(formatted,myFixture.getEditor().getDocument().getText());
            myFixture.performEditorAction(com.intellij.openapi.actionSystem.IdeActions.ACTION_UNDO);
            assertEquals(source,myFixture.getEditor().getDocument().getText());
        }finally{settings.executable=previous;}
    }
    public void testAnonymousArrowScopesCapturesAndCalls(){
        String source="namespace main\nclass Counter {\n public value int\n public func make(seed int) func(int) int {\n  return (n int) int => { return this.value + seed + n }\n }\n}\nfunc main(){\n outer := (a int) func(int) int => {\n  inner := (b int) int => { return a+b }\n  return inner\n }\n sum := (a int, b int) int => { return a+b }\n pair := (a int) (int,error) => { return a,nil }\n named := (x int) (result int, err error) => { result=x; return }\n empty := () => { }\n sum(1,2); pair(1); named(2); empty(); outer(2)\n}\n";
        var file=myFixture.configureByText("arrows.ghi",source);
        var model=GhiSymbols.forFile(file);
        int outerName=source.indexOf("(a int) func")+1;
        int innerName=source.indexOf("(b int) int")+1;
        int innerUse=source.indexOf("return a+b");
        assertEquals(model.resolve(file,outerName),model.resolve(file,innerUse+7));
        assertEquals(model.resolve(file,innerName),model.resolve(file,innerUse+9));
        int seedDeclaration=source.indexOf("seed int");
        int seedUse=source.indexOf("+ seed +")+2;
        assertEquals(model.resolve(file,seedDeclaration),model.resolve(file,seedUse));
        int fieldDeclaration=source.indexOf("value int");
        int fieldUse=source.indexOf("this.value")+5;
        assertEquals(model.resolve(file,fieldDeclaration),model.resolve(file,fieldUse));
        var sum=model.callAt(file,source.indexOf("sum(1,2)")+6);
        assertNotNull(sum);assertEquals(java.util.List.of("a int","b int"),sum.symbol().parameters);
        var pair=model.callAt(file,source.indexOf("pair(1)")+5);
        assertNotNull(pair);assertEquals("(int,error)",pair.symbol().resultSignature);
        assertEquals(java.util.List.of("a int"),pair.symbol().parameters);
        int resultName=source.indexOf("result int"),resultUse=source.indexOf("result=x");
        assertEquals(model.resolve(file,resultName),model.resolve(file,resultUse));
        var named=model.callAt(file,source.indexOf("named(2)")+7);
        assertNotNull(named);assertEquals(java.util.List.of("x int"),named.symbol().parameters);
        var empty=model.callAt(file,source.indexOf("empty()")+6);
        assertNotNull(empty);assertTrue(empty.symbol().parameters.isEmpty());
        var available=model.complete(file,innerUse+9);
        assertTrue(available.stream().anyMatch(symbol->symbol.name.equals("a")));
        assertTrue(available.stream().anyMatch(symbol->symbol.name.equals("b")));
        myFixture.getEditor().getCaretModel().moveToOffset(outerName);
        myFixture.renameElementAtCaret("input");
        assertTrue(file.getText().contains("return input+b"));
        assertTrue(file.getText().contains("sum := (a int, b int)"));
    }
    public void testExcludedInstalledPackageNavigationHintsAndAutoImport(){
        var library=myFixture.addFileToProject(".ghi/packages/acme.lib/types.ghi","namespace acme.lib\nclass Widget {public value string\nconstructor(name string){this.value=name}\npublic func run(count int){}}\nclass Wider {}\n");
        myFixture.addFileToProject(".ghi/packages/acme.lib/tests/trap.ghi","namespace acme.lib\nclass TestTrap {}\n");
        myFixture.addFileToProject(".ghi/packages/acme.lib/vendor/trap.ghi","namespace acme.lib\nclass VendorTrap {}\n");
        myFixture.addFileToProject(".ghi/packages/stale.lib/types.ghi","namespace stale.lib\nclass Stale {}\n");
        myFixture.addFileToProject("mojave.lock","{\"version\":1,\"packages\":[{\"namespace\":\"acme.lib\"}]}");
        excludeCache(library);
        var file=myFixture.configureByText("installed.ghi","namespace main\nimport acme.lib as lib\nimport acme.lib.Widget as MyWidget\nfunc main(){w:=new lib.Widget(\"x\");w.<caret>run(2);new MyWidget(\"y\")}\n");
        var model=GhiSymbols.forFile(file);
        assertEquals(library,model.resolve(file,file.getText().indexOf("lib.Widget")+4).getContainingFile());
        assertEquals(library,model.resolve(file,file.getText().indexOf("acme.lib.Widget")+9).getContainingFile());
        assertEquals(library,model.resolve(file,file.getText().indexOf("w.run")+2).getContainingFile());
        assertEquals(java.util.List.of("count int"),model.callAt(file,file.getText().indexOf("2)")).symbol().parameters);
        assertEquals(java.util.List.of("name string"),model.callAt(file,file.getText().indexOf("\"y\"")).symbol().parameters);
        assertTrue(model.complete(file,myFixture.getCaretOffset()).stream().anyMatch(symbol->symbol.name.equals("run")));
        var paths=GhiTypeImports.candidates(file,null).stream().map(GhiTypeImports.Choice::path).toList();
        assertTrue(paths.toString(),paths.contains("acme.lib.Widget"));
        assertFalse(paths.toString(),paths.contains("acme.lib.TestTrap"));
        assertFalse(paths.toString(),paths.contains("acme.lib.VendorTrap"));
        assertFalse(paths.toString(),paths.contains("stale.lib.Stale"));
        file=myFixture.configureByText("installed-complete.ghi","namespace main\nfunc main(){new Wid<caret>}\n");
        var variants=myFixture.completeBasic();assertNotNull(variants);
        var choice=java.util.Arrays.stream(variants).filter(item->item.getObject() instanceof GhiTypeImports.Choice candidate&&candidate.path().equals("acme.lib.Widget")).findFirst().orElseThrow();
        myFixture.getLookup().setCurrentItem(choice);myFixture.finishLookup(com.intellij.codeInsight.lookup.Lookup.NORMAL_SELECT_CHAR);
        assertEquals("namespace main\nimport acme.lib.Widget\nfunc main(){new Widget}\n",file.getText());
    }
    public void testOwnerQualifiedInstalledPackagesWithSameBasename(){
        var first=myFixture.addFileToProject(".ghi/packages/arm092.migrations/migrator.ghi",
            "namespace arm092.migrations\nclass Migrator {constructor(source string){} public func run() {}}\n");
        var second=myFixture.addFileToProject(".ghi/packages/someone.migrations/migrator.ghi",
            "namespace someone.migrations\nclass Migrator {constructor(version int){} public func run() {}}\n");
        myFixture.addFileToProject("mojave.lock","{\"version\":1,\"packages\":["
            +"{\"identity\":\"arm092/migrations\",\"namespace\":\"arm092.migrations\"},"
            +"{\"identity\":\"someone/migrations\",\"namespace\":\"someone.migrations\"}]}");
        excludeCache(first);
        var file=myFixture.configureByText("owned-imports.ghi","namespace main\n"
            +"import arm092.migrations.Migrator\n"
            +"import someone.migrations.Migrator as OtherMigrator\n"
            +"func main(){new Migrator(\"old\");new OtherMigrator(2)}\n");
        var model=GhiSymbols.forFile(file);
        assertEquals(first,model.resolve(file,file.getText().indexOf("Migrator\n")).getContainingFile());
        assertEquals(second,model.resolve(file,file.getText().indexOf("Migrator as")).getContainingFile());
        assertEquals(first,model.resolve(file,file.getText().indexOf("new Migrator")+4).getContainingFile());
        assertEquals(file,model.resolve(file,file.getText().indexOf("new OtherMigrator")+4).getContainingFile());
        assertEquals(second,model.type("OtherMigrator",file).file);
        assertEquals(java.util.List.of("source string"),model.callAt(file,file.getText().indexOf("\"old\"")).symbol().parameters);
        assertEquals(java.util.List.of("version int"),model.callAt(file,file.getText().indexOf("2)}")).symbol().parameters);
        var paths=GhiTypeImports.candidates(file,"Migrator").stream().map(GhiTypeImports.Choice::path).toList();
        assertTrue(paths.toString(),paths.contains("arm092.migrations.Migrator"));
        assertTrue(paths.toString(),paths.contains("someone.migrations.Migrator"));
        file=myFixture.configureByText("owned-completion.ghi","namespace main\nimport someone.migrations.Mig<caret>\n");
        var available=GhiSymbols.forFile(file).complete(file,myFixture.getCaretOffset());
        assertEquals(1,available.stream().filter(symbol->symbol.name.equals("Migrator")).count());
        assertEquals(second,available.stream().filter(symbol->symbol.name.equals("Migrator")).findFirst().orElseThrow().file);
        file=myFixture.configureByText("owned-shorten.ghi","namespace main\n"
            +"import arm092.migrations.Migrator\n"
            +"func main(){new someone.migrations.Mig<caret>rator(2)}\n");
        var shorten=new GhiImportIntention.Shorten();
        assertTrue(shorten.isAvailable(getProject(),myFixture.getEditor(),file));
        shorten.invoke(getProject(),myFixture.getEditor(),file);
        assertTrue(file.getText(),file.getText().contains("import someone.migrations.Migrator as MigrationsMigrator"));
        assertTrue(file.getText(),file.getText().contains("new MigrationsMigrator(2)"));
        assertEquals(second,GhiSymbols.forFile(file).type("MigrationsMigrator",file).file);
    }
    public void testInstalledLockRefreshAndReadOnlyRename(){
        var library=myFixture.addFileToProject(".ghi/packages/acme.lib/types.ghi","namespace acme.lib\nclass Widget {public func run(count int){}}\n");
        var next=myFixture.addFileToProject(".ghi/packages/next.lib/types.ghi","namespace next.lib\nclass Next {}\n");
        var lock=myFixture.addFileToProject("mojave.lock","{\"version\":1,\"packages\":[{\"namespace\":\"acme.lib\"}]}");
        excludeCache(library);
        var file=myFixture.configureByText("locked.ghi","namespace main\nimport acme.lib.Widget as LocalWidget\nclass Worker extends LocalWidget {public override func run(count int){}}\nfunc main(){new LocalWidget().run(1)}\n");
        var model=GhiSymbols.forFile(file);
        var declaration=model.resolve(file,file.getText().indexOf("Widget as"));assertEquals(library,declaration.getContainingFile());
        assertTrue(new GhiRenameProcessor().canProcessElement(declaration));
        try{new com.intellij.refactoring.rename.RenameProcessor(getProject(),declaration,"Gadget",false,false).run();}
        catch(RuntimeException expected){assertTrue(expected.toString(),expected.toString().contains("read-only"));}
        assertTrue(library.getText(),library.getText().contains("class Widget"));
        try{((GhiIdentifier)declaration).setName("Gadget");fail("Installed declaration changed");}
        catch(com.intellij.util.IncorrectOperationException expected){assertTrue(library.getText().contains("class Widget"));}
        var conflicts=new com.intellij.util.containers.MultiMap<com.intellij.psi.PsiElement,String>();
        new GhiRenameProcessor().findExistingNameConflicts(declaration,"Gadget",conflicts);assertFalse(conflicts.isEmpty());
        var method=model.resolve(file,file.getText().indexOf("run(1)"));conflicts.clear();
        new GhiRenameProcessor().findExistingNameConflicts(method,"execute",conflicts);assertFalse(conflicts.isEmpty());
        var localMethod=model.resolve(file,file.getText().indexOf("run(count"));conflicts.clear();
        new GhiRenameProcessor().findExistingNameConflicts(localMethod,"execute",conflicts);assertFalse(conflicts.isEmpty());
        var unaliased=myFixture.configureByText("unaliased.ghi","namespace main\nimport acme.lib.Widget\nfunc use(){new <caret>Widget()}\n");
        try{myFixture.renameElementAtCaret("Gadget");}
        catch(RuntimeException expected){assertTrue(expected.toString(),expected.toString().contains("read-only"));}
        assertTrue(unaliased.getText(),unaliased.getText().contains("new Widget()"));
        assertTrue(library.getText(),library.getText().contains("class Widget"));
        myFixture.configureFromExistingVirtualFile(file.getVirtualFile());
        myFixture.getEditor().getCaretModel().moveToOffset(file.getText().indexOf("LocalWidget()"));myFixture.renameElementAtCaret("ConsumerWidget");
        assertTrue(file.getText(),file.getText().contains("import acme.lib.Widget as ConsumerWidget"));
        assertTrue(library.getText().contains("class Widget"));
        com.intellij.openapi.command.WriteCommandAction.runWriteCommandAction(getProject(),()->{
            try{com.intellij.openapi.vfs.VfsUtil.saveText(lock.getVirtualFile(),"{\"version\":1,\"packages\":[{\"namespace\":\"next.lib\"}]}");}
            catch(java.io.IOException error){throw new RuntimeException(error);}
        });
        model=GhiSymbols.forFile(file);
        assertNull(model.resolve(file,file.getText().indexOf("Widget as")));
        var paths=GhiTypeImports.candidates(file,null).stream().map(GhiTypeImports.Choice::path).toList();
        assertFalse(paths.toString(),paths.contains("acme.lib.Widget"));assertTrue(paths.toString(),paths.contains("next.lib.Next"));
        com.intellij.openapi.command.WriteCommandAction.runWriteCommandAction(getProject(),()->{
            var document=com.intellij.psi.PsiDocumentManager.getInstance(getProject()).getDocument(next);
            assertNotNull(document);document.setText("namespace next.lib\nclass Updated {}\n");
            com.intellij.psi.PsiDocumentManager.getInstance(getProject()).commitDocument(document);
        });
        paths=GhiTypeImports.candidates(file,null).stream().map(GhiTypeImports.Choice::path).toList();
        assertFalse(paths.toString(),paths.contains("next.lib.Next"));assertTrue(paths.toString(),paths.contains("next.lib.Updated"));
        com.intellij.openapi.command.WriteCommandAction.runWriteCommandAction(getProject(),()->{
            try{next.getVirtualFile().delete(this);}
            catch(java.io.IOException error){throw new RuntimeException(error);}
        });
        assertFalse(GhiTypeImports.candidates(file,null).stream().anyMatch(choice->choice.path().equals("next.lib.Updated")));
        com.intellij.openapi.command.WriteCommandAction.runWriteCommandAction(getProject(),()->{
            try{com.intellij.openapi.vfs.VfsUtil.saveText(lock.getVirtualFile(),"{broken");}
            catch(java.io.IOException error){throw new RuntimeException(error);}
        });
        paths=GhiTypeImports.candidates(file,null).stream().map(GhiTypeImports.Choice::path).toList();
        assertFalse(paths.toString(),paths.contains("next.lib.Next"));assertFalse(paths.toString(),paths.contains("acme.lib.Widget"));
        com.intellij.openapi.command.WriteCommandAction.runWriteCommandAction(getProject(),()->{
            try{com.intellij.openapi.vfs.VfsUtil.saveText(lock.getVirtualFile(),"{\"version\":\"oops\",\"packages\":[{\"namespace\":\"acme.lib\"}]}");}
            catch(java.io.IOException error){throw new RuntimeException(error);}
        });
        assertFalse(GhiTypeImports.candidates(file,null).stream().anyMatch(choice->choice.path().equals("acme.lib.Widget")));
    }
    public void testNestedManifestDoesNotInheritAncestorPackages(){
        myFixture.addFileToProject(".ghi/packages/acme.lib/types.ghi","namespace acme.lib\nclass Widget {}\n");
        myFixture.addFileToProject("mojave.lock","{\"version\":1,\"packages\":[{\"namespace\":\"acme.lib\"}]}");
        myFixture.addFileToProject("nested/mojave.json","{\"version\":1,\"dependencies\":{}}");
        var file=myFixture.addFileToProject("nested/main.ghi","namespace main\nfunc main(){new Widget()}\n");
        myFixture.configureFromExistingVirtualFile(file.getVirtualFile());
        assertFalse(GhiTypeImports.candidates(file,null).stream().anyMatch(choice->choice.path().equals("acme.lib.Widget")));
        assertNull(GhiSymbols.forFile(file).resolve(file,file.getText().indexOf("Widget")));
    }
    public void testInstalledSourceUsesConsumingLock(){
        var library=myFixture.addFileToProject(".ghi/packages/acme.lib/types.ghi","namespace acme.lib\nimport next.lib as next\nfunc use(){new next.Next()}\n");
        var target=myFixture.addFileToProject(".ghi/packages/next.lib/types.ghi","namespace next.lib\nclass Next {}\n");
        myFixture.addFileToProject(".ghi/packages/acme.lib/mojave.json","{\"version\":1,\"dependencies\":{}}");
        myFixture.addFileToProject(".ghi/packages/acme.lib/mojave.lock","{broken");
        myFixture.addFileToProject("mojave.lock","{\"version\":1,\"packages\":[{\"namespace\":\"acme.lib\"},{\"namespace\":\"next.lib\"}]}");
        excludeCache(library);
        assertEquals(target,GhiSymbols.forFile(library).resolve(library,library.getText().indexOf("next.Next")+5).getContainingFile());
    }
    public void testLiveGhiDebuggerStopsAndStepsOnSource() throws Exception {
        String compiler=System.getenv("GHI_TEST_COMPILER");if(compiler==null||compiler.isBlank())return;
        String source="namespace main\nimport time \"go:time\"\nclass Counter {\n public value int\n constructor(value int){this.value=value}\n public func add(amount int) int {\n  this.value += amount\n  return this.value\n }\n}\nfunc main() {\n counter := new Counter(7)\n answer := counter.add(5)\n println(answer)\n try {\n  throw new Exception(\"debug exception\")\n } catch err Exception {\n  println(err.message)\n  println(err.code)\n }\n count := 0\n for {\n  time.Sleep(200 * time.Millisecond)\n  count += 1\n }\n}\n";
        Path root=diskRoot;Path sourceFile=root.resolve("main.ghi");Files.writeString(sourceFile,source);
        var virtual=com.intellij.openapi.vfs.LocalFileSystem.getInstance().refreshAndFindFileByNioFile(sourceFile);assertNotNull(virtual);
        com.intellij.openapi.application.WriteAction.run(()->{
            var roots=com.intellij.openapi.roots.ModuleRootManager.getInstance(myFixture.getModule()).getModifiableModel();
            roots.addContentEntry(virtual.getParent().getUrl());roots.commit();
        });
        myFixture.configureFromExistingVirtualFile(virtual);
        var file=com.intellij.psi.PsiManager.getInstance(getProject()).findFile(virtual);assertNotNull(file);
        var settings=GhiSettings.get(getProject()).getState();settings.executable=compiler;settings.directory=root.toString();
        int line=source.substring(0,source.indexOf("this.value += amount")).split("\n",-1).length-1;
        var type=com.intellij.xdebugger.XDebuggerUtil.getInstance().findBreakpointType(GhiBreakpointType.class);
        assertNotNull(type);assertTrue(type.canPutAt(file.getVirtualFile(),line,getProject()));
        var manager=com.intellij.xdebugger.XDebuggerManager.getInstance(getProject());
        var breakpoint=manager.getBreakpointManager().addLineBreakpoint(type,file.getVirtualFile().getUrl(),line,type.createBreakpointProperties(file.getVirtualFile(),line));
        int callLine=source.substring(0,source.indexOf("answer := counter.add(5)")).split("\n",-1).length-1;
        var callBreakpoint=manager.getBreakpointManager().addLineBreakpoint(type,file.getVirtualFile().getUrl(),callLine,type.createBreakpointProperties(file.getVirtualFile(),callLine));
        int catchLine=source.substring(0,source.indexOf("println(err.message)")).split("\n",-1).length-1;
        var catchBreakpoint=manager.getBreakpointManager().addLineBreakpoint(type,file.getVirtualFile().getUrl(),catchLine,type.createBreakpointProperties(file.getVirtualFile(),catchLine));
        int loopLine=source.substring(0,source.indexOf("count += 1")).split("\n",-1).length-1;
        int quickLine=source.substring(0,source.indexOf("count := 0")).split("\n",-1).length-1;
        com.intellij.xdebugger.breakpoints.XLineBreakpoint<GhiBreakpointType.Properties> liveBreakpoint=null;
        com.intellij.xdebugger.XDebugSession session=null;
        try{
            var action=ActionManager.getInstance().getAction("Ghi.Debug");assertNotNull(action);
            var context=com.intellij.openapi.actionSystem.impl.SimpleDataContext.getProjectContext(getProject());
            action.actionPerformed(com.intellij.openapi.actionSystem.AnActionEvent.createFromAnAction(action,null,com.intellij.openapi.actionSystem.ActionPlaces.UNKNOWN,context));
            long deadline=System.currentTimeMillis()+60000;
            while(System.currentTimeMillis()<deadline){
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();
                var sessions=manager.getDebugSessions();if(sessions.length>0){session=sessions[0];if(session.isSuspended())break;}
                Thread.sleep(50);
            }
            assertNotNull("Debug session did not start",session);
            assertTrue("Breakpoint did not stop the debuggee",session.isSuspended());
            assertEquals(callLine,session.getCurrentPosition().getLine());
            session.stepInto();
            while(System.currentTimeMillis()<deadline){
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();
                if(session.isSuspended()&&session.getCurrentPosition()!=null&&session.getCurrentPosition().getLine()==line-1)break;
                Thread.sleep(50);
            }
            assertTrue("Step into did not stop",session.isSuspended());
            assertEquals(file.getVirtualFile(),session.getCurrentPosition().getFile());
            assertEquals(line-1,session.getCurrentPosition().getLine());
            session.stepOver(false);
            while(System.currentTimeMillis()<deadline){
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();
                if(session.isSuspended()&&session.getCurrentPosition()!=null&&session.getCurrentPosition().getLine()==line)break;
                Thread.sleep(50);
            }
            assertEquals(line,session.getCurrentPosition().getLine());
            var stack=session.getSuspendContext().getActiveExecutionStack();assertNotNull(stack);
            var title=new com.intellij.ui.SimpleColoredComponent();stack.getTopFrame().customizePresentation(title);
            assertTrue("Ghi method name missing from stack",title.getCharSequence(false).toString().contains("main.Counter.add"));
            var variables=debugChildren(stack.getTopFrame());
            var receiver=debugValue(variables,"this");assertNotNull("Receiver is missing",receiver);
            assertNotNull("Method argument is missing",debugValue(variables,"amount"));
            var fields=debugChildren(receiver);
            assertNotNull("Ghi field name was not decoded",debugValue(fields,"value"));
            session.stepOver(false);
            while(System.currentTimeMillis()<deadline){
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();
                if(session.isSuspended()&&session.getCurrentPosition()!=null&&session.getCurrentPosition().getLine()==line+1)break;
                Thread.sleep(50);
            }
            assertTrue("Step over did not stop",session.isSuspended());
            assertEquals(line+1,session.getCurrentPosition().getLine());
            var stepped=debugChildren(session.getSuspendContext().getActiveExecutionStack().getTopFrame());
            var steppedFields=debugChildren(debugValue(stepped,"this"));
            assertEquals("12",debugDisplay(debugValue(steppedFields,"value")));
            session.stepOut();
            while(System.currentTimeMillis()<deadline){
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();
                if(session.isSuspended()&&session.getCurrentPosition()!=null&&session.getCurrentPosition().getLine()==callLine)break;
                Thread.sleep(50);
            }
            assertTrue("Step out did not stop",session.isSuspended());
            assertEquals("Step out did not reach the caller",callLine,session.getCurrentPosition().getLine());
            var caller=new com.intellij.ui.SimpleColoredComponent();
            session.getSuspendContext().getActiveExecutionStack().getTopFrame().customizePresentation(caller);
            assertTrue("Step out retained a generated helper frame",caller.getCharSequence(false).toString().contains("main.main"));
            session.stepOver(false);
            while(System.currentTimeMillis()<deadline){
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();
                if(session.isSuspended()&&session.getCurrentPosition()!=null&&session.getCurrentPosition().getLine()==callLine+1)break;
                Thread.sleep(50);
            }
            assertEquals(callLine+1,session.getCurrentPosition().getLine());
            var callerValues=debugChildren(session.getSuspendContext().getActiveExecutionStack().getTopFrame());
            assertEquals("12",debugDisplay(debugValue(callerValues,"answer")));
            boolean caught=false;
            for(int attempt=0;attempt<4&&!caught;attempt++){
                session.resume();
                while(System.currentTimeMillis()<deadline){
                    com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();
                    if(session.isSuspended()&&session.getCurrentPosition()!=null&&session.getCurrentPosition().getLine()==catchLine)break;
                    Thread.sleep(50);
                }
                assertTrue("Catch breakpoint did not stop",session.isSuspended());
                assertEquals(catchLine,session.getCurrentPosition().getLine());
                var caughtVariables=debugChildren(session.getSuspendContext().getActiveExecutionStack().getTopFrame());
                var exception=debugValue(caughtVariables,"err");
                if(exception==null)continue;
                var exceptionFields=debugChildren(exception);
                var message=debugValue(exceptionFields,"message");
                if(message==null)continue;
                assertTrue(debugDisplay(message).contains("debug exception"));
                assertEquals("0",debugDisplay(debugValue(exceptionFields,"code")));
                assertNotNull("Exception stackTrace missing",debugValue(exceptionFields,"stackTrace"));
                caught=true;
            }
            assertTrue("Caught Ghi exception was not displayed",caught);
            session.resume();
            assertFalse("Resume did not start execution",session.isSuspended());
            Thread.sleep(300);
            liveBreakpoint=manager.getBreakpointManager().addLineBreakpoint(type,file.getVirtualFile().getUrl(),loopLine,type.createBreakpointProperties(file.getVirtualFile(),loopLine));
            long liveDeadline=System.currentTimeMillis()+5000;
            while(System.currentTimeMillis()<liveDeadline){
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();
                if(session.isSuspended()&&session.getCurrentPosition()!=null&&session.getCurrentPosition().getLine()==loopLine)break;
                Thread.sleep(20);
            }
            assertTrue("Breakpoint added while running did not stop",session.isSuspended());
            assertEquals(loopLine,session.getCurrentPosition().getLine());
            session.resume();
            assertFalse("Resume did not start execution",session.isSuspended());
            manager.getBreakpointManager().removeBreakpoint(liveBreakpoint);
            liveBreakpoint=null;
            long clearDeadline=System.currentTimeMillis()+700;
            while(System.currentTimeMillis()<clearDeadline){
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();Thread.sleep(20);
            }
            assertFalse("Removed breakpoint stopped the running debuggee",session.isSuspended());
            var quickBreakpoint=manager.getBreakpointManager().addLineBreakpoint(type,file.getVirtualFile().getUrl(),quickLine,type.createBreakpointProperties(file.getVirtualFile(),quickLine));
            manager.getBreakpointManager().removeBreakpoint(quickBreakpoint);
            session.getDebugProcess().startPausing();
            long pauseDeadline=System.currentTimeMillis()+5000;
            while(System.currentTimeMillis()<pauseDeadline&&!session.isSuspended()){
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();Thread.sleep(20);
            }
            assertTrue("Debugger did not pause after quick add/remove",session.isSuspended());
            var connection=GhiDebugProcess.class.getDeclaredField("delve");connection.setAccessible(true);
            var delve=(GhiDelve)connection.get(session.getDebugProcess());
            var points=delve.request("ListBreakpoints",new com.google.gson.JsonObject()).get(3,TimeUnit.SECONDS).getAsJsonArray("Breakpoints");
            assertNotNull(points);
            for(var point:points)assertFalse("Quickly removed breakpoint remains in Delve",point.getAsJsonObject().get("line").getAsInt()==quickLine+1&&point.getAsJsonObject().get("file").getAsString().endsWith("main.ghi"));
            session.getDebugProcess().stop();
        }finally{
            if(session!=null){
                session.getDebugProcess().stop();
                session.stop();
                var descriptor=session.getRunContentDescriptor();
                if(descriptor!=null){
                    com.intellij.execution.ui.RunContentManager.getInstance(getProject()).removeRunContent(com.intellij.execution.executors.DefaultDebugExecutor.getDebugExecutorInstance(),descriptor);
                    if(!com.intellij.openapi.util.Disposer.isDisposed(descriptor))com.intellij.openapi.util.Disposer.dispose(descriptor);
                }
                var console=session.getDebugProcess().createConsole();
                if(console instanceof com.intellij.openapi.Disposable disposable&&!com.intellij.openapi.util.Disposer.isDisposed(disposable))com.intellij.openapi.util.Disposer.dispose(disposable);
                long stoppedBy=System.currentTimeMillis()+10000;
                while(!session.getDebugProcess().getProcessHandler().isProcessTerminated()&&System.currentTimeMillis()<stoppedBy){
                    com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();Thread.sleep(50);
                }
                assertTrue("Delve did not exit",session.getDebugProcess().getProcessHandler().isProcessTerminated());
            }
            com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();
            com.intellij.openapi.fileEditor.FileEditorManager.getInstance(getProject()).closeFile(virtual);
            com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();
            manager.getBreakpointManager().removeBreakpoint(breakpoint);
            manager.getBreakpointManager().removeBreakpoint(callBreakpoint);
            manager.getBreakpointManager().removeBreakpoint(catchBreakpoint);
            if(liveBreakpoint!=null)manager.getBreakpointManager().removeBreakpoint(liveBreakpoint);
            com.intellij.openapi.application.WriteAction.run(()->{
                var roots=com.intellij.openapi.roots.ModuleRootManager.getInstance(myFixture.getModule()).getModifiableModel();
                for(var entry:roots.getContentEntries())if(entry.getUrl().equals(virtual.getParent().getUrl()))roots.removeContentEntry(entry);
                roots.commit();
            });
        }
    }
    public void testGenericDebugNamesUseCompilerMetadata() throws Exception {
        Path executable=diskRoot.resolve("generic-debug");
        Files.writeString(Path.of(executable+".ghi-debug.json"),"{\"version\":1,\"fields\":{},\"types\":{\"main.ghiData_Box\":\"main.Box\"},\"functions\":{\"main.GhiBody_Box_get\":{\"name\":\"main.Box.get\",\"helper\":false}}}");
        var names=GhiDebugNames.read(executable);
        assertEquals("main.Box[int]",names.type("main.ghiData_Box[go.shape.int]"));
        assertEquals("main.Box.get[int]",names.function("main.GhiBody_Box_get[go.shape.int]").name());
    }
    private com.intellij.xdebugger.frame.XValueChildrenList debugChildren(com.intellij.xdebugger.frame.XValueContainer container) throws Exception {
        var values=new java.util.concurrent.atomic.AtomicReference<com.intellij.xdebugger.frame.XValueChildrenList>();
        var error=new java.util.concurrent.atomic.AtomicReference<String>();
        var node=(com.intellij.xdebugger.frame.XCompositeNode)java.lang.reflect.Proxy.newProxyInstance(
            getClass().getClassLoader(),new Class[]{com.intellij.xdebugger.frame.XCompositeNode.class},(proxy,method,args)->{
                if(method.getName().equals("addChildren"))values.set((com.intellij.xdebugger.frame.XValueChildrenList)args[0]);
                if(method.getName().equals("setErrorMessage"))error.set((String)args[0]);
                return null;
            });
        container.computeChildren(node);
        long deadline=System.currentTimeMillis()+5000;
        while(values.get()==null&&error.get()==null&&System.currentTimeMillis()<deadline){
            com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents();Thread.sleep(20);
        }
        assertNull(error.get());assertNotNull("Debugger children timed out",values.get());return values.get();
    }
    private com.intellij.xdebugger.frame.XValue debugValue(com.intellij.xdebugger.frame.XValueChildrenList values,String name){
        if(values==null)return null;
        for(int i=0;i<values.size();i++)if(values.getName(i).equals(name))return values.getValue(i);
        return null;
    }
    private String debugDisplay(com.intellij.xdebugger.frame.XValue value){
        assertNotNull(value);
        var display=new java.util.concurrent.atomic.AtomicReference<String>();
        var node=(com.intellij.xdebugger.frame.XValueNode)java.lang.reflect.Proxy.newProxyInstance(
            getClass().getClassLoader(),new Class[]{com.intellij.xdebugger.frame.XValueNode.class},(proxy,method,args)->{
                if(method.getName().equals("setPresentation")&&args.length==4)display.set((String)args[2]);
                return null;
            });
        value.computePresentation(node,com.intellij.xdebugger.frame.XValuePlace.TREE);
        assertNotNull("Variable presentation missing",display.get());return display.get();
    }
    private void excludeCache(com.intellij.psi.PsiFile source){
        var cache=source.getVirtualFile().getParent().getParent().getParent();assertEquals(".ghi",cache.getName());
        com.intellij.openapi.application.WriteAction.run(()->{
            var roots=com.intellij.openapi.roots.ModuleRootManager.getInstance(myFixture.getModule()).getModifiableModel();
            roots.getContentEntries()[0].addExcludeFolder(cache.getUrl());roots.commit();
        });
        assertTrue(com.intellij.openapi.roots.ProjectFileIndex.getInstance(getProject()).isExcluded(cache));
    }
    private void checkImportedProject(String name,String source,String library) throws Exception {
        String compiler=System.getenv("GHI_TEST_COMPILER");if(compiler==null||compiler.isBlank())return;
        Path project=Files.createDirectory(diskRoot.resolve(name));Files.writeString(project.resolve("main.ghi"),source);
        Path dependency=Files.createDirectory(project.resolve("library"));Files.writeString(dependency.resolve("types.ghi"),library);
        var process=GhiCommand.create(compiler,project,"check","").withRedirectErrorStream(true).createProcess();
        try{assertTrue(process.waitFor(30,TimeUnit.SECONDS));String output=new String(process.getInputStream().readAllBytes(),java.nio.charset.StandardCharsets.UTF_8);assertEquals(output,0,process.exitValue());}finally{process.destroyForcibly();}
    }
}
