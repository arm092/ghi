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
        assertEquals("constructor",call.symbol().name);assertEquals(1,call.parameter());assertEquals(java.util.List.of("value string","count int = 1"),call.symbol().parameters);
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
    private void checkImportedProject(String name,String source,String library) throws Exception {
        String compiler=System.getenv("GHI_TEST_COMPILER");if(compiler==null||compiler.isBlank())return;
        Path project=Files.createDirectory(diskRoot.resolve(name));Files.writeString(project.resolve("main.ghi"),source);
        Path dependency=Files.createDirectory(project.resolve("library"));Files.writeString(dependency.resolve("types.ghi"),library);
        var process=GhiCommand.create(compiler,project,"check","").withRedirectErrorStream(true).createProcess();
        try{assertTrue(process.waitFor(30,TimeUnit.SECONDS));String output=new String(process.getInputStream().readAllBytes(),java.nio.charset.StandardCharsets.UTF_8);assertEquals(output,0,process.exitValue());}finally{process.destroyForcibly();}
    }
}
