package am.ghi.ide;
import com.intellij.lexer.LexerBase;
import com.intellij.psi.TokenType;
import com.intellij.psi.tree.IElementType;
import java.util.Set;
import java.util.regex.Pattern;

public final class GhiLexer extends LexerBase {
    public static final IElementType KEYWORD=type("KEYWORD"), IDENTIFIER=type("IDENTIFIER"),
        STRING=type("STRING"), NUMBER=type("NUMBER"), COMMENT=type("COMMENT"), OPERATOR=type("OPERATOR"),
        LPAREN=type("LPAREN"), RPAREN=type("RPAREN"), LBRACE=type("LBRACE"), RBRACE=type("RBRACE"),
        LBRACKET=type("LBRACKET"), RBRACKET=type("RBRACKET");
    private static IElementType type(String name) { return new IElementType(name,GhiLanguage.INSTANCE); }
    private static final Set<String> KEYWORDS=Set.of(
        "namespace","import", "as","class","interface","extends","implements","constructor","override",
        "new","public","private","protected","this","parent","try","catch","finally","throw",
        "break","case","chan","const","continue","default","defer","else","fallthrough","for",
        "func","go","goto","if","map","match","range","return","select","struct","switch","type","var",
        "nil","true","false");
    private static final Pattern NUMERIC=Pattern.compile(
        "(?:0[xX][0-9a-fA-F_]+(?:\\.[0-9a-fA-F_]*)?(?:[pP][+-]?[0-9_]+)?|0[bB][01_]+|0[oO][0-7_]+|(?:[0-9][0-9_]*(?:\\.[0-9_]*)?|\\.[0-9][0-9_]*)(?:[eE][+-]?[0-9_]+)?)i?");
    private CharSequence buffer="";
    private int start,end,limit;
    private IElementType token;
    public void start(CharSequence text,int offset,int endOffset,int initialState) {
        buffer=text; start=offset; end=offset; limit=endOffset; advance();
    }
    public int getState(){return 0;}
    public IElementType getTokenType(){return token;}
    public int getTokenStart(){return start;}
    public int getTokenEnd(){return end;}
    public CharSequence getBufferSequence(){return buffer;}
    public int getBufferEnd(){return limit;}
    public void advance(){
        start=end;
        if(start>=limit){token=null;return;}
        char c=buffer.charAt(end++);
        if(Character.isWhitespace(c)){
            while(end<limit && Character.isWhitespace(buffer.charAt(end)))end++;
            token=TokenType.WHITE_SPACE;return;
        }
        if(c=='/' && end<limit && buffer.charAt(end)=='/'){
            while(end<limit && buffer.charAt(end)!='\n')end++;
            token=COMMENT;return;
        }
        if(c=='/' && end<limit && buffer.charAt(end)=='*'){
            end++;
            while(end<limit){if(buffer.charAt(end++)=='*' && end<limit && buffer.charAt(end)=='/'){end++;break;}}
            token=COMMENT;return;
        }
        if(c=='"' || c=='\'' || c=='`'){
            while(end<limit){char n=buffer.charAt(end++);if(n==c)break;
                if(c!='`' && n=='\\' && end<limit)end++;
                else if(c!='`' && (n=='\n' || n=='\r'))break;
            }
            token=STRING;return;
        }
        int first=Character.codePointAt(buffer,start);
        if(Character.isLetter(first) || first=='_'){
            end=start+Character.charCount(first);
            while(end<limit){int next=Character.codePointAt(buffer,end);
                if(!Character.isLetterOrDigit(next) && next!='_')break;
                end+=Character.charCount(next);
            }
            token=KEYWORDS.contains(buffer.subSequence(start,end).toString())?KEYWORD:IDENTIFIER;return;
        }
        if(Character.isDigit(c) || (c=='.' && end<limit && Character.isDigit(buffer.charAt(end)))){
            var match=NUMERIC.matcher(buffer).region(start,limit);
            if(match.lookingAt())end=match.end();
            token=NUMBER;return;
        }
        if(c=='=' && end<limit && buffer.charAt(end)=='>'){end++;token=OPERATOR;return;}
        token=switch(c){
            case '('->LPAREN;case ')'->RPAREN;case '{'->LBRACE;case '}'->RBRACE;
            case '['->LBRACKET;case ']'->RBRACKET;
            default->"+-*/%&|^!<>=:;,.?~".indexOf(c)>=0?OPERATOR:TokenType.BAD_CHARACTER;
        };
    }
}
