package am.ghi.ide;
import org.junit.Test;
import static org.junit.Assert.*;
import com.intellij.psi.TokenType;
import com.intellij.psi.tree.IElementType;
import java.nio.file.Path;
import java.util.*;
public class GhiCoreTest {
    private record Token(IElementType type,String text,int start){}
    private static List<Token> lex(String source){
        var lexer=new GhiLexer();lexer.start(source);
        var result=new ArrayList<Token>();int offset=0;
        while(lexer.getTokenType()!=null){
            assertEquals(offset,lexer.getTokenStart());assertTrue(lexer.getTokenEnd()>offset);
            result.add(new Token(lexer.getTokenType(),source.substring(lexer.getTokenStart(),lexer.getTokenEnd()),offset));
            offset=lexer.getTokenEnd();lexer.advance();
        }
        assertEquals(source.length(),offset);return result;
    }
    @Test public void syntaxAndLiteralBoundaries(){
        var tokens=lex("class User { // throw\n name string = `raw\nclass`; number := 1+2; char := '\\''; s := \"a\\\"b\" }");
        assertEquals(GhiLexer.KEYWORD,tokens.getFirst().type());
        assertTrue(tokens.stream().anyMatch(t->t.type()==GhiLexer.COMMENT && t.text().equals("// throw")));
        assertTrue(tokens.stream().anyMatch(t->t.type()==GhiLexer.STRING && t.text().equals("`raw\nclass`")));
        assertEquals(List.of("1","2"),tokens.stream().filter(t->t.type()==GhiLexer.NUMBER).map(Token::text).toList());
        assertFalse(tokens.stream().anyMatch(t->t.type()==TokenType.BAD_CHARACTER));
    }
    @Test public void supplementaryUnicodeIdentifiers(){
        var tokens=lex("𐐀 := 1; _ = 𐐀");
        assertFalse(tokens.stream().anyMatch(t->t.type()==TokenType.BAD_CHARACTER));
        assertEquals(2,tokens.stream().filter(t->t.type()==GhiLexer.IDENTIFIER && t.text().equals("𐐀")).count());
    }
    @Test public void incompleteEditingAndRestart(){
        for(String source:List.of("/* unfinished","`unfinished\nraw","\"unfinished","name := 0x1.fp+2i","անուն := .5e-2")){
            var tokens=lex(source);
            for(var token:tokens){
                var lexer=new GhiLexer();lexer.start(source,token.start(),source.length(),0);
                assertEquals(token.type(),lexer.getTokenType());
                assertEquals(token.text(),source.substring(lexer.getTokenStart(),lexer.getTokenEnd()));
            }
        }
    }
    @Test public void diagnosticPathsAndOffsets(){
        var windows=GhiDiagnostic.parse("C:\\projects with spaces\\main.ghi:12:7: bad type\n");
        assertNotNull(windows);assertEquals("C:\\projects with spaces\\main.ghi",windows.path());
        assertEquals(11,windows.line());assertEquals(6,windows.column());
        var unix=GhiDiagnostic.parse("/tmp/app/main.ghi:9: failure");assertEquals(0,unix.column());
        assertNull(GhiDiagnostic.parse("Go toolchain: go1.26.8"));
        assertNull(GhiDiagnostic.parse("file.ghi:999999999999999999:1: bad"));
        assertNull(GhiDiagnostic.parse("file.ghi:0:1: bad"));
    }
    @Test public void commandPreservesPathsAndArguments(){
        Path dir=Path.of(System.getProperty("java.io.tmpdir"),"ghi project").toAbsolutePath();
        var run=GhiCommand.create("C:/compiler folder/ghi.exe",dir,"run","first \"two words\" \"\" --port 8080");
        assertEquals("C:/compiler folder/ghi.exe",run.getExePath().replace('\\','/'));
        assertEquals(List.of("run",dir.toString(),"--","first","two words","","--port","8080"),run.getParametersList().getList());
        assertEquals(List.of("build",dir.toString()),GhiCommand.create("ghi",dir,"build","ignored").getParametersList().getList());
        assertEquals(dir.resolve("child"),GhiCommand.directory(dir.toString(),"child"));
    }
    @Test public void contextualNamesAndPartialEditorRanges(){
        String source="namespace main\nimport fmt \"go:fmt\"\nclass Person { name string; constructor(name string){this.name=name}; public func greet() string {return this.name} }\nfunc show(person Person){fmt.Println(person.greet())}";
        var lexer=new GhiHighlightingLexer();lexer.start(source);
        var roles=new HashMap<Integer,IElementType>();
        while(lexer.getTokenType()!=null){roles.put(lexer.getTokenStart(),lexer.getTokenType());lexer.advance();}
        assertEquals(GhiHighlightingLexer.TYPE,roles.get(source.indexOf("Person")));
        assertEquals(GhiHighlightingLexer.FIELD,roles.get(source.indexOf("name string")));
        assertEquals(GhiHighlightingLexer.PARAMETER,roles.get(source.indexOf("name string",source.indexOf("constructor"))));
        assertEquals(GhiHighlightingLexer.FIELD,roles.get(source.indexOf("this.name")+5));
        assertEquals(GhiHighlightingLexer.PARAMETER,roles.get(source.indexOf("=name")+1));
        assertEquals(GhiHighlightingLexer.METHOD,roles.get(source.indexOf("greet")));
        assertEquals(GhiHighlightingLexer.FUNCTION,roles.get(source.indexOf("show")));
        assertEquals(GhiHighlightingLexer.BUILTIN_TYPE,roles.get(source.indexOf("string")));
        int end=source.indexOf("Person");lexer.start(source,0,end,0);
        int count=0;while(lexer.getTokenType()!=null){assertTrue(lexer.getTokenEnd()<=end);assertTrue(++count<100);lexer.advance();}
    }
}
